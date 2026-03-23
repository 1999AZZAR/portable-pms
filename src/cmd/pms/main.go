package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"

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

	// 1. Init Database
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
		s := scanner.New(absPath, database)
		s.Start()
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
			rel, _ := filepath.Rel(absPath, m.Path)
			m.Path = rel
			list = append(list, m)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(list)
	})

	// 4. Streaming Endpoints
	http.HandleFunc("/stream", st.ServeMedia)
	
	// Handle /hls/ and its sub-paths
	http.HandleFunc("/hls/", func(w http.ResponseWriter, r *http.Request) {
		// Example: /hls/index.m3u8?path=videos/test.mp4
		// Or subsequent: /hls/seg_001.ts?path=videos/test.mp4
		st.ServeHLS(w, r)
	})

	// 5. Minimal UI
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		html := `
		<!DOCTYPE html>
		<html>
		<head><title>PMS Player</title>
		<link href="https://cdn.jsdelivr.net/npm/bootstrap@5.3.0/dist/css/bootstrap.min.css" rel="stylesheet">
		<script src="https://cdn.jsdelivr.net/npm/hls.js@latest"></script>
		<style>body{background:#121212;color:#eee} .card{background:#1e1e1e;color:#fff;margin-bottom:10px}</style>
		</head>
		<body class="p-4">
			<div class="container">
				<h2 class="mb-4">🚀 Portable Media Streamer</h2>
				<div class="row">
					<div class="col-md-4" id="list">Loading...</div>
					<div class="col-md-8">
						<video id="video" controls class="w-100 border"></video>
						<div id="status" class="mt-2 small text-secondary">Select a video</div>
					</div>
				</div>
			</div>
			<script>
				const video = document.getElementById('video');
				const listDiv = document.getElementById('list');
				const status = document.getElementById('status');

				async function loadMedia() {
					const res = await fetch('/api/media');
					const data = await res.json();
					listDiv.innerHTML = data.map(m => ` + "`" + `
						<div class="card p-2" onclick="playHLS('${m.Path}')" style="cursor:pointer">
							<strong>${m.Title}</strong><br><small class="text-secondary">${m.Category} | ${m.Type}</small>
						</div>
					` + "`" + `).join('');
				}

				function playHLS(path) {
					const hlsUrl = '/hls/index.m3u8?path=' + encodeURIComponent(path);
					status.innerText = 'Playing: ' + path;
					if (Hls.isSupported()) {
						const hls = new Hls();
						hls.loadSource(hlsUrl);
						hls.attachMedia(video);
						hls.on(Hls.Events.MANIFEST_PARSED, () => video.play());
					} else if (video.canPlayType('application/vnd.apple.mpegurl')) {
						video.src = hlsUrl;
						video.onloadedmetadata = () => video.play();
					}
				}
				loadMedia();
			</script>
		</body>
		</html>`
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, html)
	})

	log.Fatal(http.ListenAndServe(fmt.Sprintf(":%d", *port), nil))
}
