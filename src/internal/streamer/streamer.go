package streamer

import (
	"crypto/md5"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type Streamer struct {
	MediaRoot  string
	CacheRoot  string
	FFmpegPath string
}

func New(root string) *Streamer {
	cache := filepath.Join(root, ".metadata", "cache")
	os.MkdirAll(cache, 0755)
	return &Streamer{
		MediaRoot:  root,
		CacheRoot:  cache,
		FFmpegPath: "ffmpeg",
	}
}

// ServeMedia handles direct streaming
func (s *Streamer) ServeMedia(w http.ResponseWriter, r *http.Request) {
	relPath := r.URL.Query().Get("path")
	if relPath == "" {
		http.Error(w, "Path required", http.StatusBadRequest)
		return
	}

	fullPath := filepath.Join(s.MediaRoot, relPath)
	if !strings.HasPrefix(fullPath, filepath.Clean(s.MediaRoot)) {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	http.ServeFile(w, r, fullPath)
}

// ServeHLS generates and serves M3U8/TS segments
func (s *Streamer) ServeHLS(w http.ResponseWriter, r *http.Request) {
	relPath := r.URL.Query().Get("path")
	if relPath == "" {
		http.Error(w, "Path required", http.StatusBadRequest)
		return
	}

	fullPath := filepath.Join(s.MediaRoot, relPath)
	hash := fmt.Sprintf("%x", md5.Sum([]byte(relPath)))
	hlsDir := filepath.Join(s.CacheRoot, hash)
	os.MkdirAll(hlsDir, 0755)

	m3u8Path := filepath.Join(hlsDir, "index.m3u8")

	// Start transcoding if playlist doesn't exist
	if _, err := os.Stat(m3u8Path); os.IsNotExist(err) {
		fmt.Printf("🎬 Starting JIT HLS Transcode for %s\n", relPath)
		go s.transcodeToHLS(fullPath, hlsDir)
		// Wait a bit for the first segments
		// In a production app, we'd use a better signaling mechanism
	}

	// Serve the HLS directory
	fileServer := http.FileServer(http.Dir(hlsDir))
	// Strip the prefix or handle path mapping
	r.URL.Path = strings.TrimPrefix(r.URL.Path, "/hls")
	fileServer.ServeHTTP(w, r)
}

func (s *Streamer) transcodeToHLS(inputPath, outputDir string) {
	// Optimization: Use copy codec if possible, or fast x264
	cmd := exec.Command(s.FFmpegPath,
		"-i", inputPath,
		"-codec:v", "libx264", "-preset", "veryfast",
		"-codec:a", "aac", "-b:a", "128k",
		"-f", "hls",
		"-hls_time", "10",
		"-hls_list_size", "0",
		"-hls_segment_filename", filepath.Join(outputDir, "seg_%03d.ts"),
		filepath.Join(outputDir, "index.m3u8"),
	)
	err := cmd.Run()
	if err != nil {
		fmt.Printf("❌ FFmpeg Error: %v\n", err)
	}
}
