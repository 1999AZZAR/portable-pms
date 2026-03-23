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
	"sync/atomic"

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
	var scanInProgress int32

	fmt.Printf("🚀 Starting Portable Media Streamer (Dark Watch UI)\n")
	fmt.Printf("📂 Media Path: %s\n", absPath)
	fmt.Printf("🌐 Address: http://localhost:%d\n", *port)

	// 2. Start Scanner (Async)
	go func() {
		sentinelPath := filepath.Join(metaDir, "scan_done")

		var totalRows int
		_ = database.QueryRow("SELECT COUNT(*) FROM media").Scan(&totalRows)

		var staleAbsRows int
		_ = database.QueryRow("SELECT COUNT(*) FROM media WHERE path LIKE '/%'").Scan(&staleAbsRows)

		needScan := false
		if _, err := os.Stat(sentinelPath); os.IsNotExist(err) {
			needScan = true
		}
		if totalRows == 0 {
			needScan = true
		}
		if staleAbsRows > 0 {
			fmt.Printf("🧹 Found %d stale absolute-path entries, refreshing index...\n", staleAbsRows)
			if _, err := database.Exec("DELETE FROM media WHERE path LIKE '/%'"); err != nil {
				fmt.Printf("❌ Failed to clean stale entries: %v\n", err)
			}
			_ = os.Remove(sentinelPath)
			needScan = true
		}

		if needScan {
			atomic.StoreInt32(&scanInProgress, 1)
			fmt.Println("🔍 Scanning for media (first run)...")
			s := scanner.New(absPath, database)
			if err := s.Start(); err != nil {
				fmt.Printf("❌ Scan error: %v\n", err)
			}
			// Create sentinel file
			_ = os.WriteFile(sentinelPath, []byte("done"), 0644)
			fmt.Println("✅ Scan complete, sentinel created")
			atomic.StoreInt32(&scanInProgress, 0)
		} else {
			atomic.StoreInt32(&scanInProgress, 0)
			fmt.Println("🔄 Skipping scan – metadata cached")
		}
	}()


	// 3. API Endpoints
	http.HandleFunc("/api/media", func(w http.ResponseWriter, r *http.Request) {
		rows, err := database.Query("SELECT id, path, type, category, title, size FROM media ORDER BY category, title, path")
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		defer rows.Close()

		var list []db.Media
		for rows.Next() {
			var m db.Media
			rows.Scan(&m.ID, &m.Path, &m.Type, &m.Category, &m.Title, &m.Size)
			if filepath.IsAbs(m.Path) {
				rel, err := filepath.Rel(absPath, m.Path)
				if err == nil && !strings.HasPrefix(rel, "..") {
					m.Path = rel
				}
			}
			list = append(list, m)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(list)
	})

	http.HandleFunc("/api/status", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]bool{
			"scanning": atomic.LoadInt32(&scanInProgress) == 1,
		})
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

	// 5. Dark watch-style UI
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
			<script src="/static/js/hls.min.js"></script>
			<style>
				@import url('https://fonts.googleapis.com/css2?family=Manrope:wght@500;700;800&display=swap');

				:root {
					--bg: #090909;
					--surface: #121214;
					--surface-soft: #1d1d22;
					--text: #f5f5f7;
					--muted: #9ea2aa;
					--accent: #ff2f2f;
					--accent-soft: #321616;
					--border: #2a2b31;
					--shell-pad: 12px;
					--playlist-card-min: 220px;
					--playlist-card-max: 300px;
					--rail-card-min: 260px;
					--rail-card-max: 330px;
					--glow: 0 14px 40px rgba(0, 0, 0, 0.45);
				}

				* {
					box-sizing: border-box;
				}

				body {
					background:
						radial-gradient(900px 560px at 90% -18%, rgba(255, 47, 47, 0.18) 0%, transparent 55%),
						radial-gradient(700px 520px at -5% 120%, rgba(255, 140, 0, 0.08) 0%, transparent 62%),
						linear-gradient(180deg, #111115 0%, var(--bg) 40%);
					color: var(--text);
					font-family: 'Manrope', sans-serif;
					margin: 0;
					min-height: 100vh;
					height: 100vh;
					overflow: hidden;
				}

				.app-shell {
					width: 100%;
					max-width: 100vw;
					margin: 0 auto;
					padding: var(--shell-pad);
					height: 100vh;
					display: flex;
					flex-direction: column;
				}

				.topbar {
					position: sticky;
					top: 0;
					z-index: 30;
					display: flex;
					align-items: center;
					justify-content: space-between;
					flex-wrap: wrap;
					gap: 12px;
					padding: 12px 14px;
					background: linear-gradient(135deg, rgba(22, 22, 27, 0.94) 0%, rgba(13, 13, 16, 0.9) 100%);
					border: 1px solid #35363d;
					border-radius: 16px;
					backdrop-filter: blur(12px);
					box-shadow: var(--glow), inset 0 1px 0 rgba(255, 255, 255, 0.04);
					margin-bottom: 16px;
				}

				.brand {
					font-weight: 800;
					font-size: clamp(0.95rem, 1.2vw, 1.1rem);
					letter-spacing: 0.4px;
					display: flex;
					align-items: center;
					gap: 10px;
				}

				.brand-dot {
					width: 12px;
					height: 12px;
					border-radius: 999px;
					background: var(--accent);
					box-shadow: 0 0 24px rgba(255, 47, 47, 0.72);
				}

				.brand-copy {
					display: grid;
					gap: 0;
					line-height: 1.1;
				}

				.brand-name {
					font-weight: 800;
					letter-spacing: 0.55px;
				}

				.brand-sub {
					font-size: 0.68rem;
					color: #a7abb3;
					text-transform: uppercase;
					letter-spacing: 0.8px;
				}

				.search-wrap {
					flex: 1;
					max-width: 760px;
					min-width: 260px;
					display: flex;
					gap: 8px;
					align-items: center;
				}

				.search-input {
					width: 100%;
					border: 1px solid #3c3f49;
					background: #0d0d11;
					border-radius: 999px;
					padding: clamp(8px, 1.2vw, 10px) 14px;
					color: #ececf0;
					font-size: clamp(0.8rem, 1vw, 0.88rem);
					outline: none;
				}

				.search-input:focus {
					border-color: #8d3737;
					box-shadow: 0 0 0 2px rgba(255, 47, 47, 0.2);
				}

				.layout {
					display: grid;
					grid-template-columns: minmax(0, 2fr) minmax(300px, 1fr);
					gap: 18px;
					flex: 1;
					height: auto;
					min-height: 0;
				}


				.panel {
					border: 1px solid var(--border);
					background: linear-gradient(160deg, rgba(20, 20, 24, 0.97) 0%, rgba(13, 13, 16, 0.96) 100%);
					border-radius: 16px;
					overflow: hidden;
					min-height: 0;
					box-shadow: var(--glow);
				}

				.player-wrap {
					padding: 14px;
					height: 100%;
					overflow-y: auto;
					overflow-x: hidden;
				}

				.video-frame {
					aspect-ratio: 16 / 9;
					background: #000;
					border-radius: 14px;
					overflow: hidden;
					border: 1px solid #3a3a42;
					box-shadow: 0 0 0 1px rgba(255, 255, 255, 0.03) inset;
				}

				.video-frame video {
					width: 100%;
					height: 100%;
					display: block;
					background: #000;
				}

				.now-playing {
					padding: 12px 2px 4px;
				}

				.now-playing h1 {
					font-size: clamp(1rem, 1.5vw, 1.18rem);
					font-weight: 800;
					margin: 0 0 6px;
					line-height: 1.35;
				}

				.now-playing p {
					color: var(--muted);
					margin: 0;
					font-size: clamp(0.8rem, 1.1vw, 0.9rem);
				}

				.meta-row {
					display: flex;
					flex-wrap: wrap;
					gap: 8px;
					margin-top: 10px;
				}

				.control-row {
					display: flex;
					flex-wrap: wrap;
					gap: 10px;
					margin-top: 12px;
				}

				.btn-control {
					border: 1px solid #3d3f46;
					background: linear-gradient(180deg, #262931 0%, #1e2027 100%);
					color: #efeff2;
					padding: 7px 12px;
					border-radius: 10px;
					font-size: clamp(0.75rem, 0.95vw, 0.82rem);
					font-weight: 700;
					cursor: pointer;
					transition: background 0.15s ease, border-color 0.15s ease, transform 0.15s ease;
					white-space: nowrap;
				}

				.btn-control:hover {
					background: linear-gradient(180deg, #2d313c 0%, #252933 100%);
					border-color: #575d6d;
					transform: translateY(-1px);
				}

				.btn-control.accent {
					background: linear-gradient(180deg, #5b1f1f 0%, #431717 100%);
					border-color: #9f3030;
					color: #ffd3d3;
				}

				.meta-chip {
					padding: 4px 10px;
					border-radius: 999px;
					border: 1px solid #3d3d43;
					background: var(--surface-soft);
					color: #d7d7dc;
					font-size: 0.78rem;
					font-weight: 700;
					text-transform: uppercase;
				}

				.sidebar-head {
					padding: 14px;
					border-bottom: 1px solid var(--border);
					display: grid;
					gap: 10px;
					background: linear-gradient(180deg, rgba(18, 18, 22, 0.9) 0%, rgba(14, 14, 18, 0.92) 100%);
					backdrop-filter: blur(8px);
				}

				.sidebar-head-top {
					display: flex;
					align-items: center;
					justify-content: space-between;
					gap: 8px;
				}

				.sidebar-head h2,
				.sidebar-head-top h2 {
					margin: 0;
					font-size: 0.96rem;
					font-weight: 800;
				}

				.playlist-tools {
					display: grid;
					grid-template-columns: 1fr 1fr 1fr auto;
					gap: 8px;
				}

				.playlist-select {
					border: 1px solid #3e414a;
					background: #111218;
					color: #e6e6eb;
					border-radius: 10px;
					padding: 8px 10px;
					font-size: clamp(0.75rem, 0.95vw, 0.82rem);
					font-weight: 700;
					min-width: 0;
				}

				#media-count {
					font-size: 0.78rem;
					padding: 3px 9px;
					border-radius: 999px;
					background: linear-gradient(180deg, #461a1a 0%, #311111 100%);
					color: #ffc1c1;
					border: 1px solid #8f2d2d;
					font-weight: 700;
				}

				#media-list {
					max-height: none;
					overflow-y: auto;
					overflow-x: hidden;
					padding: 8px;
					flex: 1;
				}

				aside.panel {
					display: flex;
					flex-direction: column;
				}

				.media-item {
					display: block;
					width: 100%;
					border: 1px solid #3a3d45;
					background: linear-gradient(180deg, #20232b 0%, #171a20 100%);
					color: var(--text);
					text-align: left;
					border-radius: 14px;
					padding: 10px;
					margin: 0 0 8px;
					cursor: pointer;
					transition: background 0.18s ease, border-color 0.18s ease, transform 0.18s ease;
				}

				.media-list.grid {
					display: grid;
					grid-template-columns: repeat(auto-fill, minmax(var(--playlist-card-min), var(--playlist-card-max)));
					gap: 10px;
					justify-content: center;
					align-content: start;
				}

				.media-list.grid .media-item {
					margin: 0;
					height: 100%;
					max-width: var(--playlist-card-max);
				}

				.media-item:hover {
					background: linear-gradient(180deg, #2d313c 0%, #232730 100%);
					border-color: #6e3a3a;
					transform: translateY(-2px);
					box-shadow: 0 8px 24px rgba(0, 0, 0, 0.35);
				}

				.media-item.active {
					background: linear-gradient(180deg, #542323 0%, #331818 100%);
					border-color: #a33b3b;
					box-shadow: 0 10px 26px rgba(0, 0, 0, 0.4);
				}

				.media-title {
					font-size: 0.94rem;
					font-weight: 700;
					line-height: 1.35;
					margin-bottom: 6px;
					word-break: break-word;
				}

				.media-sub {
					font-size: 0.78rem;
					color: var(--muted);
				}

				.recommend-title {
					margin-top: 14px;
				}

				.recommend-list {
					display: flex;
					gap: 12px;
					overflow-x: auto;
					padding-bottom: 8px;
					margin-top: 8px;
				}

				.recommend-card {
					flex: 0 0 auto;
					min-width: var(--rail-card-min);
					max-width: var(--rail-card-max);
					border: 1px solid #3b3e46;
					background: linear-gradient(180deg, #21242c 0%, #171a20 100%);
					color: #ececf0;
					border-radius: 14px;
					padding: 10px;
					text-align: left;
					cursor: pointer;
					transition: border-color 0.18s ease, transform 0.18s ease, background 0.18s ease;
				}

				.recommend-card:hover {
					background: linear-gradient(180deg, #303542 0%, #232833 100%);
					border-color: #834040;
					transform: translateY(-2px);
					box-shadow: 0 10px 24px rgba(0, 0, 0, 0.36);
				}

				.recommend-card .recommend-name {
					font-size: 0.9rem;
					font-weight: 700;
					line-height: 1.35;
					margin-bottom: 5px;
					word-break: break-word;
				}

				.recommend-card .recommend-meta {
					font-size: 0.78rem;
					color: var(--muted);
				}

				.recent-list {
					display: flex;
					gap: 12px;
					overflow-x: auto;
					padding-bottom: 8px;
					margin-top: 8px;
				}

				.recent-card {
					flex: 0 0 auto;
					min-width: var(--rail-card-min);
					max-width: var(--rail-card-max);
					border: 1px solid #393d46;
					background: linear-gradient(180deg, #1d2028 0%, #14171d 100%);
					color: #ececf0;
					border-radius: 14px;
					padding: 10px;
					text-align: left;
					cursor: pointer;
					transition: border-color 0.18s ease, transform 0.18s ease, background 0.18s ease;
				}

				.recent-card:hover {
					border-color: #7a3b3b;
					background: linear-gradient(180deg, #2c313d 0%, #222733 100%);
					transform: translateY(-2px);
					box-shadow: 0 10px 24px rgba(0, 0, 0, 0.34);
				}

				.recent-name {
					font-size: 0.9rem;
					font-weight: 700;
					line-height: 1.35;
					margin-bottom: 4px;
				}

				.recent-meta {
					font-size: 0.78rem;
					color: var(--muted);
				}

				.card-thumb {
					width: 100%;
					aspect-ratio: 16 / 9;
					border-radius: 10px;
					object-fit: cover;
					background: radial-gradient(120% 120% at 20% 10%, #3f4556 0%, #21252f 45%, #12141a 100%);
					border: 1px solid #424753;
					margin-bottom: 8px;
					display: block;
					box-shadow: inset 0 -28px 34px rgba(0, 0, 0, 0.2);
				}

				.card-kind {
					font-size: 0.7rem;
					font-weight: 800;
					letter-spacing: 0.4px;
					color: #ff9797;
					text-transform: uppercase;
					margin-bottom: 4px;
				}

				body[data-size="S"] .layout {
					grid-template-columns: minmax(0, 2.4fr) minmax(260px, 0.8fr);
					gap: 12px;
				}

				body[data-size="S"] {
					--playlist-card-min: 180px;
					--playlist-card-max: 230px;
					--rail-card-min: 210px;
					--rail-card-max: 260px;
				}

				body[data-size="M"] .layout {
					grid-template-columns: minmax(0, 2fr) minmax(320px, 1fr);
				}

				body[data-size="M"] {
					--playlist-card-min: 220px;
					--playlist-card-max: 300px;
					--rail-card-min: 260px;
					--rail-card-max: 330px;
				}

				body[data-size="L"] .layout {
					grid-template-columns: minmax(0, 1.6fr) minmax(420px, 1.2fr);
					gap: 20px;
				}

				body[data-size="L"] {
					--playlist-card-min: 250px;
					--playlist-card-max: 360px;
					--rail-card-min: 290px;
					--rail-card-max: 390px;
				}

				.empty-state {
					margin: 12px;
					border: 1px dashed #3d3d43;
					border-radius: 12px;
					padding: 26px;
					text-align: center;
					color: var(--muted);
					background: #141417;
				}

				.loading-dot {
					display: inline-block;
					width: 10px;
					height: 10px;
					border-radius: 999px;
					background: var(--accent);
					animation: pulse 1s infinite ease-in-out;
					margin-bottom: 10px;
				}

				@keyframes pulse {
					0% { opacity: 0.35; transform: scale(0.9); }
					50% { opacity: 1; transform: scale(1.05); }
					100% { opacity: 0.35; transform: scale(0.9); }
				}

				.section-title {
					font-size: 0.84rem;
					font-weight: 700;
					letter-spacing: 0.4px;
					text-transform: uppercase;
					color: var(--muted);
					margin-bottom: 8px;
					display: flex;
					align-items: center;
					gap: 8px;
				}

				.section-title::before {
					content: "";
					display: inline-block;
					width: 12px;
					height: 3px;
					border-radius: 999px;
					background: linear-gradient(90deg, #ff3a3a 0%, #ff8a00 100%);
				}

				@media (max-width: 1024px) {
					.topbar {
						align-items: flex-start;
					}

					.search-wrap {
						order: 3;
						flex-basis: 100%;
						max-width: none;
					}

					.playlist-tools {
						grid-template-columns: 1fr 1fr;
					}

					.playlist-tools > :last-child {
						grid-column: 1 / -1;
					}

					.layout {
						grid-template-columns: 1fr;
						height: auto;
					}

					#media-list {
						max-height: 54vh;
					}

					.recommend-card,
					.recent-card {
						min-width: 220px;
						max-width: 260px;
					}
				}

				@media (max-width: 640px) {
					body {
						overflow: auto;
					}

					.app-shell {
						padding: 10px;
						height: auto;
					}

					.topbar {
						padding: 10px;
						border-radius: 12px;
					}

					.search-wrap {
						max-width: none;
						min-width: 0;
					}

					.player-wrap {
						padding: 10px;
					}

					.video-frame {
						border-radius: 10px;
					}

					.playlist-tools {
						grid-template-columns: 1fr;
					}

					.playlist-tools > :last-child {
						grid-column: auto;
					}

					.recommend-card,
					.recent-card {
						min-width: 180px;
						max-width: 220px;
					}

					.card-thumb {
						border-radius: 8px;
					}
				}

				#media-list,
				.player-wrap,
				.recommend-list,
				.recent-list {
					scrollbar-width: none;
				}
				#media-list:hover,
				.player-wrap:hover,
				.recommend-list:hover,
				.recent-list:hover {
					scrollbar-width: thin;
					scrollbar-color: rgba(255, 255, 255, 0.12) transparent;
				}
				#media-list::-webkit-scrollbar,
				.player-wrap::-webkit-scrollbar,
				.recommend-list::-webkit-scrollbar,
				.recent-list::-webkit-scrollbar {
					width: 0;
					height: 0;
				}
				#media-list:hover::-webkit-scrollbar,
				.player-wrap:hover::-webkit-scrollbar,
				.recommend-list:hover::-webkit-scrollbar,
				.recent-list:hover::-webkit-scrollbar {
					width: 10px;
					height: 10px;
				}
				#media-list::-webkit-scrollbar-track,
				.player-wrap::-webkit-scrollbar-track,
				.recommend-list::-webkit-scrollbar-track,
				.recent-list::-webkit-scrollbar-track {
					background: transparent;
				}
				#media-list::-webkit-scrollbar-thumb,
				.player-wrap::-webkit-scrollbar-thumb,
				.recommend-list::-webkit-scrollbar-thumb,
				.recent-list::-webkit-scrollbar-thumb {
					background: transparent;
					border-radius: 999px;
					border: 2px solid transparent;
				}
				#media-list:hover::-webkit-scrollbar-thumb,
				.player-wrap:hover::-webkit-scrollbar-thumb,
				.recommend-list:hover::-webkit-scrollbar-thumb,
				.recent-list:hover::-webkit-scrollbar-thumb {
					background: rgba(255, 255, 255, 0.08);
				}
			</style>
		</head>
		<body>
			<div class="app-shell">
				<header class="topbar">
					<div class="brand">
						<span class="brand-dot"></span>
						<div class="brand-copy">
							<span class="brand-name">PMS</span>
							<span class="brand-sub">Portable Media Streamer</span>
						</div>
					</div>
					<div class="search-wrap">
						<input id="search-input" class="search-input" type="search" placeholder="Search title, series, category... (Press /)">
						<button id="btn-clear-search" class="btn-control" type="button">Clear</button>
					</div>
				</header>

				<div class="layout">
					<main class="panel">
						<div class="player-wrap">
							<div class="video-frame">
								<video id="video-player" controls playsinline preload="metadata"></video>
							</div>
							<div class="now-playing">
								<h1 id="now-playing-title">No media selected</h1>
								<p id="now-playing-meta">Pick an item from the list to start streaming.</p>
								<div class="meta-row">
									<span class="meta-chip" id="meta-type">-</span>
									<span class="meta-chip" id="meta-category">-</span>
									<span class="meta-chip" id="meta-episode">EP -</span>
								</div>
								<div class="control-row">
									<button id="btn-prev" class="btn-control" type="button">Prev</button>
									<button id="btn-next" class="btn-control" type="button">Next</button>
									<button id="btn-autoplay" class="btn-control accent" type="button">Autoplay: ON</button>
									<button id="btn-play-random" class="btn-control" type="button">Play Random</button>
								</div>
								<div class="section-title recommend-title">Recommended</div>
								<div id="recommend-list" class="recommend-list"></div>
								<div class="section-title recommend-title">Recently Played</div>
								<div id="recent-list" class="recent-list"></div>
							</div>
						</div>
					</main>

					<aside class="panel">
						<div class="sidebar-head">
							<div class="sidebar-head-top">
								<h2>Playlist</h2>
								<span id="media-count">0 items</span>
							</div>
							<div class="playlist-tools">
								<select id="root-select" class="playlist-select">
									<option value="">All roots</option>
								</select>
								<select id="series-select" class="playlist-select">
									<option value="">All</option>
								</select>
								<select id="type-select" class="playlist-select">
									<option value="">All types</option>
								</select>
								<button id="btn-shuffle" class="btn-control" type="button">Shuffle</button>
							</div>
						</div>
						<div id="media-list" class="media-list">
							<div class="empty-state">
								<div class="loading-dot"></div>
								<div>Scanning media library...</div>
							</div>
						</div>
					</aside>
				</div>
			</div>

			<script>
				const video = document.getElementById('video-player');
				const listDiv = document.getElementById('media-list');
				const playTitle = document.getElementById('now-playing-title');
				const playMeta = document.getElementById('now-playing-meta');
				const metaType = document.getElementById('meta-type');
				const metaCategory = document.getElementById('meta-category');
				const metaEpisode = document.getElementById('meta-episode');
				const mediaCount = document.getElementById('media-count');
				const rootSelect = document.getElementById('root-select');
				const seriesSelect = document.getElementById('series-select');
				const typeSelect = document.getElementById('type-select');
				const searchInput = document.getElementById('search-input');
				const btnClearSearch = document.getElementById('btn-clear-search');
				const btnPrev = document.getElementById('btn-prev');
				const btnNext = document.getElementById('btn-next');
				const btnAutoplay = document.getElementById('btn-autoplay');
				const btnPlayRandom = document.getElementById('btn-play-random');
				const btnShuffle = document.getElementById('btn-shuffle');
				const recommendList = document.getElementById('recommend-list');
				const recentList = document.getElementById('recent-list');
				let mediaData = [];
				let currentQueue = [];
				let currentIndex = -1;
				let autoplayNext = true;
				let lastPlayedPath = '';
				let hlsInstance = null;
				let recentPaths = [];
				let recommendationAnchorPath = '';
				let recommendationPaths = [];

				function esc(input) {
					return String(input || '')
						.replace(/&/g, '&amp;')
						.replace(/</g, '&lt;')
						.replace(/>/g, '&gt;')
						.replace(/"/g, '&quot;')
						.replace(/'/g, '&#39;');
				}

				function setSizeMode(mode) {
					document.body.setAttribute('data-size', mode);
				}

				function chooseAutoSizeMode() {
					const w = window.innerWidth;
					const h = window.innerHeight;
					if (w < 1200 || h < 760) {
						return 'S';
					}
					if (w > 1850 && h > 860) {
						return 'L';
					}
					return 'M';
				}

				function applyAutoSizeMode() {
					setSizeMode(chooseAutoSizeMode());
				}

				function fileNameFromPath(path) {
					if (!path) return '';
					const normalized = String(path).replace(/\\\\/g, '/');
					const parts = normalized.split('/');
					return parts[parts.length - 1] || '';
				}

				function trimExt(name) {
					return String(name || '').replace(/\.[^.]+$/, '');
				}

				function parentDir(path) {
					const normalized = String(path || '').replace(/\\\\/g, '/');
					const idx = normalized.lastIndexOf('/');
					if (idx < 0) return '';
					return normalized.slice(0, idx);
				}

				function destroyHls() {
					if (hlsInstance) {
						try {
							hlsInstance.destroy();
						} catch (_) {}
						hlsInstance = null;
					}
				}

				function playViaHLS(path) {
					const hlsUrl = '/hls/index.m3u8?path=' + encodeURIComponent(path);
					destroyHls();
					if (window.Hls && Hls.isSupported()) {
						hlsInstance = new Hls({
							enableWorker: true,
							lowLatencyMode: false,
						});
						hlsInstance.loadSource(hlsUrl);
						hlsInstance.attachMedia(video);
						hlsInstance.on(Hls.Events.MANIFEST_PARSED, function() {
							video.play().catch(function(){});
						});
						return;
					}
					if (video.canPlayType('application/vnd.apple.mpegurl')) {
						video.src = hlsUrl;
						video.load();
						video.play().catch(function(){});
					}
				}

				function playWithFallback(path) {
					const directUrl = '/stream?path=' + encodeURIComponent(path);
					destroyHls();
					video.src = directUrl;
					video.load();
					video.play().catch(function(){});

					video.addEventListener('error', function onDirectError() {
						video.removeEventListener('error', onDirectError);
						playViaHLS(path);
					}, { once: true });

					setTimeout(function() {
						if (video.networkState === video.NETWORK_NO_SOURCE) {
							playViaHLS(path);
						}
					}, 2500);
				}

				function parseEpisodeNumber(name) {
					const n = String(name || '').toLowerCase();
					let m = n.match(/s(\d{1,2})[ ._-]*e(\d{1,3})/i);
					if (m) {
						return Number(m[2]);
					}
					m = n.match(/(?:ep|episode)[ ._-]?(\d{1,3})/i);
					if (m) {
						return Number(m[1]);
					}
					m = n.match(/(?:^|\\D)(\d{1,3})(?:\\D|$)/);
					if (m) {
						return Number(m[1]);
					}
					return null;
				}

				function normalizeMedia(item, idx) {
					const filename = fileNameFromPath(item.Path || '');
					const isJav = String(item.Type || '').toLowerCase() === 'jav' || /\/jav$/i.test(String(item.Category || '').replace(/\\\\/g, '/'));
					const isArtist = !isJav && (String(item.Type || '').toLowerCase() === 'artist' || /\/pornstarts$/i.test(String(item.Category || '').replace(/\\\\/g, '/')) || /\/uc$/i.test(String(item.Category || '').replace(/\\\\/g, '/')));
					let episodeTitle = trimExt(filename) || item.Title || 'Untitled';
					let episodeNo = parseEpisodeNumber(episodeTitle);
					let series = item.Type === 'collection'
						? ((item.Category || 'General') + ' / ' + (item.Title || 'Series'))
						: (item.Category || 'General');

					if (isJav) {
						episodeTitle = item.Title || episodeTitle;
						episodeNo = null;
						series = item.Category || 'JAV';
					}
					if (isArtist) {
						series = (item.Category || 'General') + ' / ' + (item.Title || 'Artist');
					}

					const coverBase = parentDir(item.Path || '');
					const coverPath = coverBase ? (coverBase + '/cover.jpg') : '';
					const root = String(item.Category || 'General').split('/')[0] || 'General';

					return {
						raw: item,
						idx: idx,
						path: item.Path,
						type: item.Type || 'video',
						category: item.Category || 'General',
						title: item.Title || 'Untitled',
						episodeTitle: episodeTitle,
						episodeNo: episodeNo,
						series: series,
						isJav: isJav,
						isArtist: isArtist,
						coverPath: coverPath,
						root: root,
					};
				}

				function compareEpisodes(a, b) {
					if (a.isJav && b.isJav) {
						return a.episodeTitle.localeCompare(b.episodeTitle, undefined, { numeric: true, sensitivity: 'base' });
					}
					if (a.episodeNo !== null && b.episodeNo !== null && a.episodeNo !== b.episodeNo) {
						return a.episodeNo - b.episodeNo;
					}
					if (a.episodeNo !== null && b.episodeNo === null) return -1;
					if (a.episodeNo === null && b.episodeNo !== null) return 1;
					return a.episodeTitle.localeCompare(b.episodeTitle, undefined, { sensitivity: 'base' });
				}

				function rebuildSeriesOptions() {
					const selectedRoot = rootSelect.value;
					const seriesSet = {};
					mediaData.forEach(function(m) {
						if (!selectedRoot || m.root === selectedRoot) {
							seriesSet[m.series] = true;
						}
					});
					const allSeries = Object.keys(seriesSet).sort(function(a, b) {
						return a.localeCompare(b, undefined, { sensitivity: 'base' });
					});

					const current = seriesSelect.value;
					seriesSelect.innerHTML = '<option value="">All playlists</option>' + allSeries.map(function(s) {
						return '<option value="' + esc(s) + '">' + esc(s) + '</option>';
					}).join('');
					seriesSelect.value = allSeries.indexOf(current) >= 0 ? current : '';
				}

				function rebuildRootOptions() {
					const rootSet = {};
					mediaData.forEach(function(m) { rootSet[m.root] = true; });
					const roots = Object.keys(rootSet).sort(function(a, b) {
						return a.localeCompare(b, undefined, { sensitivity: 'base' });
					});
					const current = rootSelect.value;
					rootSelect.innerHTML = '<option value="">All roots</option>' + roots.map(function(r) {
						return '<option value="' + esc(r) + '">' + esc(r) + '</option>';
					}).join('');
					rootSelect.value = roots.indexOf(current) >= 0 ? current : '';
				}

				function rebuildTypeOptions() {
					const typeSet = {};
					mediaData.forEach(function(m) { typeSet[m.type] = true; });
					const allTypes = Object.keys(typeSet).sort(function(a, b) {
						return a.localeCompare(b, undefined, { sensitivity: 'base' });
					});
					const current = typeSelect.value;
					typeSelect.innerHTML = '<option value="">All types</option>' + allTypes.map(function(t) {
						return '<option value="' + esc(t) + '">' + esc(t) + '</option>';
					}).join('');
					typeSelect.value = allTypes.indexOf(current) >= 0 ? current : '';
				}

				function refreshQueue(selectFirst) {
					buildQueue();
					if (lastPlayedPath) {
						syncActiveFromPath(lastPlayedPath);
					}
					if (currentIndex < 0 && currentQueue.length > 0) {
						currentIndex = 0;
					}
					renderQueue();
					if (selectFirst && currentIndex >= 0) {
						selectItemByQueueIndex(currentIndex);
					}
				}

				function buildQueue() {
					const selectedRoot = rootSelect.value;
					const selectedSeries = seriesSelect.value;
					const selectedType = typeSelect.value;
					const q = String(searchInput.value || '').trim().toLowerCase();
					let queue = mediaData.slice();
					if (selectedRoot) {
						queue = queue.filter(function(m) { return m.root === selectedRoot; });
					}
					if (selectedSeries) {
						queue = queue.filter(function(m) { return m.series === selectedSeries; });
					}
					if (selectedType) {
						queue = queue.filter(function(m) { return m.type === selectedType; });
					}
					if (q) {
						queue = queue.filter(function(m) {
							const hay = (m.episodeTitle + ' ' + m.series + ' ' + m.category + ' ' + m.type).toLowerCase();
							return hay.indexOf(q) >= 0;
						});
					}
					queue.sort(compareEpisodes);
					currentQueue = queue;
					mediaCount.textContent = queue.length + ' / ' + mediaData.length;
				}

				function renderQueue() {
					if (currentQueue.length === 0) {
						listDiv.classList.remove('grid');
						listDiv.innerHTML = '<div class="empty-state">No match for current filters.</div>';
						renderRecommendations();
						renderRecent();
						return;
					}
					const useGrid = window.innerWidth > 640;
					listDiv.classList.toggle('grid', useGrid);

					listDiv.innerHTML = currentQueue.map(function(m, i) {
						const ep = m.episodeNo !== null ? ('EP ' + m.episodeNo) : 'EP -';
						const sub = m.isJav ? (m.series + ' • CODE') : (m.isArtist ? (m.series + ' • CLIP') : (m.series + ' • ' + ep));
						const kind = m.isJav ? 'JAV' : (m.isArtist ? 'Artist' : 'Episode');
						return '' +
							'<button class="media-item' + (i === currentIndex ? ' active' : '') + '" data-qidx="' + i + '">' +
								cardThumbHtml(m) +
								'<div class="card-kind">' + esc(kind) + '</div>' +
								'<div class="media-title">' + esc(m.episodeTitle) + '</div>' +
								'<div class="media-sub">' + esc(sub) + '</div>' +
							'</button>';
					}).join('');

					document.querySelectorAll('.media-item').forEach(function(el) {
						el.addEventListener('click', function() {
							const idx = Number(el.getAttribute('data-qidx'));
							selectItemByQueueIndex(idx);
						});
					});

					const activeEl = document.querySelector('.media-item.active');
					if (activeEl) {
						activeEl.scrollIntoView({ block: 'nearest' });
					}
					renderRecommendations();
					renderRecent();
				}

				function sampleRecommendations(limit) {
					const activePath = (currentIndex >= 0 && currentQueue[currentIndex]) ? currentQueue[currentIndex].path : lastPlayedPath;
					let pool = currentQueue.slice();
					if (activePath) {
						const active = currentQueue.find(function(m) { return m.path === activePath; });
						if (active) {
							const sameSeries = pool.filter(function(m) { return m.path !== activePath && m.series === active.series; });
							const sameCategory = pool.filter(function(m) { return m.path !== activePath && m.category === active.category; });
							const sameType = pool.filter(function(m) { return m.path !== activePath && m.type === active.type; });
							const mix = sameSeries.concat(sameCategory, sameType, pool);
							const seen = {};
							pool = mix.filter(function(m) {
								if (m.path === activePath || seen[m.path]) return false;
								seen[m.path] = true;
								return true;
							});
						}
					}
					if (pool.length === 0) {
						pool = currentQueue.filter(function(m) { return m.path !== activePath; });
					}
					for (let i = pool.length - 1; i > 0; i--) {
						const j = Math.floor(Math.random() * (i + 1));
						const tmp = pool[i];
						pool[i] = pool[j];
						pool[j] = tmp;
					}
					return pool.slice(0, limit);
				}

				function refreshRecommendationsForCurrent() {
					const activePath = (currentIndex >= 0 && currentQueue[currentIndex]) ? currentQueue[currentIndex].path : lastPlayedPath;
					recommendationAnchorPath = activePath || '';
					recommendationPaths = sampleRecommendations(8).map(function(m) { return m.path; });
				}

				function cardThumbHtml(m) {
					if (m.isJav && m.coverPath) {
						return '<img class="card-thumb" src="/stream?path=' + encodeURIComponent(m.coverPath) + '" alt="' + esc(m.episodeTitle || m.title) + '" loading="lazy" onerror="this.remove()">';
					}
					return '<div class="card-thumb"></div>';
				}

				function renderRecommendations() {
					if (!currentQueue.length) {
						recommendList.innerHTML = '<div class="media-sub">No recommendations yet.</div>';
						return;
					}
					const activePath = (currentIndex >= 0 && currentQueue[currentIndex]) ? currentQueue[currentIndex].path : lastPlayedPath;
					if (!recommendationPaths.length || recommendationAnchorPath !== activePath) {
						refreshRecommendationsForCurrent();
					}
					const map = {};
					mediaData.forEach(function(m) { map[m.path] = m; });
					const picks = recommendationPaths.map(function(p) { return map[p]; }).filter(Boolean).slice(0, 8);
					recommendList.innerHTML = picks.map(function(m) {
						const kind = m.isJav ? 'JAV' : (m.isArtist ? 'Artist' : 'Episode');
						return '' +
							'<button class="recommend-card" data-rec-path="' + esc(m.path) + '">' +
								cardThumbHtml(m) +
								'<div class="card-kind">' + esc(kind) + '</div>' +
								'<div class="recommend-name">' + esc(m.episodeTitle || m.title) + '</div>' +
								'<div class="recommend-meta">' + esc(m.series || m.category || '-') + '</div>' +
							'</button>';
					}).join('');
					document.querySelectorAll('.recommend-card').forEach(function(el) {
						el.addEventListener('click', function() {
							const path = el.getAttribute('data-rec-path');
							const idx = currentQueue.findIndex(function(m) { return m.path === path; });
							if (idx >= 0) {
								selectItemByQueueIndex(idx);
								return;
							}
							const globalIdx = mediaData.findIndex(function(m) { return m.path === path; });
							if (globalIdx >= 0) {
								applySelected(mediaData[globalIdx]);
							}
						});
					});
				}

				function markRecentPlayed(path) {
					if (!path) return;
					recentPaths = recentPaths.filter(function(p) { return p !== path; });
					recentPaths.unshift(path);
					if (recentPaths.length > 18) {
						recentPaths = recentPaths.slice(0, 18);
					}
				}

				function renderRecent() {
					if (!recentPaths.length) {
						recentList.innerHTML = '<div class="media-sub">No recent plays yet.</div>';
						return;
					}
					const map = {};
					mediaData.forEach(function(m) { map[m.path] = m; });
					const items = recentPaths.map(function(p) { return map[p]; }).filter(Boolean).slice(0, 10);
					recentList.innerHTML = items.map(function(m) {
						const kind = m.isJav ? 'JAV' : (m.isArtist ? 'Artist' : 'Episode');
						return '' +
							'<button class="recent-card" data-recent-path="' + esc(m.path) + '">' +
								cardThumbHtml(m) +
								'<div class="card-kind">' + esc(kind) + '</div>' +
								'<div class="recent-name">' + esc(m.episodeTitle || m.title) + '</div>' +
								'<div class="recent-meta">' + esc(m.series || m.category || '-') + '</div>' +
							'</button>';
					}).join('');
					document.querySelectorAll('.recent-card').forEach(function(el) {
						el.addEventListener('click', function() {
							const p = el.getAttribute('data-recent-path');
							const idx = currentQueue.findIndex(function(m) { return m.path === p; });
							if (idx >= 0) {
								selectItemByQueueIndex(idx);
								return;
							}
							const globalIdx = mediaData.findIndex(function(m) { return m.path === p; });
							if (globalIdx >= 0) {
								applySelected(mediaData[globalIdx]);
							}
						});
					});
				}

				function syncActiveFromPath(path) {
					currentIndex = currentQueue.findIndex(function(m) { return m.path === path; });
				}

				function selectItem(idx) {
					const m = mediaData.find(function(x) { return x.idx === idx; });
					if (!m) {
						return;
					}
					syncActiveFromPath(m.path);
					renderQueue();
					applySelected(m);
				}

				function selectItemByQueueIndex(qIdx) {
					const m = currentQueue[qIdx];
					if (!m) {
						return;
					}
					currentIndex = qIdx;
					renderQueue();
					applySelected(m);
				}

				function applySelected(m) {
					playTitle.textContent = m.episodeTitle || m.title || 'Untitled';
					playMeta.textContent = (m.series || '-') + ' / ' + (m.type || '-');
					metaType.textContent = (m.type || '-').toUpperCase();
					metaCategory.textContent = (m.category || '-').toUpperCase();
					metaEpisode.textContent = m.isJav ? 'CODE' : (m.isArtist ? 'CLIP' : (m.episodeNo !== null ? ('EP ' + m.episodeNo) : 'EP -'));
					lastPlayedPath = m.path;
					markRecentPlayed(m.path);
					refreshRecommendationsForCurrent();
					playWithFallback(m.path);
					renderRecommendations();
					renderRecent();
				}

				function playRandom() {
					if (!currentQueue.length) return;
					const idx = Math.floor(Math.random() * currentQueue.length);
					selectItemByQueueIndex(idx);
				}

				function playNext() {
					if (currentQueue.length === 0) return;
					let nextIdx = currentIndex + 1;
					if (nextIdx >= currentQueue.length) {
						nextIdx = 0;
					}
					selectItemByQueueIndex(nextIdx);
				}

				function playPrev() {
					if (currentQueue.length === 0) return;
					let prevIdx = currentIndex - 1;
					if (prevIdx < 0) {
						prevIdx = currentQueue.length - 1;
					}
					selectItemByQueueIndex(prevIdx);
				}

				function shuffleQueue() {
					for (let i = currentQueue.length - 1; i > 0; i--) {
						const j = Math.floor(Math.random() * (i + 1));
						const t = currentQueue[i];
						currentQueue[i] = currentQueue[j];
						currentQueue[j] = t;
					}
					currentIndex = 0;
					renderQueue();
					selectItemByQueueIndex(currentIndex);
				}

				async function fetchMedia() {
					try {
						const mediaRes = await fetch('/api/media');
						const data = await mediaRes.json();
						let scanning = false;
						try {
							const statusRes = await fetch('/api/status');
							if (statusRes.ok) {
								const status = await statusRes.json();
								scanning = !!(status && status.scanning);
							}
						} catch (_) {
							// Status endpoint is optional for backward compatibility.
						}
						if(!Array.isArray(data) || data.length === 0) {
							mediaData = [];
							currentQueue = [];
							currentIndex = -1;
							mediaCount.textContent = '0 items';
							listDiv.innerHTML = scanning
								? '<div class="empty-state"><div class="loading-dot"></div><div>Scanning media library...</div></div>'
								: '<div class="empty-state">No media found.</div>';
							return;
						}

						const previousPath = lastPlayedPath || (video.src ? decodeURIComponent((video.src.split('path=')[1] || '').split('&')[0] || '') : '');

						mediaData = data.map(normalizeMedia);
						rebuildRootOptions();
						rebuildSeriesOptions();
						rebuildTypeOptions();
						buildQueue();

						if (previousPath) {
							syncActiveFromPath(previousPath);
						}
						if (currentIndex < 0 && currentQueue.length > 0) {
							currentIndex = 0;
						}

						renderQueue();
						if (!video.src && currentQueue.length > 0) {
							selectItemByQueueIndex(currentIndex);
						}
					} catch(e) {
						listDiv.innerHTML = '<div class="empty-state">Failed to load media list.</div>';
					}
				}

				btnPrev.addEventListener('click', playPrev);
				btnNext.addEventListener('click', playNext);
				btnPlayRandom.addEventListener('click', playRandom);
				btnShuffle.addEventListener('click', shuffleQueue);
				btnAutoplay.addEventListener('click', function() {
					autoplayNext = !autoplayNext;
					btnAutoplay.textContent = autoplayNext ? 'Autoplay: ON' : 'Autoplay: OFF';
				});
				video.addEventListener('ended', function() {
					if (autoplayNext) {
						playNext();
					}
				});
				rootSelect.addEventListener('change', function() {
					rebuildSeriesOptions();
					currentIndex = -1;
					refreshQueue(true);
				});
				seriesSelect.addEventListener('change', function() {
					currentIndex = -1;
					refreshQueue(true);
				});
				typeSelect.addEventListener('change', function() {
					currentIndex = -1;
					refreshQueue(true);
				});
				searchInput.addEventListener('input', function() {
					refreshQueue(false);
				});
				searchInput.addEventListener('keydown', function(e) {
					if (e.key === 'Enter' && currentQueue.length > 0) {
						selectItemByQueueIndex(currentIndex >= 0 ? currentIndex : 0);
					}
				});
				btnClearSearch.addEventListener('click', function() {
					searchInput.value = '';
					searchInput.focus();
					refreshQueue(false);
				});
				document.addEventListener('keydown', function(e) {
					if (e.target && (e.target.tagName === 'INPUT' || e.target.tagName === 'SELECT' || e.target.tagName === 'TEXTAREA')) {
						return;
					}
					if (e.key === '/') {
						e.preventDefault();
						searchInput.focus();
						searchInput.select();
						return;
					}
					if (e.key === 'ArrowRight' || e.key.toLowerCase() === 'j') {
						playNext();
						return;
					}
					if (e.key === 'ArrowLeft' || e.key.toLowerCase() === 'k') {
						playPrev();
					}
				});

				applyAutoSizeMode();
				window.addEventListener('resize', function() {
					applyAutoSizeMode();
					renderQueue();
				});
				fetchMedia();
				// Refresh list periodically (faster while scanning)
				setInterval(fetchMedia, 5000);
			</script>
		</body>
		</html>`
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, html)
	})

	log.Fatal(http.ListenAndServe(fmt.Sprintf(":%d", *port), nil))
}
