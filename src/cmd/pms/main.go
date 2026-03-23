package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"

	"github.com/1999AZZAR/portable-pms/src/internal/db"
	"github.com/1999AZZAR/portable-pms/src/internal/scanner"
	"github.com/1999AZZAR/portable-pms/src/internal/streamer"
)

func main() {
	mediaPath := flag.String("path", ".", "Path to media directory")
	port := flag.Int("port", 8080, "Server port")
	flag.Parse()

	absPath, err := filepath.Abs(*mediaPath)
	if err != nil {
		log.Fatalf("Invalid path: %v", err)
	}

	// 1. Init Database in .metadata folder
	metaDir := filepath.Join(absPath, ".metadata")
	os.MkdirAll(metaDir, 0755)
	database, err := db.InitDB(filepath.Join(metaDir, "pms.db"))
	if err != nil {
		log.Fatalf("DB Init failed: %v", err)
	}

	st := streamer.New(absPath)

	fmt.Printf("🚀 Starting Portable Media Streamer\n")
	fmt.Printf("📂 Media Path: %s\n", absPath)
	fmt.Printf("🌐 Address: http://localhost:%d\n", *port)

	// 2. Start Scanner (Async)
	go func() {
		fmt.Printf("🔍 Scanning for media...\n")
		s := scanner.New(absPath, database)
		if err := s.Start(); err != nil {
			fmt.Printf("❌ Scanner error: %v\n", err)
		}
		fmt.Printf("✨ Scanning complete!\n")
	}()

	// 3. API Endpoints
	http.HandleFunc("/api/media", func(w http.ResponseWriter, r *http.Request) {
		rows, err := database.Query("SELECT id, path, type, category, title, size FROM media")
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		defer rows.Close()

		var list []db.Media
		for rows.Next() {
			var m db.Media
			rows.Scan(&m.ID, &m.Path, &m.Type, &m.Category, &m.Title, &m.Size)
			// Send relative path to frontend for security
			rel, _ := filepath.Rel(absPath, m.Path)
			m.Path = rel
			list = append(list, m)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(list)
	})

	http.HandleFunc("/stream", st.ServeMedia)

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "Welcome to PMS\nAPI: /api/media\nStream: /stream?path=relative/path/to/video.mp4")
	})

	log.Fatal(http.ListenAndServe(fmt.Sprintf(":%d", *port), nil))
}
