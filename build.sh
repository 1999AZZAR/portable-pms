#!/bin/bash
# Cross-platform build script for Portable Media Streamer

set -e

VERSION="1.1.0"
OUTPUT_DIR="bin"
SOURCE="src/cmd/pms/main.go"

echo "Building Portable Media Streamer v$VERSION"
echo "=========================================="

mkdir -p "$OUTPUT_DIR"

build_target() {
    local OS=$1
    local ARCH=$2
    local EXT=$3
    local OUTPUT="${OUTPUT_DIR}/pms-${OS}-${ARCH}${EXT}"
    
    echo "Building for ${OS}/${ARCH}..."
    
    CGO_ENABLED=1 GOOS=$OS GOARCH=$ARCH go build \
        -ldflags="-s -w -X main.version=${VERSION}" \
        -o "$OUTPUT" \
        "$SOURCE"
    
    if [ $? -eq 0 ]; then
        echo "  ✓ Built: $OUTPUT"
    else
        echo "  ✗ Failed: ${OS}/${ARCH}"
        return 1
    fi
}

echo ""
echo "Building native binary..."
build_target "$(go env GOOS)" "$(go env GOARCH)" ""
cp "${OUTPUT_DIR}/pms-$(go env GOOS)-$(go env GOARCH)" "${OUTPUT_DIR}/pms" 2>/dev/null || true

echo ""
echo "Cross-compiling for other platforms..."
echo "(Note: Cross-compilation requires CGO_ENABLED=1 and cross-compilers)"
echo ""

# Uncomment platforms as needed
# build_target "linux" "amd64" ""
# build_target "linux" "arm64" ""
# build_target "darwin" "amd64" ""
# build_target "darwin" "arm64" ""
# build_target "windows" "amd64" ".exe"

echo ""
echo "Build complete!"
echo "Binary location: ${OUTPUT_DIR}/pms"
echo ""
echo "To run from portable drive:"
echo "  ./start.sh              (Linux/Mac)"
echo "  start.bat               (Windows)"
echo ""
