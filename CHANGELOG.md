# Changelog

## v1.4.0 - Professional Streaming UI (Research-Driven)

### Mobile Video Player Redesign

Based on comprehensive research of YouTube (2026), Netflix (2026), Material Design 3, and mobile UX best practices.

**Fullscreen-First Player**:
- Player takes full viewport on mobile
- Immersive viewing experience
- Proper portrait aspect ratio handling
- Black background for cinematic feel

**Auto-Hiding Controls**:
- Controls fade after 3 seconds of inactivity
- Reappear on tap
- Persist when video is paused
- Configurable timeout via CSS variable

**Double-Tap to Seek**:
- Tap left third → seek -10 seconds
- Tap center third → toggle play/pause
- Tap right third → seek +10 seconds
- Visual seek indicators with icons
- Haptic feedback on interaction

**Vertical Swipe Navigation**:
- Swipe up → play next video
- Swipe down → play previous video
- TikTok-style interaction pattern
- 100px minimum swipe threshold
- Haptic feedback on navigation

**Semi-Transparent Overlays**:
- Gradient overlay (rgba 0,0,0,0.6)
- Backdrop blur effects
- Top bar with back button and title
- Bottom bar with playback controls

**Touch-Optimized Controls**:
- 48x48px minimum touch targets
- 64x64px play/pause button
- 32px spacing between controls
- Ripple effects on tap
- Proper active states

### Performance Improvements

- Passive event listeners for better scroll performance
- Hardware-accelerated animations (60fps)
- GPU-accelerated backdrop blur
- Debounced control hiding
- Reduced CPU usage

### User Experience

- Professional streaming app feel
- Material You design language
- Gesture-based navigation
- Clear visual feedback
- Accessible touch targets

See PROFESSIONAL_STREAMING_UI.md for detailed research and implementation.

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
