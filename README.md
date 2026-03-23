# Portable Media Streamer (PMS)

A zero-dependency media streaming service designed for high portability and low-resource environments. PMS is built as a single static Go binary that runs directly from external storage with zero installation or host system configuration required.

## Technical Architecture

PMS implements a decentralized media serving model, where the logic and metadata reside on the same drive as the source media.

*   **Core Engine**: Go (Golang) static binary for high concurrency and memory efficiency.
*   **Media Processor**: FFmpeg static build integration for JIT (Just-In-Time) transcoding.
*   **Database**: SQLite 3 for persistent metadata storage, residing in the `.metadata/` directory of the mount point.
*   **Streaming Protocol**: Direct HTTP byte-range streaming by default for minimal CPU/RAM usage, with optional HLS fallback.
*   **UI System**: Dark watch-style UI with playlist management, recommendations, and recent history.

## Features

### 1. Media Discovery and Classification
Automated indexing logic categorizes media based on directory structure:
*   **Flat Files**: Single files are indexed as `video`.
*   **Collections**: Nested series-style folders are indexed as `collection`.
*   **JAV Pattern**: `<root>/<top>/JAV/<code>/<video>` is indexed as `jav` (with cover-aware UI support).
*   **Artist Pattern**: `<root>/<top>/PORNSTARTS/<artist>/<video>` and `<root>/<top>/UC/<artist>/<video>` are indexed as `artist`.

### 2. Playback Compatibility
*   Direct HTTP range streaming (`/stream`) is the default lightweight path.
*   Automatic HLS fallback (`/hls`) is used for files that fail browser-native decode support.
*   HLS segments are generated via FFmpeg and stored in `.metadata/cache/`.

### 3. Playlist and Queue Management
*   Root, playlist, type, and search filters.
*   Episode-aware ordering (with heuristic episode parsing).
*   Prev/Next/Shuffle/Random controls.
*   Autoplay next toggle.
*   Recommendations and recently played rails.

### 4. Path Sanitization
Enforced directory traversal protection. All media requests are validated against the absolute root of the mount point to prevent unauthorized filesystem access.

### 5. Zero-Footprint Portability
All application state, including the SQLite database and transcoded segments, are stored on the external drive. No data is written to the host system's internal storage.

## Setup and Usage

### 1. Compilation
Ensure Go 1.21+ is installed. Run the following command to download dependencies and build the binary:
```bash
go mod tidy
go build -o bin/pms src/cmd/pms/main.go
```

### 2. Deployment
Place the `bin/pms` binary and the `web/` directory at the root of your external drive.

### 3. Execution
Run the binary with the `--path` flag. We recommend using a relative path like `.` if the binary is at the root of your drive.
```bash
./bin/pms --path /path/to/media --port 8080
```

### 4. Access
Open `http://localhost:8080` in your browser.

## API Reference
*   `GET /api/media`: Returns a JSON array of indexed media metadata.
*   `GET /api/status`: Returns scanner state (`{"scanning": true|false}`).
*   `GET /stream?path=...`: Serves direct file stream with range request support.
*   `GET /hls/...`: Serves HLS playlists and segments.

## License
MIT
