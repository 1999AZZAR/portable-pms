#!/bin/bash
# Portable Media Streamer Launcher
# Auto-detects drive location and starts PMS

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
BINARY="$SCRIPT_DIR/bin/pms"
PORT="${PMS_PORT:-8080}"
LOG_LEVEL="${PMS_LOG_LEVEL:-info}"

if [ ! -f "$BINARY" ]; then
    echo "Error: PMS binary not found at $BINARY"
    exit 1
fi

if [ ! -x "$BINARY" ]; then
    chmod +x "$BINARY"
fi

echo "================================================"
echo "  Portable Media Streamer"
echo "================================================"
echo "  Drive Location: $SCRIPT_DIR"
echo "  Port: $PORT"
echo "  Log Level: $LOG_LEVEL"
echo "================================================"
echo ""
echo "Starting server..."
echo "Access at: http://localhost:$PORT"
echo "Press Ctrl+C to stop"
echo ""

"$BINARY" --path "$SCRIPT_DIR" --port "$PORT" --log-level "$LOG_LEVEL"
