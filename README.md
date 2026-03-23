# Portable Media Streaming Service (PMS) 🚀

Portable, zero-dependency media server built in Go. Designed to run directly from an external drive with zero installation on the host machine.

## ✨ Features
- **Zero-Dependency**: Static Go binary. Just plug and play.
- **Tri-Pattern Discovery**: Intelligent folder scanning for Videos, Movies, and Artist collections.
- **Fast Indexing**: Concurrent scanning using Go routines.
- **Portable DB**: Uses SQLite 3 stored in a hidden `.metadata` folder on your drive.
- **High Performance**: Optimized for low-resource environments (ThinkPad T14, OCI Free Tier).
- **HLS Streaming**: Fast seeking and adaptive bitrate support (In Progress).

## 🛠 Tech Stack
- **Core**: Golang
- **Database**: SQLite 3
- **Media Engine**: FFmpeg (Static build recommended in `./bin`)
- **Frontend**: Minimal HTML5 Player

## 🚀 Quick Start

1. **Build the binary**:
   ```bash
   go build -o bin/pms src/cmd/pms/main.go
   ```

2. **Run it**:
   ```bash
   ./bin/pms --path /path/to/your/media --port 8080
   ```

3. **Access**:
   Open `http://localhost:8080` in your browser.

## 📂 Tri-Pattern Discovery Logic
PMS automatically categorizes your files based on folder structure:
- `movies/Film_Title/file.mp4` -> Categorized as **Movie**.
- `artis/Artist_Name/song.mp4` -> Categorized as **Artist Collection**.
- `anything_else/file.mp4` -> Categorized as **General Video**.

## 🛡 Security
- **Path Sanitization**: Prevents directory traversal attacks.
- **Read-Only**: PMS only needs write access to the `.metadata` folder for the DB and cache. Your original media stays untouched.

## 📜 License
MIT / Wong Edan Philosophy.
