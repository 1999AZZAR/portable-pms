package scanner

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/1999AZZAR/portable-pms/src/internal/db"
)

type Scanner struct {
	RootPath string
	DB       *sql.DB
}

func New(root string, database *sql.DB) *Scanner {
	return &Scanner{RootPath: root, DB: database}
}

func (s *Scanner) Start() error {
	var wg sync.WaitGroup
	extensions := map[string]bool{".mp4": true, ".mkv": true, ".avi": true, ".mov": true}

	err := filepath.Walk(s.RootPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if !info.IsDir() && extensions[strings.ToLower(filepath.Ext(path))] {
			wg.Add(1)
			go func(p string, i os.FileInfo) {
				defer wg.Done()
				s.processFile(p, i)
			}(path, info)
		}
		return nil
	})

	wg.Wait()
	return err
}

func (s *Scanner) processFile(path string, info os.FileInfo) {
	relPath, _ := filepath.Rel(s.RootPath, path)
	parts := strings.Split(relPath, string(os.PathSeparator))

	mediaType := "video"
	category := "General"
	title := info.Name()

	// Tri-Pattern Discovery Logic
	if len(parts) > 1 {
		category = parts[0]
		switch strings.ToLower(category) {
		case "movie", "movies":
			mediaType = "movie"
			if len(parts) >= 2 {
				title = parts[len(parts)-2] // Folder name is usually the title
			}
		case "artis", "artist", "music":
			mediaType = "artist"
			if len(parts) >= 2 {
				title = parts[len(parts)-2] // Artist name or album
			}
		}
	}

	_, err := s.DB.Exec("INSERT OR REPLACE INTO media (path, type, category, title, size) VALUES (?, ?, ?, ?, ?)",
		path, mediaType, category, title, info.Size())
	if err != nil {
		fmt.Printf("❌ Error indexing %s: %v\n", path, err)
	} else {
		fmt.Printf("✅ Indexed: [%s] %s\n", mediaType, title)
	}
}
