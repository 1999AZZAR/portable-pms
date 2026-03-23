package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
)

func main() {
	mediaPath := flag.String("path", ".", "Path to media directory")
	port := flag.Int("port", 8080, "Server port")
	flag.Parse()

	absPath, err := filepath.Abs(*mediaPath)
	if err != nil {
		log.Fatalf("Invalid path: %v", err)
	}

	fmt.Printf("🚀 Starting Portable Media Streamer\n")
	fmt.Printf("📂 Media Path: %s\n", absPath)
	fmt.Printf("🌐 Address: http://localhost:%d\n", *port)

	// Minimal check for FFmpeg in local bin
	ffmpegPath := filepath.Join(".", "bin", "ffmpeg")
	if _, err := os.Stat(ffmpegPath); os.IsNotExist(err) {
		fmt.Printf("⚠️ Warning: FFmpeg not found in ./bin/ffmpeg. Transcoding will use system ffmpeg.\n")
	}

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "Welcome to Portable Media Streamer (PMS)\nMaster: Azzar Budiyanto\nStatus: Core Initialized")
	})

	log.Fatal(http.ListenAndServe(fmt.Sprintf(":%d", *port), nil))
}
