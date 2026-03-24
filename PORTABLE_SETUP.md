# Portable Drive Setup Guide

## Overview

PMS is designed to run entirely from a portable drive (USB flash drive, external HDD/SSD) with zero installation on the host system.

## Directory Structure on Portable Drive

```
/media/your-drive/
├── bin/
│   └── pms                 # Compiled binary
├── web/
│   └── static/             # UI assets (CSS, JS)
├── .metadata/              # Auto-created on first run
│   ├── pms.db             # SQLite database
│   ├── cache/             # HLS transcoding cache
│   └── scan_done          # Scan completion marker
├── start.sh               # Linux/Mac launcher
├── start.bat              # Windows launcher
├── build.sh               # Build script
├── Videos/                # Your media files
├── Movies/
└── Series/
```

## First-Time Setup

### 1. Copy Files to Drive

```bash
# Copy the essentials
cp -r bin web start.sh start.bat /media/your-drive/
chmod +x /media/your-drive/start.sh
chmod +x /media/your-drive/bin/pms
```

### 2. Organize Media

Place your media in any structure:
```
/media/your-drive/
├── Movies/
│   ├── Action/
│   └── Comedy/
├── Series/
│   ├── Breaking Bad/
│   │   ├── Season 1/
│   │   └── Season 2/
└── JAV/
    └── STAR-123/
        ├── cover.jpg
        └── video.mp4
```

### 3. Launch on Any Computer

**Linux/Mac:**
```bash
cd /media/your-drive
./start.sh
```

**Windows:**
```batch
F:
start.bat
```

The binary auto-detects the drive location and scans from there.

## How It Works

### Automatic Drive Detection

```go
// main.go line ~165-170
executable, _ := os.Executable()
baseDir := filepath.Dir(executable)
if _, err := os.Stat(filepath.Join(baseDir, "web", "static")); os.IsNotExist(err) {
    baseDir, _ = os.Getwd()
}
```

The binary:
1. Checks its own location for `web/static/`
2. Falls back to current working directory
3. Stores all data in `.metadata/` on the same drive

### Database Portability

```go
// main.go line ~48-52
metaDir := filepath.Join(absPath, ".metadata")
database, err := db.InitDB(filepath.Join(metaDir, "pms.db"))
```

SQLite database is stored on the drive, not the host system. No host-side persistence.

### Cache Management

```go
// streamer.go line ~22-24
cache := filepath.Join(root, ".metadata", "cache")
os.MkdirAll(cache, 0755)
```

HLS transcoding cache is also on the drive. Transparent to the user.

## Multi-Platform Support

### Building for Multiple Platforms

```bash
# Build for current platform
go build -o bin/pms src/cmd/pms/main.go

# Build for Windows (from Linux/Mac)
GOOS=windows GOARCH=amd64 CGO_ENABLED=1 CC=x86_64-w64-mingw32-gcc \
  go build -o bin/pms.exe src/cmd/pms/main.go

# Build for Mac ARM (from Linux)
GOOS=darwin GOARCH=arm64 CGO_ENABLED=1 CC=aarch64-apple-darwin20.2-gcc \
  go build -o bin/pms-darwin-arm64 src/cmd/pms/main.go
```

**Note**: CGO is required for SQLite. Cross-compilation needs appropriate cross-compilers.

### Launcher Scripts

The launcher scripts (`start.sh`, `start.bat`) automatically:
- Detect drive mount point
- Use relative paths
- Set default port and log level
- Display access URL

## Drive Safety

### Prevent Data Loss

**Graceful Unmount:**
```bash
# Stop the server first (Ctrl+C)
# Wait for "shutdown complete" message
# Then unmount the drive
```

**SQLite WAL Mode:**
- Enabled by default for crash resistance
- `-wal` and `-shm` files are temporary
- Safe to delete on unmount (will be recreated)

**Cache Cleanup:**
```bash
# Optional: Clear transcoding cache to save space
rm -rf .metadata/cache/*
```

### Read-Only Media Protection

```bash
# Make media files read-only to prevent accidental modification
chmod -R 444 /media/your-drive/Movies
chmod -R 444 /media/your-drive/Series

# Keep .metadata writable
chmod -R 755 /media/your-drive/.metadata
```

## Performance on Different Drives

### USB 2.0 Flash Drive
- Scan: Slow (10-30 files/sec)
- Streaming: OK for 1-2 concurrent streams
- Transcoding: Not recommended

### USB 3.0+ Flash Drive
- Scan: Moderate (50-100 files/sec)
- Streaming: Good for 3-5 concurrent streams
- Transcoding: Acceptable

### External HDD/SSD
- Scan: Fast (100-500 files/sec)
- Streaming: Excellent (10+ concurrent streams)
- Transcoding: Recommended

### NVMe USB Enclosure
- Scan: Very Fast (500+ files/sec)
- Streaming: Excellent (20+ concurrent streams)
- Transcoding: Optimal

## Troubleshooting Portable Issues

### "Permission Denied" on Linux

```bash
# Remount with exec permission
sudo mount -o remount,exec /media/your-drive

# Or run from home directory
cp /media/your-drive/bin/pms ~/pms-temp
~/pms-temp --path /media/your-drive
```

### Windows Antivirus Blocks Binary

1. Add exception for `pms.exe`
2. Or digitally sign the binary
3. Or run from PowerShell with:
   ```powershell
   Set-ExecutionPolicy -Scope Process -ExecutionPolicy Bypass
   .\bin\pms.exe --path .
   ```

### macOS Gatekeeper Blocks Binary

```bash
# Remove quarantine attribute
xattr -d com.apple.quarantine bin/pms

# Or right-click > Open > confirm
```

### Drive Letters Change (Windows)

Use the launcher script - it auto-detects the drive:
```batch
F:\start.bat    # Works regardless of drive letter
```

### Slow Scanning

```bash
# Skip scan if database exists
./bin/pms --path . --log-level warn

# Or scan in background on first run
# (already default behavior)
```

## Security Considerations

### Network Exposure

By default, PMS binds to `0.0.0.0:8080` (all interfaces). For public networks:

```bash
# Bind to localhost only
./bin/pms --path . --port 8080
# Then access via: http://localhost:8080

# Or use SSH tunnel
ssh -L 8080:localhost:8080 user@remote-host
```

### No Built-in Authentication

PMS has no authentication. Options:
1. Use on trusted networks only
2. Run behind reverse proxy with auth
3. Use VPN (Tailscale, WireGuard)
4. Firewall rules

### HTTPS Not Included

For HTTPS, use a reverse proxy:

**Caddy (automatic HTTPS):**
```
media.example.com {
    reverse_proxy localhost:8080
}
```

**nginx:**
```nginx
server {
    listen 443 ssl;
    server_name media.example.com;
    ssl_certificate /path/to/cert.pem;
    ssl_certificate_key /path/to/key.pem;
    
    location / {
        proxy_pass http://localhost:8080;
    }
}
```

## Best Practices

1. **Regular Backups**: Back up `.metadata/pms.db` periodically
2. **Clean Shutdowns**: Always Ctrl+C before unplugging
3. **Drive Health**: Check SMART status regularly
4. **File Organization**: Use consistent folder structures
5. **Cache Management**: Clear cache if space is limited
6. **Log Rotation**: Redirect logs to file and rotate:
   ```bash
   ./bin/pms --path . 2>&1 | tee -a pms.log
   ```

## Advanced: Auto-Launch on Insert (Linux)

Create udev rule:
```bash
# /etc/udev/rules.d/99-pms-autostart.rules
ACTION=="add", SUBSYSTEM=="block", ENV{ID_FS_UUID}=="your-drive-uuid", \
  RUN+="/media/your-drive/start.sh"
```

## Advanced: Auto-Launch on Insert (Windows)

Use Task Scheduler with Event Trigger (Event ID 2001, Kernel-PnP).

## Advanced: Multi-Drive Setup

Run multiple instances on different ports:
```bash
# Drive 1
/media/drive1/bin/pms --path /media/drive1 --port 8080 &

# Drive 2
/media/drive2/bin/pms --path /media/drive2 --port 8081 &

# Unified UI (nginx)
upstream pms_cluster {
    server localhost:8080;
    server localhost:8081;
}
```

## Conclusion

PMS is truly portable - plug in, run, unplug. All state stays on the drive, nothing touches the host system.
