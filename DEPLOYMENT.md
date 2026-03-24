# Production Deployment Guide

## Overview

This guide covers deploying PMS in production environments with monitoring, logging, and high availability.

## Deployment Options

### 1. Systemd Service (Linux)

1. Create a dedicated user:
```bash
sudo useradd -r -s /bin/false pms
```

2. Copy the binary and web files:
```bash
sudo mkdir -p /opt/pms
sudo cp bin/pms /opt/pms/
sudo cp -r web /opt/pms/
sudo chown -R pms:pms /opt/pms
```

3. Install the service file:
```bash
sudo cp pms.service /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable pms
sudo systemctl start pms
```

4. Check status:
```bash
sudo systemctl status pms
sudo journalctl -u pms -f
```

### 2. Docker Deployment

1. Build the image:
```bash
docker build -t pms:latest .
```

2. Run with Docker Compose:
```bash
docker-compose up -d
```

3. Check logs:
```bash
docker-compose logs -f pms
```

### 3. Kubernetes Deployment

See `k8s/` directory for manifests (deployment, service, ingress, configmap).

## Monitoring and Observability

### Health Checks

PMS exposes a health endpoint at `/health`:

```bash
curl http://localhost:8080/health
```

Response:
```json
{
  "status": "healthy",
  "version": "1.1.0",
  "scanning": false
}
```

Status codes:
- `200`: Service healthy
- `503`: Service degraded (database unavailable)

### Prometheus Metrics (Future)

For now, monitor via:
- Health check endpoint
- Structured JSON logs
- System metrics (CPU, memory, disk I/O)

### Log Aggregation

PMS outputs structured JSON logs to stdout. Integrate with:
- **ELK Stack**: Filebeat → Elasticsearch → Kibana
- **Loki**: Promtail → Loki → Grafana
- **Splunk**: Universal Forwarder → Splunk

Example log entry:
```json
{
  "time": "2026-03-25T10:30:00Z",
  "level": "INFO",
  "msg": "scan completed",
  "duration": "45.2s",
  "files_found": 1523
}
```

## Performance Tuning

### Database Optimization

SQLite is configured with:
- WAL mode for concurrent reads
- 5-second busy timeout
- Connection pooling (25 max open, 5 idle)

For large libraries (>10k files):
```bash
# Increase connection pool
# Modify src/internal/db/sqlite.go:
db.SetMaxOpenConns(50)
db.SetMaxIdleConns(10)
```

### FFmpeg Tuning

Transcode settings in `src/internal/streamer/streamer.go`:
- Preset: `veryfast` (balance speed/quality)
- CRF: `23` (quality level)
- Audio bitrate: `128k`

For lower CPU usage:
```bash
-preset ultrafast -crf 28
```

For better quality:
```bash
-preset fast -crf 20
```

### Network Optimization

- Enable HTTP/2 (requires TLS)
- Use a reverse proxy (nginx, Caddy) for:
  - TLS termination
  - Static file caching
  - Rate limiting
  - Load balancing

Example nginx config:
```nginx
upstream pms_backend {
    server 127.0.0.1:8080;
}

server {
    listen 443 ssl http2;
    server_name media.example.com;

    ssl_certificate /etc/letsencrypt/live/media.example.com/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/media.example.com/privkey.pem;

    location /static/ {
        proxy_pass http://pms_backend;
        proxy_cache static_cache;
        expires 1h;
    }

    location / {
        proxy_pass http://pms_backend;
        proxy_http_version 1.1;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection "upgrade";
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_buffering off;
    }
}
```

## Security Hardening

### File Permissions

```bash
chmod 755 /opt/pms/bin/pms
chmod 755 /opt/pms/web
chmod 644 /opt/pms/web/static/*
chown -R pms:pms /media/external/.metadata
chmod 700 /media/external/.metadata
```

### Firewall Rules

```bash
# Allow only necessary ports
sudo ufw allow 8080/tcp
sudo ufw enable
```

### Rate Limiting

Use a reverse proxy or firewall to prevent abuse:

```nginx
limit_req_zone $binary_remote_addr zone=media_limit:10m rate=10r/s;
limit_req zone=media_limit burst=20 nodelay;
```

### Authentication (Optional)

PMS has no built-in auth. Use:
- Reverse proxy basic auth
- OAuth2 proxy (oauth2-proxy)
- VPN/Tailscale for private access

## Backup Strategy

### Database Backup

```bash
# Create backup script
#!/bin/bash
BACKUP_DIR=/backups/pms
mkdir -p $BACKUP_DIR
sqlite3 /media/external/.metadata/pms.db ".backup $BACKUP_DIR/pms-$(date +%Y%m%d).db"
find $BACKUP_DIR -name "pms-*.db" -mtime +7 -delete
```

### Automation

```bash
# Add to cron
0 2 * * * /opt/pms/scripts/backup.sh
```

## Troubleshooting

### High CPU Usage

Check for stuck FFmpeg processes:
```bash
ps aux | grep ffmpeg
```

Kill orphaned processes:
```bash
pkill -9 ffmpeg
```

### Database Locked

Check for stale locks:
```bash
fuser /media/external/.metadata/pms.db
```

Restart the service:
```bash
sudo systemctl restart pms
```

### Memory Leaks

Monitor with:
```bash
watch -n 5 'ps aux | grep pms'
```

Enable Go profiling (requires rebuild with pprof):
```bash
go tool pprof http://localhost:6060/debug/pprof/heap
```

## Scaling

### Horizontal Scaling

PMS is designed for single-instance deployment. For multiple instances:
1. Use a shared network filesystem (NFS, Ceph)
2. Load balance with sticky sessions
3. Share the `.metadata` directory

### Vertical Scaling

Recommended resources per 1000 media files:
- CPU: 1 core
- RAM: 512MB
- Disk I/O: 100 IOPS

For 10k+ files:
- CPU: 2 cores
- RAM: 2GB
- Disk I/O: 500 IOPS

## Support

For issues, check:
1. Service logs: `journalctl -u pms -f`
2. Health endpoint: `curl http://localhost:8080/health`
3. Database integrity: `sqlite3 pms.db "PRAGMA integrity_check;"`
