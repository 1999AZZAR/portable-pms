package streamer

import (
	"fmt"
	"net/http"
	"os/exec"
	"path/filepath"
	"strings"
)

type Streamer struct {
	MediaRoot  string
	FFmpegPath string
}

func New(root string) *Streamer {
	return &Streamer{
		MediaRoot:  root,
		FFmpegPath: "ffmpeg", // Fallback to system ffmpeg
	}
}

// ServeMedia handles direct streaming and range requests
func (s *Streamer) ServeMedia(w http.ResponseWriter, r *http.Request) {
	relPath := r.URL.Query().Get("path")
	if relPath == "" {
		http.Error(w, "Path required", http.StatusBadRequest)
		return
	}

	// 🛡 Path Sanitization
	fullPath := filepath.Join(s.MediaRoot, relPath)
	if !strings.HasPrefix(fullPath, filepath.Clean(s.MediaRoot)) {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	http.ServeFile(w, r, fullPath)
}

// GenerateThumbnail uses ffmpeg to extract a frame
func (s *Streamer) GenerateThumbnail(videoPath string, outPath string) error {
	cmd := exec.Command(s.FFmpegPath, "-i", videoPath, "-ss", "00:00:05", "-vframes", "1", "-q:v", "2", outPath)
	return cmd.Run()
}

// HLSLogic Placeholder for Phase 2 implementation
func (s *Streamer) StartTranscode(videoPath string) {
	fmt.Printf("🎬 Transcoding scheduled for %s\n", videoPath)
}
