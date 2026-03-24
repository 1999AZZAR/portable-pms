# Portable Media Streamer - Quick Reference

## Portable Drive Usage

### Directory Structure
```
your-drive/
├── bin/pms          # Binary
├── web/             # UI assets
├── start.sh         # Linux/Mac launcher
├── start.bat        # Windows launcher
├── .metadata/       # Auto-created (DB + cache)
└── [your media files/folders]
```

### Quick Start
```bash
# Linux/Mac
./start.sh

# Windows
start.bat

# Manual
./bin/pms --path . --port 8080
```

Access: http://localhost:8080

### Common Operations

**Check Health:**
```bash
curl http://localhost:8080/health
```

**Change Port:**
```bash
export PMS_PORT=9000
./start.sh
```

**Enable Debug Logs:**
```bash
export PMS_LOG_LEVEL=debug
./start.sh
```

**Stop Server:**
Press `Ctrl+C` and wait for "shutdown complete"

### Safe Unmounting

1. Stop PMS (Ctrl+C)
2. Wait for "shutdown complete"
3. Unmount/eject drive safely

### Performance by Drive Type

| Drive Type | Scan Speed | Streams | Transcoding |
|------------|------------|---------|-------------|
| USB 2.0    | 10-30/s    | 1-2     | ✗           |
| USB 3.0+   | 50-100/s   | 3-5     | OK          |
| SSD/HDD    | 100-500/s  | 10+     | ✓           |
| NVMe       | 500+/s     | 20+     | ✓✓          |

### Troubleshooting

**Linux Permission Denied:**
```bash
chmod +x start.sh bin/pms
# Or remount with exec
sudo mount -o remount,exec /media/your-drive
```

**Windows Antivirus Block:**
Add exception for `pms.exe`

**macOS Gatekeeper:**
```bash
xattr -d com.apple.quarantine bin/pms
```

**Slow Initial Scan:**
Normal. Subsequent runs use cached database.

**Database Locked:**
Stop PMS, ensure no other instances running:
```bash
pkill -f pms
```

### Files to Never Delete
- `bin/pms` - Binary
- `web/` - UI assets
- `.metadata/pms.db` - Database

### Files Safe to Delete
- `.metadata/cache/*` - Transcoding cache (regenerates)
- `.metadata/pms.db-wal` - Temp file (recreates)
- `.metadata/pms.db-shm` - Temp file (recreates)

### Backup Recommendations
```bash
# Backup database
cp .metadata/pms.db .metadata/pms.db.backup

# Restore if needed
cp .metadata/pms.db.backup .metadata/pms.db
```

## See Full Docs
- [PORTABLE_SETUP.md](PORTABLE_SETUP.md) - Complete portable guide
- [README.md](README.md) - Full documentation
- [DEPLOYMENT.md](DEPLOYMENT.md) - Production deployment
