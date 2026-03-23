package scanner

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

type Scanner struct {
	RootPath string
	DB       *sql.DB
	mu       sync.Mutex // protect DB writes
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

		// Skip hidden folders like .metadata
		if info.IsDir() && strings.HasPrefix(info.Name(), ".") {
			return filepath.SkipDir
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

	// Enhanced Pattern Discovery based on drive structure
	if len(parts) > 1 {
		category = parts[0]
		if len(parts) >= 3 {
			category = fmt.Sprintf("%s/%s", parts[0], parts[1])
			title = parts[len(parts)-2]
			mediaType = "collection"
		} else if len(parts) == 2 {
			title = parts[0]
			mediaType = "video"
		}
	}

	title = strings.TrimSuffix(title, filepath.Ext(title))

	// Serialize DB write to avoid lock contention
	s.mu.Lock()
	_, err := s.DB.Exec("INSERT OR REPLACE INTO media (path, type, category, title, size) VALUES (?, ?, ?, ?, ?)",
		path, mediaType, category, title, info.Size())
	s.mu.Unlock()

	if err != nil {
		fmt.Printf("❌ Error indexing %s: %v\n", path, err)
	} else {
		fmt.Printf("✅ Indexed: [%s] %s\n", category, title)
	}
}
