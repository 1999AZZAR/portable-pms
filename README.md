# Portable Media Streamer (PMS)

A zero-dependency media streaming service designed for **true portability**. PMS runs entirely from external storage (USB flash drives, external HDD/SSD) with zero installation or configuration on the host system. All data, metadata, and cache remain on the portable drive.

## Key Features

### True Portability
*   **Self-Contained**: Binary, UI assets, database, and cache all live on the same drive
*   **Auto-Detection**: Automatically finds its location and media files
*   **Cross-Platform**: Single binary works on any Linux/Mac machine
*   **No Installation**: Plug in, run, unplug - no trace left on host
*   **Drive-Safe**: Graceful shutdown with WAL-mode SQLite for data integrity

### Technical Architecture

PMS implements a decentralized media serving model, where the logic and metadata reside on the same drive as the source media.

*   **Core Engine**: Go (Golang) static binary for high concurrency and memory efficiency.
*   **Media Processor**: FFmpeg static build integration for JIT (Just-In-Time) transcoding.
*   **Database**: SQLite 3 with WAL mode for persistent metadata storage, residing in the `.metadata/` directory of the mount point.
*   **Streaming Protocol**: Direct HTTP byte-range streaming by default for minimal CPU/RAM usage, with optional HLS fallback.
*   **UI System**: Dark watch-style UI with playlist management, recommendations, and recent history.

## Quick Start

### 1. Download/Build
```bash
go build -o bin/pms src/cmd/pms/main.go
```

### 2. Copy to Portable Drive
```bash
# Copy essentials to your USB/external drive
cp -r bin web start.sh /media/your-drive/
chmod +x /media/your-drive/start.sh
```

### 3. Add Media Files
Organize your media on the drive:
```
/media/your-drive/
├── Movies/
├── Series/
└── JAV/
```

### 4. Run Anywhere
```bash
# Linux/Mac
cd /media/your-drive
./start.sh

# Or manually
./bin/pms --path . --port 8080
```

Access at `http://localhost:8080`

All scanning, metadata, and cache happen on the drive itself. Unplug and move to another computer - everything persists.

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
*   HLS segments are generated via FFmpeg and stored in `.metadata/cache/` on the drive.

### 3. Playlist and Queue Management
*   Root, playlist, type, and search filters.
*   Episode-aware ordering (with heuristic episode parsing).
*   Prev/Next/Shuffle/Random controls.
*   Autoplay next toggle.
*   Recommendations and recently played rails.

### 4. Path Sanitization
Enforced directory traversal protection. All media requests are validated against the absolute root of the mount point to prevent unauthorized filesystem access.

### 5. Zero-Footprint Design
**Nothing is written to the host system.** All application state, including:
- SQLite database → `.metadata/pms.db`
- Transcoded segments → `.metadata/cache/`
- Scan sentinel → `.metadata/scan_done`

Everything stays on the portable drive.

## Stability and Reliability (v1.1.0)

### Production-Ready Features
*   **Panic Recovery**: Scanner and worker goroutines protected with defer/recover
*   **Graceful Shutdown**: Handles SIGINT/SIGTERM with coordinated cleanup
*   **Context Cancellation**: Proper timeout and cancellation for long operations
*   **Connection Pooling**: Optimized SQLite connection management with WAL mode
*   **Process Management**: FFmpeg process tracking and cleanup to prevent zombies
*   **Structured Logging**: JSON logs with configurable levels (debug, info, warn, error)
*   **Health Checks**: `/health` endpoint for monitoring and orchestration
*   **Request Timeouts**: 30-second handler timeout with configurable server timeouts
*   **Enhanced Security**: Improved path validation and access logging

## Usage

### Command Line Flags

```bash
./bin/pms [flags]

Flags:
  --path string       Path to media directory (default ".")
  --port int          Server port (default 8080)
  --log-level string  Log level: debug, info, warn, error (default "info")
```

### Launcher Scripts

**Linux/Mac:**
```bash
./start.sh
# Auto-detects drive location, uses sensible defaults
```

**Windows:**
```batch
start.bat
```

### Environment Variables
```bash
export PMS_PORT=8080
export PMS_LOG_LEVEL=info
./start.sh
```

## API Reference
*   `GET /health`: Health check with database and scanner status
*   `GET /api/media`: Returns a JSON array of indexed media metadata.
*   `GET /api/status`: Returns scanner state (`{"scanning": true|false}`).
*   `GET /stream?path=...`: Serves direct file stream with range request support.
*   `GET /hls/...`: Serves HLS playlists and segments.

## Portable Drive Setup

See [PORTABLE_SETUP.md](PORTABLE_SETUP.md) for comprehensive guide on:
- Directory structure
- Drive safety best practices
- Performance on different drive types
- Troubleshooting portable-specific issues
- Multi-platform setup

## Deployment Options

While designed for portable drives, PMS can also be deployed traditionally:

- **Systemd**: `pms.service` included
- **Docker**: `docker-compose.yml` provided
- **Kubernetes**: Ready for orchestration

See [DEPLOYMENT.md](DEPLOYMENT.md) for production deployment guides.

## Monitoring

Check service health:
```bash
curl http://localhost:8080/health
```

Example response:
```json
{
  "status": "healthy",
  "version": "1.1.0",
  "scanning": false
}
```

## Documentation

- [PORTABLE_SETUP.md](PORTABLE_SETUP.md) - Portable drive configuration and best practices
- [DEPLOYMENT.md](DEPLOYMENT.md) - Traditional deployment (systemd, Docker, Kubernetes)
- [CHANGELOG.md](CHANGELOG.md) - Version history and upgrade notes
- [CONTRIBUTING.md](CONTRIBUTING.md) - Development guidelines
- [STABILITY_REPORT.md](STABILITY_REPORT.md) - Technical stability improvements

## Performance

Tested on various portable drives:
- **USB 2.0 Flash**: 10-30 files/sec scan, 1-2 concurrent streams
- **USB 3.0+ Flash**: 50-100 files/sec scan, 3-5 concurrent streams
- **External SSD**: 100-500 files/sec scan, 10+ concurrent streams
- **NVMe Enclosure**: 500+ files/sec scan, 20+ concurrent streams

## Security Notes

PMS has no built-in authentication. For public networks:
- Bind to localhost only
- Use reverse proxy with authentication
- Deploy behind VPN (Tailscale, WireGuard)
- Configure firewall rules

See [PORTABLE_SETUP.md](PORTABLE_SETUP.md#security-considerations) for details.

## License
MIT
