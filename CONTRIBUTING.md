# Contributing to Portable Media Streamer

## Code Standards

### Go Style Guide

Follow standard Go conventions:
- `gofmt` for formatting
- `go vet` for static analysis
- Effective Go guidelines

### Project Structure

```
portable-pms/
├── bin/                    # Compiled binaries
├── src/
│   ├── cmd/pms/           # Main application entry point
│   └── internal/          # Internal packages
│       ├── db/            # Database layer
│       ├── scanner/       # Media file scanner
│       └── streamer/      # HTTP streaming handlers
├── web/                   # Frontend assets
│   └── static/            # CSS, JS, images
├── .env.example           # Configuration template
├── CHANGELOG.md           # Version history
├── DEPLOYMENT.md          # Production deployment guide
├── README.md              # Project overview
└── go.mod                 # Go module definition
```

### Testing

Run tests before committing:
```bash
go test ./src/...
```

Add tests for new features:
```go
func TestScannerWithContext(t *testing.T) {
    // Test implementation
}
```

### Logging

Use structured logging with appropriate levels:
```go
logger.Debug("operation details", "key", value)
logger.Info("normal operation", "key", value)
logger.Warn("recoverable issue", "key", value, "error", err)
logger.Error("critical error", "key", value, "error", err)
```

### Error Handling

Always handle errors explicitly:
```go
if err != nil {
    logger.Error("operation failed", "error", err)
    return fmt.Errorf("context: %w", err)
}
```

### Security

Before committing:
1. Check for hardcoded credentials
2. Validate all user inputs
3. Use context timeouts for long operations
4. Test path traversal protection

## Pull Request Process

1. Fork the repository
2. Create a feature branch
3. Make your changes
4. Add tests
5. Update documentation
6. Submit PR with clear description

## Release Process

1. Update version in `src/cmd/pms/main.go`
2. Update `CHANGELOG.md`
3. Tag the release: `git tag v1.1.0`
4. Build binaries for multiple platforms
5. Create GitHub release
