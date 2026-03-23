# Portable Media Streamer (PMS)

A zero-dependency media streaming service designed for high portability and low-resource environments. PMS is built as a single static Go binary that runs directly from external storage with zero installation or host system configuration required.

## Technical Architecture

PMS implements a decentralized media serving model, where the logic and metadata reside on the same drive as the source media.

*   **Core Engine**: Go (Golang) static binary for high concurrency and memory efficiency.
*   **Media Processor**: FFmpeg static build integration for JIT (Just-In-Time) transcoding.
*   **Database**: SQLite 3 for persistent metadata storage, residing in the `.metadata/` directory of the mount point.
*   **Streaming Protocol**: HLS (HTTP Live Streaming) with support for range requests and adaptive seeking.
*   **UI System**: Neo-M3 Hybrid implementation using local CSS/JS assets to ensure full offline functionality.

## Features

### 1. Tri-Pattern Discovery
Automated indexing logic that categorizes media based on directory structure:
*   **Flat Scan**: Single files are indexed as standalone video entities.
*   **Nested Scan**: Directories containing a dominant video file are indexed as unified Movie entities.
*   **Collection Scan**: Nested directories under a primary category (e.g., Artis) are grouped as playlists or artist collections.

### 2. JIT Transcoding
Automatic HLS segment generation using FFmpeg. The system detects client compatibility and triggers background transcoding when required, storing segments in the local `.metadata/cache/` directory.

### 3. Path Sanitization
Enforced directory traversal protection. All media requests are validated against the absolute root of the mount point to prevent unauthorized filesystem access.

### 4. Zero-Footprint Portability
All application state, including the SQLite database and transcoded segments, are stored on the external drive. No data is written to the host system's internal storage.

## Installation and Usage

### Prerequisites
*   Go 1.21+ (for building from source)
*   FFmpeg (static binary placed in `./bin/` or available in system PATH)

### Building
```bash
go build -o bin/pms src/cmd/pms/main.go
```

### Execution
```bash
./bin/pms --path /path/to/media --port 8080
```

### API Endpoints
*   `GET /api/media`: Returns a JSON array of indexed media metadata.
*   `GET /stream?path=...`: Serves direct file stream with range request support.
*   `GET /hls/...`: Serves HLS playlists and segments.

## License
MIT
