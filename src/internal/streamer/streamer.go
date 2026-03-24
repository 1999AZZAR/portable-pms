package streamer

import (
	"context"
	"crypto/md5"
	"errors"
	"fmt"
	"log/slog"
	"mime"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type Streamer struct {
	MediaRoot  string
	CacheRoot  string
	FFmpegPath string
	Logger     *slog.Logger
	processes  sync.Map
}

func New(root string, logger *slog.Logger) *Streamer {
	if logger == nil {
		logger = slog.Default()
	}
	cache := filepath.Join(root, ".metadata", "cache")
	os.MkdirAll(cache, 0755)
	return &Streamer{
		MediaRoot:  root,
		CacheRoot:  cache,
		FFmpegPath: "ffmpeg",
		Logger:     logger,
	}
}

func (s *Streamer) Shutdown(ctx context.Context) error {
	s.Logger.Info("shutting down streamer, terminating active transcodes")
	
	var killErrs []error
	s.processes.Range(func(key, value interface{}) bool {
		if cmd, ok := value.(*exec.Cmd); ok {
			if cmd.Process != nil {
				if err := cmd.Process.Kill(); err != nil {
					s.Logger.Warn("failed to kill process", "error", err)
					killErrs = append(killErrs, err)
				}
			}
		}
		s.processes.Delete(key)
		return true
	})

	if len(killErrs) > 0 {
		return fmt.Errorf("failed to kill %d processes", len(killErrs))
	}
	return nil
}

// ServeMedia handles direct streaming
func (s *Streamer) ServeMedia(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Query().Get("path")
	if path == "" {
		http.Error(w, "Missing path parameter", http.StatusBadRequest)
		return
	}

	fullPath, err := s.resolveMediaPath(path)
	if err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, os.ErrPermission) {
			status = http.StatusForbidden
			s.Logger.Warn("forbidden path access attempt", "path", path, "remote_addr", r.RemoteAddr)
		}
		http.Error(w, "Invalid path", status)
		return
	}

	f, err := os.Open(fullPath)
	if err != nil {
		if os.IsNotExist(err) {
			http.NotFound(w, r)
			return
		}
		s.Logger.Error("failed to open media file", "path", fullPath, "error", err)
		http.Error(w, "Unable to open media", http.StatusInternalServerError)
		return
	}
	defer f.Close()

	fi, err := f.Stat()
	if err != nil {
		s.Logger.Error("failed to stat media file", "path", fullPath, "error", err)
		http.Error(w, "Unable to read media metadata", http.StatusInternalServerError)
		return
	}
	if !fi.Mode().IsRegular() {
		http.Error(w, "Not a regular file", http.StatusBadRequest)
		return
	}

	if ct := contentTypeForPath(fullPath); ct != "" {
		w.Header().Set("Content-Type", ct)
	}
	w.Header().Set("Accept-Ranges", "bytes")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "public, max-age=3600")
	http.ServeContent(w, r, fi.Name(), fi.ModTime(), f)
}

// ServeHLS generates and serves M3U8/TS segments
func (s *Streamer) ServeHLS(w http.ResponseWriter, r *http.Request) {
	relPath := r.URL.Query().Get("path")
	if relPath == "" {
		http.Error(w, "Missing path parameter", http.StatusBadRequest)
		return
	}

	fullPath, err := s.resolveMediaPath(relPath)
	if err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, os.ErrPermission) {
			status = http.StatusForbidden
			s.Logger.Warn("forbidden HLS path access attempt", "path", relPath, "remote_addr", r.RemoteAddr)
		}
		http.Error(w, "Invalid path", status)
		return
	}

	hash := fmt.Sprintf("%x", md5.Sum([]byte(relPath)))
	hlsDir := filepath.Join(s.CacheRoot, hash)
	os.MkdirAll(hlsDir, 0755)

	m3u8Path := filepath.Join(hlsDir, "index.m3u8")

	if _, err := os.Stat(m3u8Path); os.IsNotExist(err) {
		s.Logger.Info("starting JIT HLS transcode", "path", relPath, "hash", hash)
		
		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Minute)
		defer cancel()
		
		go s.transcodeToHLS(ctx, fullPath, hlsDir, hash)
		
		timeout := time.After(15 * time.Second)
		ticker := time.NewTicker(500 * time.Millisecond)
		defer ticker.Stop()
		
		for {
			select {
			case <-timeout:
				http.Error(w, "Transcode timeout waiting for first segments", http.StatusServiceUnavailable)
				return
			case <-ticker.C:
				if _, err := os.Stat(m3u8Path); err == nil {
					goto serveFile
				}
			case <-ctx.Done():
				http.Error(w, "Request cancelled", http.StatusRequestTimeout)
				return
			}
		}
	}

serveFile:
	fileServer := http.FileServer(http.Dir(hlsDir))
	r.URL.Path = strings.TrimPrefix(r.URL.Path, "/hls")
	fileServer.ServeHTTP(w, r)
}

func (s *Streamer) transcodeToHLS(ctx context.Context, inputPath, outputDir, hash string) {
	defer func() {
		if r := recover(); r != nil {
			s.Logger.Error("transcode panic", "error", r)
		}
	}()

	cmd := exec.CommandContext(ctx, s.FFmpegPath,
		"-i", inputPath,
		"-codec:v", "libx264", "-preset", "veryfast", "-crf", "23",
		"-codec:a", "aac", "-b:a", "128k",
		"-f", "hls",
		"-hls_time", "10",
		"-hls_list_size", "0",
		"-hls_segment_filename", filepath.Join(outputDir, "seg_%03d.ts"),
		filepath.Join(outputDir, "index.m3u8"),
	)
	
	s.processes.Store(hash, cmd)
	defer s.processes.Delete(hash)

	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			s.Logger.Info("transcode cancelled", "hash", hash)
		} else {
			s.Logger.Error("ffmpeg error", "hash", hash, "error", err)
		}
	} else {
		s.Logger.Info("transcode completed", "hash", hash)
	}
}

func (s *Streamer) resolveMediaPath(relPath string) (string, error) {
	if relPath == "" {
		return "", fmt.Errorf("path required")
	}

	if strings.Contains(relPath, "..") {
		return "", fmt.Errorf("%w: path traversal detected", os.ErrPermission)
	}

	cleanRel := filepath.Clean(string(filepath.Separator) + relPath)
	cleanRel = strings.TrimPrefix(cleanRel, string(filepath.Separator))
	
	fullPath := filepath.Join(s.MediaRoot, cleanRel)
	fullPath = filepath.Clean(fullPath)
	
	absRoot, err := filepath.Abs(s.MediaRoot)
	if err != nil {
		return "", fmt.Errorf("invalid media root: %w", err)
	}
	
	absTarget, err := filepath.Abs(fullPath)
	if err != nil {
		return "", fmt.Errorf("invalid target path: %w", err)
	}
	
	rootWithSep := strings.TrimSuffix(absRoot, string(filepath.Separator)) + string(filepath.Separator)
	if !strings.HasPrefix(absTarget+string(filepath.Separator), rootWithSep) {
		return "", fmt.Errorf("%w: access outside media root", os.ErrPermission)
	}
	
	return absTarget, nil
}

func contentTypeForPath(path string) string {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".mp4":
		return "video/mp4"
	case ".mkv":
		return "video/x-matroska"
	case ".avi":
		return "video/x-msvideo"
	case ".mov":
		return "video/quicktime"
	case ".m3u8":
		return "application/vnd.apple.mpegurl"
	case ".ts":
		return "video/mp2t"
	default:
		return mime.TypeByExtension(ext)
	}
}
