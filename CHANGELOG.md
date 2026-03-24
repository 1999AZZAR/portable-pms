# Changelog

## v1.1.0 - Stability and Reliability Improvements

### Critical Fixes

- **Panic Recovery**: Added defer/recover wrappers to scanner goroutine and worker pools to prevent crashes
- **Context Cancellation**: Implemented proper context-based cancellation for scanner and long-running operations
- **Graceful Shutdown**: Added signal handling (SIGINT/SIGTERM) with coordinated shutdown sequence
- **FFmpeg Process Tracking**: Process lifecycle management with cleanup to prevent zombie processes and resource leaks

### Database Improvements

- **Connection Pooling**: Configured max connections, idle connections, and connection lifetime limits
- **WAL Mode**: Enabled Write-Ahead Logging for better concurrent access
- **Busy Timeout**: Set 5-second busy timeout to handle SQLite lock contention
- **Indexes**: Added indexes on category, type, and title columns for faster queries
- **Schema Constraints**: Added NOT NULL constraints and default values

### Logging and Observability

- **Structured Logging**: Migrated from fmt.Printf to log/slog with JSON output
- **Log Levels**: Configurable log levels (debug, info, warn, error) via --log-level flag
- **Health Check Endpoint**: Added /health endpoint for monitoring and orchestration
- **Better Error Context**: Enhanced error messages with structured attributes

### Network and Security

- **Request Timeouts**: Added 30-second timeout wrapper for all HTTP handlers
- **Server Timeouts**: Configured read (15s), write (30s), and idle (120s) timeouts
- **Path Traversal Protection**: Enhanced path validation with explicit ".." detection and absolute path comparison
- **Input Validation**: Added parameter validation for all API endpoints
- **Security Headers**: Added X-Content-Type-Options and Cache-Control headers
- **Access Logging**: Security-relevant events logged with remote addresses

### Performance

- **Transcoding Timeout**: Added 10-minute context timeout for FFmpeg operations
- **Startup Wait Logic**: Polls for HLS playlist availability with 15-second timeout
- **Additional Format Support**: Added .webm to supported video extensions
- **Scanner Metrics**: Track and log scan duration and file counts

### Usage

```bash
./bin/pms --path /media --port 8080 --log-level info
```

New flags:
- `--log-level`: Set logging verbosity (debug, info, warn, error)

New endpoints:
- `GET /health`: Health check with database connectivity and scan status

### Breaking Changes

None. All changes are backward compatible.

### Migration Notes

The scanner constructor now requires a logger parameter. If migrating from v1.0.0, pass `slog.Default()` or a custom logger instance.
