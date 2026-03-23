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
			<style>
				@import url('https://fonts.googleapis.com/css2?family=Manrope:wght@500;700;800&display=swap');

				:root {
					--bg: #0f0f10;
					--surface: #1a1a1c;
					--surface-soft: #232327;
					--text: #f1f1f1;
					--muted: #a8a8ad;
					--accent: #ff3d3d;
					--accent-soft: #3a1f1f;
					--border: #2e2e33;
				}

				* {
					box-sizing: border-box;
				}

				body {
					background: radial-gradient(1000px 700px at 80% -10%, #2a2a35 0%, var(--bg) 45%);
					color: var(--text);
					font-family: 'Manrope', sans-serif;
					margin: 0;
					min-height: 100vh;
				}

				.app-shell {
					max-width: 1420px;
					margin: 0 auto;
					padding: 16px;
				}

				.topbar {
					position: sticky;
					top: 0;
					z-index: 30;
					display: flex;
					align-items: center;
					justify-content: space-between;
					gap: 12px;
					padding: 10px 14px;
					background: rgba(18, 18, 20, 0.85);
					border: 1px solid var(--border);
					border-radius: 14px;
					backdrop-filter: blur(10px);
					margin-bottom: 16px;
				}

				.brand {
					font-weight: 800;
					font-size: 1.1rem;
					letter-spacing: 0.4px;
					display: flex;
					align-items: center;
					gap: 10px;
				}

				.brand-dot {
					width: 10px;
					height: 10px;
					border-radius: 999px;
					background: var(--accent);
					box-shadow: 0 0 16px rgba(255, 61, 61, 0.65);
				}

				.search-wrap {
					flex: 1;
					max-width: 640px;
					display: flex;
					gap: 8px;
					align-items: center;
				}

				.search-input {
					width: 100%;
					border: 1px solid #38383f;
					background: #141417;
					border-radius: 999px;
					padding: 9px 14px;
					color: #ececf0;
					font-size: 0.88rem;
					outline: none;
				}

				.search-input:focus {
					border-color: #6a3737;
					box-shadow: 0 0 0 2px rgba(255, 61, 61, 0.2);
				}

				.layout {
					display: grid;
					grid-template-columns: minmax(0, 2fr) minmax(300px, 1fr);
					gap: 18px;
				}

				.panel {
					border: 1px solid var(--border);
					background: var(--surface);
					border-radius: 16px;
					overflow: hidden;
				}

				.player-wrap {
					padding: 14px;
				}

				.video-frame {
					aspect-ratio: 16 / 9;
					background: #000;
					border-radius: 14px;
					overflow: hidden;
					border: 1px solid #000;
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
					font-size: 1.15rem;
					font-weight: 800;
					margin: 0 0 6px;
					line-height: 1.35;
				}

				.now-playing p {
					color: var(--muted);
					margin: 0;
					font-size: 0.9rem;
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
					border: 1px solid #3b3b41;
					background: #25252a;
					color: #efeff2;
					padding: 7px 12px;
					border-radius: 10px;
					font-size: 0.82rem;
					font-weight: 700;
					cursor: pointer;
					transition: background 0.15s ease, border-color 0.15s ease;
				}

				.btn-control:hover {
					background: #2f2f35;
					border-color: #4a4a52;
				}

				.btn-control.accent {
					background: #3a1f1f;
					border-color: #5b2f2f;
					color: #ffb3b3;
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
					grid-template-columns: 1fr 1fr auto;
					gap: 8px;
				}

				.playlist-select {
					border: 1px solid #3b3b41;
					background: #151519;
					color: #e6e6eb;
					border-radius: 10px;
					padding: 8px 10px;
					font-size: 0.82rem;
					font-weight: 700;
					min-width: 0;
				}

				#media-count {
					font-size: 0.78rem;
					padding: 3px 9px;
					border-radius: 999px;
					background: var(--accent-soft);
					color: #ffb3b3;
					border: 1px solid #5f2b2b;
					font-weight: 700;
				}

				#media-list {
					max-height: calc(100vh - 180px);
					overflow: auto;
					padding: 8px;
				}

				.media-item {
					display: block;
					width: 100%;
					border: 1px solid transparent;
					background: transparent;
					color: var(--text);
					text-align: left;
					border-radius: 12px;
					padding: 12px;
					margin: 0 0 8px;
					cursor: pointer;
					transition: background 0.18s ease, border-color 0.18s ease;
				}

				.media-row {
					display: flex;
					align-items: flex-start;
					gap: 10px;
				}

				.media-thumb {
					width: 68px;
					aspect-ratio: 2 / 3;
					object-fit: cover;
					border-radius: 8px;
					border: 1px solid #3a3a40;
					background: #101013;
					flex: 0 0 auto;
				}

				.media-main {
					min-width: 0;
					flex: 1;
				}

				.media-item:hover {
					background: var(--surface-soft);
					border-color: #393940;
				}

				.media-item.active {
					background: #2a1a1a;
					border-color: #5b2f2f;
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
				}

				@media (max-width: 1024px) {
					.layout {
						grid-template-columns: 1fr;
					}

					#media-list {
						max-height: 48vh;
					}
				}

				@media (max-width: 640px) {
					.app-shell {
						padding: 10px;
					}

					.topbar {
						padding: 10px;
						border-radius: 12px;
					}

					.search-wrap {
						max-width: none;
					}

					.player-wrap {
						padding: 10px;
					}

					.video-frame {
						border-radius: 10px;
					}
				}

				::-webkit-scrollbar { width: 10px; }
				::-webkit-scrollbar-track { background: #16161a; }
				::-webkit-scrollbar-thumb { background: #33333a; border-radius: 999px; }
			</style>
		</head>
		<body>
			<div class="app-shell">
				<header class="topbar">
					<div class="brand">
						<span class="brand-dot"></span>
						<span>PMS</span>
					</div>
					<div class="search-wrap">
						<input id="search-input" class="search-input" type="search" placeholder="Search title, series, category... (Press /)">
						<button id="btn-clear-search" class="btn-control" type="button">Clear</button>
					</div>
					<div class="section-title mb-0">Watch</div>
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
								</div>
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
								<select id="series-select" class="playlist-select">
									<option value="">All</option>
								</select>
								<select id="type-select" class="playlist-select">
									<option value="">All types</option>
								</select>
								<button id="btn-shuffle" class="btn-control" type="button">Shuffle</button>
							</div>
						</div>
						<div id="media-list">
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
				const seriesSelect = document.getElementById('series-select');
				const typeSelect = document.getElementById('type-select');
				const searchInput = document.getElementById('search-input');
				const btnClearSearch = document.getElementById('btn-clear-search');
				const btnPrev = document.getElementById('btn-prev');
				const btnNext = document.getElementById('btn-next');
				const btnAutoplay = document.getElementById('btn-autoplay');
				const btnShuffle = document.getElementById('btn-shuffle');
				let mediaData = [];
				let currentQueue = [];
				let currentIndex = -1;
				let autoplayNext = true;
				let lastPlayedPath = '';

				function esc(input) {
					return String(input || '')
						.replace(/&/g, '&amp;')
						.replace(/</g, '&lt;')
						.replace(/>/g, '&gt;')
						.replace(/"/g, '&quot;')
						.replace(/'/g, '&#39;');
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
					const seriesSet = {};
					mediaData.forEach(function(m) { seriesSet[m.series] = true; });
					const allSeries = Object.keys(seriesSet).sort(function(a, b) {
						return a.localeCompare(b, undefined, { sensitivity: 'base' });
					});

					const current = seriesSelect.value;
					seriesSelect.innerHTML = '<option value="">All playlists</option>' + allSeries.map(function(s) {
						return '<option value="' + esc(s) + '">' + esc(s) + '</option>';
					}).join('');
					seriesSelect.value = allSeries.indexOf(current) >= 0 ? current : '';
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

				function buildQueue() {
					const selectedSeries = seriesSelect.value;
					const selectedType = typeSelect.value;
					const q = String(searchInput.value || '').trim().toLowerCase();
					let queue = mediaData.slice();
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
						listDiv.innerHTML = '<div class="empty-state">No match for current filters.</div>';
						return;
					}

					listDiv.innerHTML = currentQueue.map(function(m, i) {
						const ep = m.episodeNo !== null ? ('EP ' + m.episodeNo) : 'EP -';
						const sub = m.isJav ? (m.series + ' • CODE') : (m.isArtist ? (m.series + ' • CLIP') : (m.series + ' • ' + ep));
						const thumb = m.isJav && m.coverPath
							? ('<img class="media-thumb" src="/stream?path=' + encodeURIComponent(m.coverPath) + '" alt="' + esc(m.episodeTitle) + '" loading="lazy" onerror="this.style.display=&quot;none&quot;">')
							: '';
						return '' +
							'<button class="media-item' + (i === currentIndex ? ' active' : '') + '" data-qidx="' + i + '">' +
								'<div class="media-row">' +
									thumb +
									'<div class="media-main">' +
										'<div class="media-title">' + esc(m.episodeTitle) + '</div>' +
										'<div class="media-sub">' + esc(sub) + '</div>' +
									'</div>' +
								'</div>' +
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

					video.src = '/stream?path=' + encodeURIComponent(m.path);
					video.load();
					video.play().catch(function(){});
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
				seriesSelect.addEventListener('change', function() {
					buildQueue();
					currentIndex = currentQueue.length > 0 ? 0 : -1;
					renderQueue();
					if (currentIndex >= 0) {
						selectItemByQueueIndex(currentIndex);
					}
				});
				typeSelect.addEventListener('change', function() {
					buildQueue();
					currentIndex = currentQueue.length > 0 ? 0 : -1;
					renderQueue();
					if (currentIndex >= 0) {
						selectItemByQueueIndex(currentIndex);
					}
				});
				searchInput.addEventListener('input', function() {
					buildQueue();
					if (lastPlayedPath) {
						syncActiveFromPath(lastPlayedPath);
					}
					if (currentIndex < 0 && currentQueue.length > 0) {
						currentIndex = 0;
					}
					renderQueue();
				});
				searchInput.addEventListener('keydown', function(e) {
					if (e.key === 'Enter' && currentQueue.length > 0) {
						selectItemByQueueIndex(currentIndex >= 0 ? currentIndex : 0);
					}
				});
				btnClearSearch.addEventListener('click', function() {
					searchInput.value = '';
					searchInput.focus();
					buildQueue();
					if (lastPlayedPath) {
						syncActiveFromPath(lastPlayedPath);
					}
					if (currentIndex < 0 && currentQueue.length > 0) {
						currentIndex = 0;
					}
					renderQueue();
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
