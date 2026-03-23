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

	// 1. Init Database
	metaDir := filepath.Join(absPath, ".metadata")
	os.MkdirAll(metaDir, 0755)
	database, err := db.InitDB(filepath.Join(metaDir, "pms.db"))
	if err != nil {
		log.Fatalf("DB Init failed: %v", err)
	}

	st := streamer.New(absPath)

	fmt.Printf("🚀 Starting Portable Media Streamer (Neo-M3 Hybrid UI)\n")
	fmt.Printf("📂 Media Path: %s\n", absPath)
	fmt.Printf("🌐 Address: http://localhost:%d\n", *port)

	// 2. Start Scanner (Async)
	go func() {
		// Skip scan if metadata already exists
		if _, err := os.Stat(filepath.Join(metaDir, "scan_done")); os.IsNotExist(err) {
			fmt.Println("🔍 Scanning for media (first run)...")
			s := scanner.New(absPath, database)
			if err := s.Start(); err != nil {
				fmt.Printf("❌ Scan error: %v\n", err)
			}
			// Create sentinel file
			_ = os.WriteFile(filepath.Join(metaDir, "scan_done"), []byte("done"), 0644)
			fmt.Println("✅ Scan complete, sentinel created")
		} else {
			fmt.Println("🔄 Skipping scan – metadata cached")
		}
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

	// 4. Streaming & Static Endpoints
	http.HandleFunc("/stream", st.ServeMedia)
	http.HandleFunc("/hls/", st.ServeHLS)

	executable, _ := os.Executable()
	baseDir := filepath.Dir(executable)
	if _, err := os.Stat(filepath.Join(baseDir, "web", "static")); os.IsNotExist(err) {
		baseDir, _ = os.Getwd()
	}

	staticDir := filepath.Join(baseDir, "web", "static")
	http.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.Dir(staticDir))))

	// 5. Neo-M3 Hybrid UI (High Contrast, 3px Borders, Hard Shadows)
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		html := `
		<!DOCTYPE html>
		<html lang="en">
		<head>
			<meta charset="UTF-8">
			<meta name="viewport" content="width=device-width, initial-scale=1.0">
			<title>PMS - Portable Media Streamer</title>
			<link href="/static/css/bootstrap.min.css" rel="stylesheet">
			<link href="https://cdnjs.cloudflare.com/ajax/libs/font-awesome/6.4.0/css/all.min.css" rel="stylesheet">
			<script src="/static/js/hls.min.js"></script>
			<style>
				@import url('https://fonts.googleapis.com/css2?family=Plus+Jakarta+Sans:wght@400;600;800&display=swap');
				
				:root {
					--bg: #f0f0f0;
					--card-bg: #ffffff;
					--text: #1a1a1a;
					--accent: #ff4d4d;
					--border: #1a1a1a;
					--shadow: #1a1a1a;
				}

				body {
					background-color: var(--bg);
					color: var(--text);
					font-family: 'Plus Jakarta Sans', sans-serif;
					padding-bottom: 50px;
				}

				.m3-header {
					border-bottom: 3px solid var(--border);
					padding: 2rem 0;
					margin-bottom: 2rem;
					background: #fff;
				}

				.m3-title {
					font-weight: 800;
					font-size: 2.5rem;
					text-transform: uppercase;
					letter-spacing: -1px;
				}

				.m3-card {
					background: var(--card-bg);
					border: 3px solid var(--border);
					box-shadow: 6px 6px 0px var(--shadow);
					padding: 1.5rem;
					margin-bottom: 1.5rem;
					transition: all 0.2s ease;
					cursor: pointer;
				}

				.m3-card:hover {
					transform: translate(-2px, -2px);
					box-shadow: 10px 10px 0px var(--shadow);
				}

				.m3-card.active {
					background: var(--accent);
					color: #fff;
				}

				.video-container {
					border: 3px solid var(--border);
					box-shadow: 8px 8px 0px var(--shadow);
					background: #000;
					position: sticky;
					top: 20px;
				}

				.badge-m3 {
					border: 2px solid var(--border);
					padding: 4px 10px;
					font-weight: 700;
					text-transform: uppercase;
					font-size: 0.7rem;
					display: inline-block;
					margin-right: 5px;
					background: #fff;
					color: #000;
				}

				::-webkit-scrollbar { width: 10px; }
				::-webkit-scrollbar-track { background: var(--bg); }
				::-webkit-scrollbar-thumb { background: var(--border); }
			</style>
		</head>
		<body>
			<header class="m3-header">
				<div class="container d-flex justify-content-between align-items-center">
					<div class="m3-title"><i class="fa-solid fa-bolt"></i> PMS</div>
					<div class="fw-bold">WONG EDAN MAH AJAIB</div>
				</div>
			</header>

			<div class="container">
				<div class="row">
					<div class="col-lg-4">
						<div id="media-list">
							<div class="text-center p-5">
								<i class="fa-solid fa-circle-notch fa-spin fa-2x"></i>
								<p class="mt-2 fw-bold">SCANNING ARCHIVES...</p>
							</div>
						</div>
					</div>
					<div class="col-lg-8">
						<div class="video-container mb-4">
							<video id="video-player" controls class="w-100"></video>
						</div>
						<div class="m3-card">
							<h4 id="now-playing-title" class="fw-800">NO MEDIA SELECTED</h4>
							<p id="now-playing-meta" class="text-secondary fw-bold mb-0">Select a file from the list to start streaming.</p>
						</div>
					</div>
				</div>
			</div>

			<script>
				const video = document.getElementById('video-player');
				const listDiv = document.getElementById('media-list');
				const playTitle = document.getElementById('now-playing-title');
				const playMeta = document.getElementById('now-playing-meta');

				async function fetchMedia() {
					try {
						const res = await fetch('/api/media');
						const data = await res.json();
						if(!data || data.length === 0) {
							listDiv.innerHTML = '<div class="m3-card text-center">NO MEDIA FOUND</div>';
							return;
						}
						
						listDiv.innerHTML = data.map(m => ` + "`" + `
							<div class="m3-card" onclick="playMedia(this, '${m.Path}', '${m.Title}', '${m.Type}', '${m.Category}')">
								<div class="d-flex justify-content-between align-items-start mb-2">
									<span class="badge-m3">${m.Type}</span>
									<span class="badge-m3">${m.Category}</span>
								</div>
								<h5 class="fw-bold mb-0 text-truncate">${m.Title}</h5>
							</div>
						` + "`" + `).join('');
					} catch(e) {
						listDiv.innerHTML = '<div class="m3-card text-center text-danger">ERROR CONNECTING TO CORE</div>';
					}
				}

				function playMedia(el, path, title, type, category) {
					// UI State
					document.querySelectorAll('.m3-card').forEach(c => c.classList.remove('active'));
					el.classList.add('active');
					playTitle.innerText = title;
					playMeta.innerText = ` + "`" + `${category} / ${type}` + "`" + `;

					const hlsUrl = '/hls/index.m3u8?path=' + encodeURIComponent(path);
					
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

				fetchMedia();
				// Refresh list every 30s in case scanner adds new items
				setInterval(fetchMedia, 30000);
			</script>
		</body>
		</html>`
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, html)
	})

	log.Fatal(http.ListenAndServe(fmt.Sprintf(":%d", *port), nil))
}
