package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/1999AZZAR/portable-pms/src/internal/db"
	"github.com/1999AZZAR/portable-pms/src/internal/scanner"
	"github.com/1999AZZAR/portable-pms/src/internal/streamer"
)

var (
	version = "1.1.0"
)

func main() {
	mediaPath := flag.String("path", ".", "Path to media directory")
	port := flag.Int("port", 8080, "Server port")
	logLevel := flag.String("log-level", "info", "Log level (debug, info, warn, error)")
	flag.Parse()

	level := parseLogLevel(*logLevel)
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: level}))
	slog.SetDefault(logger)

	absPath, err := filepath.Abs(*mediaPath)
	if err != nil {
		logger.Error("invalid path", "path", *mediaPath, "error", err)
		os.Exit(1)
	}

	if stat, err := os.Stat(absPath); err != nil || !stat.IsDir() {
		logger.Error("media path not accessible", "path", absPath, "error", err)
		os.Exit(1)
	}

	metaDir := filepath.Join(absPath, ".metadata")
	if err := os.MkdirAll(metaDir, 0755); err != nil {
		logger.Error("failed to create metadata directory", "error", err)
		os.Exit(1)
	}

	database, err := db.InitDB(filepath.Join(metaDir, "pms.db"))
	if err != nil {
		logger.Error("database init failed", "error", err)
		os.Exit(1)
	}
	defer database.Close()

	st := streamer.New(absPath, logger)
	var scanInProgress atomic.Int32

	logger.Info("starting portable media streamer",
		"version", version,
		"media_path", absPath,
		"port", *port,
		"log_level", *logLevel)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		defer func() {
			if r := recover(); r != nil {
				logger.Error("scanner goroutine panic", "error", r)
			}
		}()

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
			logger.Info("found stale absolute-path entries, refreshing index", "count", staleAbsRows)
			if _, err := database.Exec("DELETE FROM media WHERE path LIKE '/%'"); err != nil {
				logger.Error("failed to clean stale entries", "error", err)
			}
			_ = os.Remove(sentinelPath)
			needScan = true
		}

		if needScan {
			scanInProgress.Store(1)
			logger.Info("starting media scan")
			
			scanCtx, scanCancel := context.WithTimeout(ctx, 30*time.Minute)
			defer scanCancel()
			
			s := scanner.New(absPath, database, logger)
			if err := s.Start(scanCtx); err != nil {
				if err == context.Canceled {
					logger.Info("scan cancelled")
				} else {
					logger.Error("scan error", "error", err)
				}
			} else {
				_ = os.WriteFile(sentinelPath, []byte("done"), 0644)
				logger.Info("scan complete, sentinel created")
			}
			scanInProgress.Store(0)
		} else {
			scanInProgress.Store(0)
			logger.Info("skipping scan, metadata cached")
		}
	}()

	mux := http.NewServeMux()

	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()

		health := map[string]interface{}{
			"status":  "healthy",
			"version": version,
		}

		if err := database.PingContext(ctx); err != nil {
			health["status"] = "degraded"
			health["database"] = "unhealthy"
			w.WriteHeader(http.StatusServiceUnavailable)
		}

		health["scanning"] = scanInProgress.Load() == 1

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(health)
	})

	mux.HandleFunc("/api/media", func(w http.ResponseWriter, r *http.Request) {
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

	mux.HandleFunc("/api/status", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]bool{
			"scanning": scanInProgress.Load() == 1,
		})
	})

	mux.HandleFunc("/stream", st.ServeMedia)
	mux.HandleFunc("/hls/", st.ServeHLS)

	executable, _ := os.Executable()
	baseDir := filepath.Dir(executable)
	if _, err := os.Stat(filepath.Join(baseDir, "web", "static")); os.IsNotExist(err) {
		baseDir, _ = os.Getwd()
	}

	staticDir := filepath.Join(baseDir, "web", "static")
	mux.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.Dir(staticDir))))

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		html := `
		<!DOCTYPE html>
		<html lang="en">
		<head>
		<meta charset="UTF-8">
		<meta name="viewport" content="width=device-width, initial-scale=1.0, maximum-scale=1.0, user-scalable=no, viewport-fit=cover">
		<meta name="apple-mobile-web-app-capable" content="yes">
		<meta name="apple-mobile-web-app-status-bar-style" content="black-translucent">
		<meta name="theme-color" content="#090909">
		<title>PMS - Portable Media Streamer</title>
		<link rel="stylesheet" href="https://cdnjs.cloudflare.com/ajax/libs/font-awesome/6.5.1/css/all.min.css">
		<link href="/static/css/bootstrap.min.css" rel="stylesheet">
		<script src="/static/js/hls.min.js"></script>
			<style>
				@import url('https://fonts.googleapis.com/css2?family=Manrope:wght@500;700;800&display=swap');

		:root {
			--bg: #090909;
			--surface: #121214;
			--surface-soft: #1d1d22;
			--surface-elevated: #1f2024;
			--text: #f5f5f7;
			--text-secondary: #b8bbc2;
			--muted: #9ea2aa;
			--accent: #ff2f2f;
			--accent-hover: #ff4545;
			--accent-soft: #321616;
			--accent-container: #5b1f1f;
			--border: #2a2b31;
			--shell-pad: 12px;
			--playlist-card-min: 220px;
			--playlist-card-max: 300px;
			--rail-card-min: 260px;
			--rail-card-max: 330px;
			--elevation-1: 0 2px 4px rgba(0, 0, 0, 0.2);
			--elevation-2: 0 4px 8px rgba(0, 0, 0, 0.3);
			--elevation-3: 0 8px 16px rgba(0, 0, 0, 0.4);
			--elevation-4: 0 16px 32px rgba(0, 0, 0, 0.5);
			--bottom-nav-height: 0px;
			--transition-quick: 150ms cubic-bezier(0.2, 0, 0, 1);
			--transition-standard: 250ms cubic-bezier(0.2, 0, 0, 1);
			--transition-emphasized: 400ms cubic-bezier(0.2, 0, 0, 1);
			--controls-timeout: 3000;
		}

		* {
			box-sizing: border-box;
			-webkit-tap-highlight-color: transparent;
		}

		@keyframes ripple {
			0% {
				transform: scale(0);
				opacity: 0.5;
			}
			100% {
				transform: scale(4);
				opacity: 0;
			}
		}

		@keyframes slideUp {
			from {
				transform: translateY(20px);
				opacity: 0;
			}
			to {
				transform: translateY(0);
				opacity: 1;
			}
		}

		@keyframes skeleton-loading {
			0% {
				background-position: -200px 0;
			}
			100% {
				background-position: calc(200px + 100%) 0;
			}
		}

		@keyframes float {
			0%, 100% { transform: translateY(0px); }
			50% { transform: translateY(-10px); }
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
				padding-bottom: var(--bottom-nav-height);
			}

			.app-shell {
				width: 100%;
				max-width: 100vw;
				margin: 0 auto;
				padding: var(--shell-pad);
				height: calc(100vh - var(--bottom-nav-height));
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
				padding: 12px 16px;
				border-radius: 12px;
				font-size: clamp(0.8rem, 1vw, 0.88rem);
				font-weight: 700;
				cursor: pointer;
				transition: background 0.15s ease, border-color 0.15s ease, transform 0.15s ease;
				white-space: nowrap;
				min-height: 48px;
				min-width: 48px;
				display: inline-flex;
				align-items: center;
				justify-content: center;
				touch-action: manipulation;
				user-select: none;
			}

			.btn-control:active {
				transform: scale(0.96);
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
			transition: background var(--transition-standard), border-color var(--transition-standard), transform var(--transition-quick);
			position: relative;
			overflow: hidden;
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
			display: flex;
			flex-direction: column;
			align-items: center;
			justify-content: center;
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

		.mobile-bottom-nav {
			position: fixed;
			bottom: 0;
			left: 0;
			right: 0;
			background: linear-gradient(180deg, rgba(18, 18, 22, 0.98) 0%, rgba(9, 9, 9, 0.98) 100%);
			border-top: 1px solid var(--border);
			backdrop-filter: blur(20px);
			display: none;
			padding: 8px 8px calc(8px + env(safe-area-inset-bottom));
			z-index: 1000;
			box-shadow: var(--elevation-3);
		}

		.mobile-nav-grid {
			display: grid;
			grid-template-columns: repeat(4, 1fr);
			gap: 8px;
			max-width: 480px;
			margin: 0 auto;
		}

		.mobile-nav-btn {
			position: relative;
			overflow: hidden;
			display: flex;
			flex-direction: column;
			align-items: center;
			justify-content: center;
			gap: 4px;
			padding: 12px 8px;
			border: none;
			background: transparent;
			color: var(--muted);
			font-size: 0.7rem;
			font-weight: 600;
			cursor: pointer;
			border-radius: 16px;
			transition: all var(--transition-standard);
			min-height: 64px;
			touch-action: manipulation;
		}

		.mobile-nav-btn::before {
			content: '';
			position: absolute;
			top: 50%;
			left: 50%;
			width: 0;
			height: 0;
			border-radius: 50%;
			background: rgba(255, 47, 47, 0.2);
			transform: translate(-50%, -50%);
			transition: width var(--transition-quick), height var(--transition-quick);
		}

		.mobile-nav-btn:active::before {
			width: 100%;
			height: 100%;
		}

		.mobile-nav-btn.active {
			background: rgba(255, 47, 47, 0.12);
			color: var(--accent);
		}

		.mobile-nav-btn:active {
			transform: scale(0.96);
		}

		.mobile-nav-icon {
			font-size: 1.4rem;
			line-height: 1;
		}

		.fab {
			position: fixed;
			bottom: calc(var(--bottom-nav-height) + 16px);
			right: 16px;
			width: 56px;
			height: 56px;
			border-radius: 16px;
			background: linear-gradient(135deg, var(--accent) 0%, var(--accent-hover) 100%);
			color: white;
			border: none;
			box-shadow: var(--elevation-3);
			display: none;
			align-items: center;
			justify-content: center;
			font-size: 1.5rem;
			cursor: pointer;
			z-index: 999;
			transition: all var(--transition-standard);
			animation: float 3s ease-in-out infinite;
		}

		.fab:active {
			transform: scale(0.92);
		}

		.skeleton {
			background: linear-gradient(
				90deg,
				var(--surface-soft) 0px,
				var(--surface-elevated) 40px,
				var(--surface-soft) 80px
			);
			background-size: 200px 100%;
			animation: skeleton-loading 1.5s infinite;
			border-radius: 12px;
		}

		.skeleton-card {
			height: 200px;
			margin-bottom: 12px;
		}

		.ripple {
			position: absolute;
			border-radius: 50%;
			background: rgba(255, 255, 255, 0.3);
			transform: scale(0);
			animation: ripple 0.6s ease-out;
			pointer-events: none;
		}

		.swipe-feedback {
			position: absolute;
			top: 50%;
			transform: translateY(-50%);
			font-size: 4rem;
			color: var(--accent);
			opacity: 0;
			transition: opacity var(--transition-quick);
			pointer-events: none;
			z-index: 10;
			text-shadow: 0 4px 12px rgba(255, 47, 47, 0.5);
		}

		.swipe-feedback.left {
			left: 20%;
		}

		.swipe-feedback.right {
			right: 20%;
		}

		.swipe-feedback.show {
			opacity: 1;
		}

		.pip-button {
			position: absolute;
			top: 12px;
			right: 12px;
			width: 40px;
			height: 40px;
			border-radius: 12px;
			background: rgba(0, 0, 0, 0.6);
			backdrop-filter: blur(10px);
			border: none;
			color: white;
			display: flex;
			align-items: center;
			justify-content: center;
			cursor: pointer;
			opacity: 0;
			transition: opacity var(--transition-standard);
			z-index: 10;
		}

		.video-frame:hover .pip-button {
			opacity: 1;
		}

		.empty-state-icon {
			font-size: 4rem;
			color: var(--muted);
			margin-bottom: 16px;
			opacity: 0.3;
		}

		.view-transition {
			animation: slideUp var(--transition-emphasized) ease-out;
		}

			.mobile-filter-panel {
				position: fixed;
				bottom: 0;
				left: 0;
				right: 0;
				background: linear-gradient(180deg, rgba(18, 18, 22, 0.98) 0%, rgba(9, 9, 9, 0.98) 100%);
				border-top: 1px solid var(--border);
				backdrop-filter: blur(20px);
				transform: translateY(100%);
				transition: transform 0.3s cubic-bezier(0.2, 0, 0, 1);
				z-index: 999;
				max-height: 70vh;
				overflow-y: auto;
				padding: 16px 16px calc(16px + env(safe-area-inset-bottom));
			}

			.mobile-filter-panel.show {
				transform: translateY(0);
			}

			.mobile-filter-header {
				display: flex;
				justify-content: space-between;
				align-items: center;
				margin-bottom: 16px;
			}

			.mobile-filter-header h3 {
				margin: 0;
				font-size: 1.1rem;
				font-weight: 800;
			}

			.mobile-filter-close {
				width: 40px;
				height: 40px;
				border-radius: 999px;
				border: none;
				background: rgba(255, 255, 255, 0.05);
				color: var(--text);
				display: flex;
				align-items: center;
				justify-content: center;
				font-size: 1.5rem;
				cursor: pointer;
			}

			.mobile-filter-group {
				margin-bottom: 16px;
			}

			.mobile-filter-label {
				display: block;
				font-size: 0.85rem;
				font-weight: 700;
				color: var(--muted);
				margin-bottom: 8px;
				text-transform: uppercase;
				letter-spacing: 0.5px;
			}

			.mobile-filter-select {
				width: 100%;
				padding: 14px 16px;
				border: 1px solid #3e414a;
				background: #111218;
				color: #e6e6eb;
				border-radius: 12px;
				font-size: 1rem;
				font-weight: 600;
				min-height: 52px;
			}

			.swipe-indicator {
				position: absolute;
				top: 50%;
				transform: translateY(-50%);
				font-size: 3rem;
				color: rgba(255, 47, 47, 0.3);
				pointer-events: none;
				opacity: 0;
				transition: opacity 0.2s ease;
			}

			.swipe-indicator.left {
				left: 20px;
			}

			.swipe-indicator.right {
				right: 20px;
			}

			.swipe-indicator.show {
				opacity: 1;
			}

		@media (max-width: 640px) {
			:root {
				--bottom-nav-height: 80px;
			}

			.mobile-bottom-nav {
				display: block;
			}

			.fab {
				display: flex;
			}

			body {
				overflow: auto;
				height: auto;
				min-height: 100vh;
			}

			.app-shell {
				padding: 8px 0 0 0;
				height: auto;
				min-height: calc(100vh - var(--bottom-nav-height));
				padding-bottom: var(--bottom-nav-height);
			}

			.topbar {
				padding: 10px;
				border-radius: 12px;
				margin: 0 8px 12px 8px;
			}

			.search-wrap {
				max-width: none;
				min-width: 0;
			}

			.layout {
				grid-template-columns: 1fr;
				gap: 12px;
				height: auto;
				min-height: auto;
			}

			main.panel {
				margin: 0 8px;
				border-radius: 16px;
			}

			aside.panel {
				margin: 0 8px;
				border-radius: 16px;
				max-height: 70vh;
			}

			.player-wrap {
				position: fixed;
				top: 0;
				left: 0;
				right: 0;
				bottom: var(--bottom-nav-height);
				padding: 0;
				background: #000;
				z-index: 100;
				touch-action: manipulation;
			}

			.video-frame {
				position: absolute;
				top: 0;
				left: 0;
				right: 0;
				bottom: 0;
				border-radius: 0;
				display: flex;
				align-items: center;
				justify-content: center;
				margin: 0;
			}
			
			.video-frame video {
				width: 100%;
				height: 100%;
				object-fit: contain;
			}
			
			.player-overlay {
				position: absolute;
				top: 0;
				left: 0;
				right: 0;
				bottom: 0;
				background: linear-gradient(180deg, rgba(0,0,0,0.6) 0%, transparent 20%, transparent 80%, rgba(0,0,0,0.6) 100%);
				opacity: 1;
				transition: opacity 0.3s ease;
				pointer-events: none;
				z-index: 1;
			}
			
			.player-overlay.hidden {
				opacity: 0;
			}
			
			.player-top-bar {
				position: absolute;
				top: 0;
				left: 0;
				right: 0;
				padding: 20px 16px;
				display: flex;
				align-items: center;
				gap: 12px;
				z-index: 2;
				pointer-events: auto;
			}
			
			.player-back-btn {
				width: 40px;
				height: 40px;
				border-radius: 50%;
				background: rgba(0, 0, 0, 0.5);
				backdrop-filter: blur(8px);
				border: none;
				color: #fff;
				font-size: 1.25rem;
				display: flex;
				align-items: center;
				justify-content: center;
				cursor: pointer;
				transition: transform 0.2s ease;
			}
			
			.player-back-btn:active {
				transform: scale(0.9);
			}
			
			.player-title-wrap {
				flex: 1;
				min-width: 0;
			}
			
			.player-title {
				color: #fff;
				font-size: 1rem;
				font-weight: 700;
				margin: 0;
				overflow: hidden;
				text-overflow: ellipsis;
				white-space: nowrap;
				text-shadow: 0 2px 4px rgba(0, 0, 0, 0.5);
			}
			
			.player-subtitle {
				color: rgba(255, 255, 255, 0.8);
				font-size: 0.75rem;
				margin: 2px 0 0;
				overflow: hidden;
				text-overflow: ellipsis;
				white-space: nowrap;
				text-shadow: 0 2px 4px rgba(0, 0, 0, 0.5);
			}
			
			.player-bottom-bar {
				position: absolute;
				bottom: 0;
				left: 0;
				right: 0;
				padding: 16px;
				z-index: 2;
				pointer-events: auto;
			}
			
			.player-controls {
				display: flex;
				align-items: center;
				justify-content: center;
				gap: 32px;
				margin-bottom: 16px;
			}
			
			.player-control-btn {
				width: 48px;
				height: 48px;
				border-radius: 50%;
				background: rgba(0, 0, 0, 0.5);
				backdrop-filter: blur(8px);
				border: none;
				color: #fff;
				font-size: 1.5rem;
				display: flex;
				align-items: center;
				justify-content: center;
				cursor: pointer;
				transition: all 0.2s ease;
			}
			
			.player-control-btn:active {
				transform: scale(0.9);
			}
			
			.player-control-btn.play {
				width: 64px;
				height: 64px;
				font-size: 2rem;
				background: var(--accent);
			}
			
			.seek-indicator {
				position: absolute;
				top: 50%;
				transform: translateY(-50%);
				width: 120px;
				height: 120px;
				border-radius: 50%;
				background: rgba(0, 0, 0, 0.7);
				backdrop-filter: blur(8px);
				display: flex;
				flex-direction: column;
				align-items: center;
				justify-content: center;
				gap: 8px;
				opacity: 0;
				pointer-events: none;
				transition: opacity 0.2s ease;
				z-index: 3;
			}
			
			.seek-indicator.left {
				left: 20%;
			}
			
			.seek-indicator.right {
				right: 20%;
			}
			
			.seek-indicator.show {
				opacity: 1;
			}
			
			.seek-indicator i {
				font-size: 2.5rem;
				color: #fff;
			}
			
			.seek-indicator span {
				font-size: 1rem;
				color: #fff;
				font-weight: 700;
			}

			.control-row {
				display: grid;
				grid-template-columns: repeat(2, 1fr);
				gap: 10px;
				margin-top: 16px;
			}

			.control-row .btn-control {
				width: 100%;
				padding: 14px 12px;
				font-size: 0.9rem;
			}

			.section-title {
				font-size: 0.8rem;
				margin-top: 20px;
				margin-bottom: 12px;
			}

			.recommend-list,
			.recent-list {
				margin-left: -12px;
				margin-right: -12px;
				padding-left: 12px;
				padding-right: 12px;
			}

			.recommend-card,
			.recent-card {
				min-width: 200px;
				max-width: 240px;
			}

			.card-thumb {
				border-radius: 10px;
			}

			.sidebar-head {
				padding: 12px;
				position: sticky;
				top: 0;
				z-index: 10;
			}

			.sidebar-head h2 {
				font-size: 1rem;
			}

			#media-count {
				font-size: 0.75rem;
				padding: 4px 10px;
			}

			.playlist-tools {
				display: none;
			}

			#media-list {
				padding: 8px;
				max-height: none;
				overflow-y: auto;
			}

			.media-list.grid {
				grid-template-columns: 1fr;
				gap: 8px;
			}

			.media-item {
				padding: 12px;
				border-radius: 12px;
				margin-bottom: 8px;
			}

			.media-title {
				font-size: 1rem;
				margin-bottom: 6px;
			}

			.media-sub {
				font-size: 0.85rem;
			}

			.empty-state {
				margin: 16px;
				padding: 32px 20px;
			}

			aside.panel {
				display: none;
			}

			aside.panel.mobile-visible {
				display: flex;
			}

			main.panel.mobile-hidden {
				display: none;
			}
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
					<div class="player-wrap mobile-visible">
					<div class="video-frame">
						<video id="video-player" playsinline preload="metadata"></video>
						
						<div class="player-overlay" id="player-overlay">
							<div class="player-top-bar">
								<button class="player-back-btn" id="player-back-btn">
									<i class="fa-solid fa-chevron-down"></i>
								</button>
								<div class="player-title-wrap">
									<h2 class="player-title" id="player-overlay-title">Select a video</h2>
									<p class="player-subtitle" id="player-overlay-subtitle"></p>
								</div>
							</div>
							
							<div class="player-bottom-bar">
								<div class="player-controls">
									<button class="player-control-btn" id="player-prev-btn">
										<i class="fa-solid fa-backward-step"></i>
									</button>
									<button class="player-control-btn play" id="player-play-btn">
										<i class="fa-solid fa-play"></i>
									</button>
									<button class="player-control-btn" id="player-next-btn">
										<i class="fa-solid fa-forward-step"></i>
									</button>
								</div>
							</div>
						</div>
						
						<div class="seek-indicator left" id="seek-left">
							<i class="fa-solid fa-backward-fast"></i>
							<span>-10s</span>
						</div>
						<div class="seek-indicator right" id="seek-right">
							<i class="fa-solid fa-forward-fast"></i>
							<span>+10s</span>
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

		<button class="fab" id="fab-play-random" type="button" title="Play Random">
			<i class="fa-solid fa-shuffle"></i>
		</button>

		<nav class="mobile-bottom-nav">
			<div class="mobile-nav-grid">
				<button class="mobile-nav-btn active" id="mobile-nav-player" type="button">
					<i class="mobile-nav-icon fa-solid fa-play"></i>
					<span>Player</span>
				</button>
				<button class="mobile-nav-btn" id="mobile-nav-playlist" type="button">
					<i class="mobile-nav-icon fa-solid fa-list"></i>
					<span>Playlist</span>
				</button>
				<button class="mobile-nav-btn" id="mobile-nav-filter" type="button">
					<i class="mobile-nav-icon fa-solid fa-filter"></i>
					<span>Filter</span>
				</button>
				<button class="mobile-nav-btn" id="mobile-nav-more" type="button">
					<i class="mobile-nav-icon fa-solid fa-ellipsis-vertical"></i>
					<span>More</span>
				</button>
			</div>
		</nav>

			<div class="mobile-filter-panel" id="mobile-filter-panel">
				<div class="mobile-filter-header">
					<h3>Filters</h3>
					<button class="mobile-filter-close" id="mobile-filter-close" type="button">×</button>
				</div>
				<div class="mobile-filter-group">
					<label class="mobile-filter-label">Root</label>
					<select id="root-select-mobile" class="mobile-filter-select">
						<option value="">All roots</option>
					</select>
				</div>
				<div class="mobile-filter-group">
					<label class="mobile-filter-label">Playlist</label>
					<select id="series-select-mobile" class="mobile-filter-select">
						<option value="">All playlists</option>
					</select>
				</div>
				<div class="mobile-filter-group">
					<label class="mobile-filter-label">Type</label>
					<select id="type-select-mobile" class="mobile-filter-select">
						<option value="">All types</option>
					</select>
				</div>
			</div>

			<script>
				const video = document.getElementById('video-player');
				const listDiv = document.getElementById('media-list');
				const playerOverlay = document.getElementById('player-overlay');
				const playerOverlayTitle = document.getElementById('player-overlay-title');
				const playerOverlaySubtitle = document.getElementById('player-overlay-subtitle');
				const playerBackBtn = document.getElementById('player-back-btn');
				const playerPlayBtn = document.getElementById('player-play-btn');
				const playerPrevBtn = document.getElementById('player-prev-btn');
				const playerNextBtn = document.getElementById('player-next-btn');
				const seekLeft = document.getElementById('seek-left');
				const seekRight = document.getElementById('seek-right');
				const mediaCount = document.getElementById('media-count');
				const rootSelect = document.getElementById('root-select');
				const seriesSelect = document.getElementById('series-select');
				const typeSelect = document.getElementById('type-select');
				const searchInput = document.getElementById('search-input');
				const btnClearSearch = document.getElementById('btn-clear-search');
			let mediaData = [];
			let currentQueue = [];
			let currentIndex = -1;
			let autoplayNext = true;
			let lastPlayedPath = '';
			let hlsInstance = null;
			let controlsTimeout;
			let lastTap = 0;
			let hideControlsDelay = parseInt(getComputedStyle(document.documentElement).getPropertyValue('--controls-timeout'));
			
			function showControls() {
				playerOverlay.classList.remove('hidden');
				clearTimeout(controlsTimeout);
				controlsTimeout = setTimeout(() => {
					if (!video.paused) {
						playerOverlay.classList.add('hidden');
					}
				}, hideControlsDelay);
			}
			
			function hideControls() {
				playerOverlay.classList.add('hidden');
				clearTimeout(controlsTimeout);
			}
			
			function togglePlayPause() {
				if (video.paused) {
					video.play();
					playerPlayBtn.innerHTML = '<i class="fa-solid fa-pause"></i>';
				} else {
					video.pause();
					playerPlayBtn.innerHTML = '<i class="fa-solid fa-play"></i>';
				}
			}
			
			function seekVideo(seconds) {
				video.currentTime = Math.max(0, Math.min(video.duration, video.currentTime + seconds));
			}
			
			function showSeekIndicator(direction) {
				const indicator = direction === 'left' ? seekLeft : seekRight;
				indicator.classList.add('show');
				setTimeout(() => {
					indicator.classList.remove('show');
				}, 500);
			}
			let recentPaths = [];
			let recommendationAnchorPath = '';
			let recommendationPaths = [];

			let touchStartX = 0;
			let touchStartY = 0;
			let touchEndX = 0;
			let touchEndY = 0;
			const swipeThreshold = 80;

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
				listDiv.innerHTML = '<div class="empty-state"><i class="empty-state-icon fa-solid fa-folder-open"></i><div><strong>No media found</strong></div><div>Adjust your filters or add media files</div></div>';
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
					playerOverlayTitle.textContent = m.episodeTitle || m.title || 'Untitled';
					const subtitle = (m.series || '-') + ' / ' + (m.type || '-');
					playerOverlaySubtitle.textContent = subtitle;
					lastPlayedPath = m.path;
					markRecentPlayed(m.path);
					refreshRecommendationsForCurrent();
					playWithFallback(m.path);
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
				} catch (_) {}
				
				if(!Array.isArray(data) || data.length === 0) {
					mediaData = [];
					currentQueue = [];
					currentIndex = -1;
					mediaCount.textContent = '0 items';
					
					if (scanning) {
						listDiv.innerHTML = '<div class="empty-state"><div class="skeleton skeleton-card"></div><div class="skeleton skeleton-card"></div><div><i class="fa-solid fa-spinner fa-spin" style="font-size: 2rem; color: var(--accent); margin-top: 12px;"></i></div><div style="margin-top: 12px;">Scanning media library...</div></div>';
					} else {
						listDiv.innerHTML = '<div class="empty-state"><i class="empty-state-icon fa-solid fa-compact-disc"></i><div><strong>No media found</strong></div><div>Add video files to get started</div></div>';
					}
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

				
				playerPlayBtn.addEventListener('click', togglePlayPause);
				playerBackBtn.addEventListener('click', () => {
					document.querySelector('.mobile-nav-btn[data-view="playlist"]').click();
				});
				playerPrevBtn.addEventListener('click', () => playPrev());
				playerNextBtn.addEventListener('click', () => playNext());
				
				video.addEventListener('play', () => {
					playerPlayBtn.innerHTML = '<i class="fa-solid fa-pause"></i>';
					showControls();
				});
				
				video.addEventListener('pause', () => {
					playerPlayBtn.innerHTML = '<i class="fa-solid fa-play"></i>';
					showControls();
				});
				
				video.addEventListener('click', (e) => {
					e.stopPropagation();
				});
				
				playerOverlay.parentElement.addEventListener('click', (e) => {
					const now = Date.now();
					const timeSinceLastTap = now - lastTap;
					const rect = video.getBoundingClientRect();
					const clickX = e.clientX - rect.left;
					const clickWidth = rect.width;
					
					if (timeSinceLastTap < 300 && timeSinceLastTap > 0) {
						e.preventDefault();
						if (clickX < clickWidth / 3) {
							seekVideo(-10);
							showSeekIndicator('left');
							triggerHaptic('medium');
						} else if (clickX > (clickWidth * 2) / 3) {
							seekVideo(10);
							showSeekIndicator('right');
							triggerHaptic('medium');
						} else {
							togglePlayPause();
							triggerHaptic('light');
						}
						lastTap = 0;
					} else {
						if (playerOverlay.classList.contains('hidden')) {
							showControls();
						} else {
							hideControls();
						}
						lastTap = now;
					}
				});
				
				video.addEventListener('touchmove', showControls);
				
				let touchStartY = 0;
				let touchStartX = 0;
				
				document.querySelector('.video-frame').addEventListener('touchstart', (e) => {
					touchStartY = e.touches[0].clientY;
					touchStartX = e.touches[0].clientX;
				}, { passive: true });
				
				document.querySelector('.video-frame').addEventListener('touchend', (e) => {
					const touchEndY = e.changedTouches[0].clientY;
					const touchEndX = e.changedTouches[0].clientX;
					const deltaY = touchStartY - touchEndY;
					const deltaX = touchStartX - touchEndX;
					
					if (Math.abs(deltaY) > 100 && Math.abs(deltaY) > Math.abs(deltaX)) {
						triggerHaptic('medium');
						if (deltaY > 0) {
							playNext();
						} else {
							playPrev();
						}
					}
				}, { passive: true });
				
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
			setInterval(fetchMedia, 5000);

		// Mobile swipe gestures
		const playerWrap = document.querySelector('.player-wrap');
		const videoFrame = document.querySelector('.video-frame');
		const swipeLeft = document.getElementById('swipe-left');
		const swipeRight = document.getElementById('swipe-right');

		function triggerHaptic(style) {
			if ('vibrate' in navigator) {
				switch(style) {
					case 'light':
						navigator.vibrate(10);
						break;
					case 'medium':
						navigator.vibrate(20);
						break;
					case 'heavy':
						navigator.vibrate([30, 10, 30]);
						break;
				}
			}
		}

		function createRipple(event) {
			const button = event.currentTarget;
			const ripple = document.createElement('span');
			const rect = button.getBoundingClientRect();
			const size = Math.max(rect.width, rect.height);
			const x = event.clientX - rect.left - size / 2;
			const y = event.clientY - rect.top - size / 2;

			ripple.className = 'ripple';
			ripple.style.width = ripple.style.height = size + 'px';
			ripple.style.left = x + 'px';
			ripple.style.top = y + 'px';

			button.appendChild(ripple);
			setTimeout(() => ripple.remove(), 600);
		}

		document.querySelectorAll('.btn-control, .mobile-nav-btn, .media-item').forEach(btn => {
			btn.addEventListener('click', createRipple);
		});

		// Mobile bottom navigation
		const mobileNavPlayer = document.getElementById('mobile-nav-player');
		const mobileNavPlaylist = document.getElementById('mobile-nav-playlist');
		const mobileNavFilter = document.getElementById('mobile-nav-filter');
		const mobileNavMore = document.getElementById('mobile-nav-more');
		const mobileFilterPanel = document.getElementById('mobile-filter-panel');
		const mobileFilterClose = document.getElementById('mobile-filter-close');
		const mainPanel = document.querySelector('main.panel');
		const asidePanel = document.querySelector('aside.panel');
		const fabPlayRandom = document.getElementById('fab-play-random');

		// FAB
		if (fabPlayRandom) {
			fabPlayRandom.addEventListener('click', function() {
				triggerHaptic('medium');
				playRandom();
			});
		}

		function setActiveNav(activeBtn) {
			document.querySelectorAll('.mobile-nav-btn').forEach(btn => btn.classList.remove('active'));
			activeBtn.classList.add('active');
			triggerHaptic('light');
		}

		if (mobileNavPlayer) {
			mobileNavPlayer.addEventListener('click', function() {
				setActiveNav(mobileNavPlayer);
				if (mainPanel) {
					mainPanel.classList.remove('mobile-hidden');
					mainPanel.style.display = 'block';
					mainPanel.classList.add('view-transition');
				}
				if (asidePanel) {
					asidePanel.classList.remove('mobile-visible');
					asidePanel.style.display = 'none';
				}
				mobileFilterPanel.classList.remove('show');
			});
		}

		if (mobileNavPlaylist) {
			mobileNavPlaylist.addEventListener('click', function() {
				setActiveNav(mobileNavPlaylist);
				if (mainPanel) {
					mainPanel.classList.add('mobile-hidden');
					mainPanel.style.display = 'none';
				}
				if (asidePanel) {
					asidePanel.classList.add('mobile-visible');
					asidePanel.style.display = 'flex';
					asidePanel.classList.add('view-transition');
				}
				mobileFilterPanel.classList.remove('show');
			});
		}

		if (mobileNavFilter) {
			mobileNavFilter.addEventListener('click', function() {
				setActiveNav(mobileNavFilter);
				mobileFilterPanel.classList.toggle('show');
			});
		}

		if (mobileNavMore) {
			mobileNavMore.addEventListener('click', function() {
				setActiveNav(mobileNavMore);
				// Could open a settings panel in future
				shuffleQueue();
				setTimeout(() => setActiveNav(mobileNavPlayer), 1000);
			});
		}

			if (mobileFilterClose) {
				mobileFilterClose.addEventListener('click', function() {
					mobileFilterPanel.classList.remove('show');
				});
			}

			// Sync mobile filters with desktop
			const rootSelectMobile = document.getElementById('root-select-mobile');
			const seriesSelectMobile = document.getElementById('series-select-mobile');
			const typeSelectMobile = document.getElementById('type-select-mobile');

			if (rootSelectMobile) {
				rootSelectMobile.addEventListener('change', function() {
					rootSelect.value = rootSelectMobile.value;
					rootSelect.dispatchEvent(new Event('change'));
					mobileFilterPanel.classList.remove('show');
				});
			}

			if (seriesSelectMobile) {
				seriesSelectMobile.addEventListener('change', function() {
					seriesSelect.value = seriesSelectMobile.value;
					seriesSelect.dispatchEvent(new Event('change'));
					mobileFilterPanel.classList.remove('show');
				});
			}

			if (typeSelectMobile) {
				typeSelectMobile.addEventListener('change', function() {
					typeSelect.value = typeSelectMobile.value;
					typeSelect.dispatchEvent(new Event('change'));
					mobileFilterPanel.classList.remove('show');
				});
			}

			// Update mobile selects when desktop selects change
			const originalRebuildRoot = rebuildRootOptions;
			rebuildRootOptions = function() {
				originalRebuildRoot();
				if (rootSelectMobile) {
					rootSelectMobile.innerHTML = rootSelect.innerHTML;
					rootSelectMobile.value = rootSelect.value;
				}
			};

			const originalRebuildSeries = rebuildSeriesOptions;
			rebuildSeriesOptions = function() {
				originalRebuildSeries();
				if (seriesSelectMobile) {
					seriesSelectMobile.innerHTML = seriesSelect.innerHTML;
					seriesSelectMobile.value = seriesSelect.value;
				}
			};

			const originalRebuildType = rebuildTypeOptions;
			rebuildTypeOptions = function() {
				originalRebuildType();
				if (typeSelectMobile) {
					typeSelectMobile.innerHTML = typeSelect.innerHTML;
					typeSelectMobile.value = typeSelect.value;
				}
			};

			// Pull to refresh
			let pullStartY = 0;
			let pulling = false;

			if (asidePanel) {
				asidePanel.addEventListener('touchstart', function(e) {
					if (asidePanel.scrollTop === 0) {
						pullStartY = e.touches[0].clientY;
						pulling = true;
					}
				}, { passive: true });

				asidePanel.addEventListener('touchmove', function(e) {
					if (pulling && e.touches[0].clientY - pullStartY > 100) {
						pulling = false;
						fetchMedia();
					}
				}, { passive: true });

				asidePanel.addEventListener('touchend', function() {
					pulling = false;
				}, { passive: true });
			}
		</script>
		</body>
		</html>`
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, html)
	})

	timeoutHandler := http.TimeoutHandler(mux, 30*time.Second, "Request timeout")

	server := &http.Server{
		Addr:         fmt.Sprintf(":%d", *port),
		Handler:      timeoutHandler,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	serverErrors := make(chan error, 1)
	go func() {
		logger.Info("http server listening", "address", server.Addr)
		serverErrors <- server.ListenAndServe()
	}()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	select {
	case err := <-serverErrors:
		logger.Error("server error", "error", err)
		os.Exit(1)
	case sig := <-sigChan:
		logger.Info("shutdown signal received", "signal", sig.String())

		cancel()

		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer shutdownCancel()

		if err := server.Shutdown(shutdownCtx); err != nil {
			logger.Error("graceful shutdown failed", "error", err)
			server.Close()
		}

		if err := st.Shutdown(shutdownCtx); err != nil {
			logger.Warn("streamer shutdown error", "error", err)
		}

		logger.Info("shutdown complete")
	}
}

func parseLogLevel(level string) slog.Level {
	switch strings.ToLower(level) {
	case "debug":
		return slog.LevelDebug
	case "info":
		return slog.LevelInfo
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
