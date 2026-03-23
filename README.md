# Portable Media Streaming Service (PMS)

Portable, zero-dependency media server built in Go. Designed to run directly from an external drive.

## Tech Stack
- **Core**: Go (Golang)
- **Media**: FFmpeg (Static Binary)
- **DB**: SQLite 3
- **Streaming**: HLS & Direct Stream

## Project Structure
- `src/cmd/pms`: Main entry point.
- `src/internal/scanner`: Auto-indexing logic (Tri-Pattern Discovery).
- `src/internal/streamer`: FFmpeg integration & HLS logic.
- `src/internal/db`: SQLite management.
- `bin/`: External dependencies (ffmpeg, etc).
- `web/`: Frontend streaming interface.

## Quick Start
```bash
./pms --path /path/to/media
```
