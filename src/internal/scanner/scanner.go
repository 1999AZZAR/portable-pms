package scanner

import (
	"database/sql"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
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
	extensions := map[string]bool{".mp4": true, ".mkv": true, ".avi": true, ".mov": true}

	fileCh := make(chan fileTask, 512)
	workerCount := max(2, runtime.NumCPU())
	var wg sync.WaitGroup

	for i := 0; i < workerCount; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for task := range fileCh {
				s.processFile(task.path, task.info)
			}
		}()
	}

	err := filepath.WalkDir(s.RootPath, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		// Skip hidden folders like .metadata
		if d.IsDir() && strings.HasPrefix(d.Name(), ".") {
			return filepath.SkipDir
		}

		if d.IsDir() {
			return nil
		}

		if !extensions[strings.ToLower(filepath.Ext(path))] {
			return nil
		}

		info, infoErr := d.Info()
		if infoErr != nil {
			return nil
		}
		fileCh <- fileTask{path: path, info: info}
		return nil
	})

	close(fileCh)
	wg.Wait()
	return err
}

func (s *Scanner) processFile(path string, info os.FileInfo) {
	relPath, _ := filepath.Rel(s.RootPath, path)
	parts := splitPath(relPath)

	mediaType, category, title := classify(parts, info.Name())

	// Serialize DB write to avoid lock contention
	s.mu.Lock()
	_, err := s.DB.Exec("INSERT OR REPLACE INTO media (path, type, category, title, size) VALUES (?, ?, ?, ?, ?)",
		relPath, mediaType, category, title, info.Size())
	s.mu.Unlock()

	if err != nil {
		fmt.Printf("❌ Error indexing %s: %v\n", path, err)
	} else {
		fmt.Printf("✅ Indexed: [%s] %s\n", category, title)
	}
}

type fileTask struct {
	path string
	info os.FileInfo
}

func splitPath(relPath string) []string {
	clean := filepath.Clean(relPath)
	if clean == "." || clean == string(filepath.Separator) {
		return nil
	}
	return strings.Split(clean, string(filepath.Separator))
}

func classify(parts []string, fileName string) (mediaType, category, title string) {
	baseTitle := strings.TrimSuffix(fileName, filepath.Ext(fileName))
	mediaType = "video"
	category = "General"
	title = baseTitle

	if len(parts) == 0 {
		return
	}
	if len(parts) == 2 {
		category = parts[0]
		return
	}
	if len(parts) == 3 {
		category = filepath.Join(parts[0], parts[1])
		return
	}

	// Dedicated JAV folder structure:
	// <root>/<top>/JAV/<code>/<episode-file>
	// Treat each code as a single item inside a shared JAV playlist.
	if len(parts) >= 4 && strings.EqualFold(parts[1], "JAV") {
		mediaType = "jav"
		category = filepath.Join(parts[0], parts[1])
		title = parts[2]
		return
	}

	// Artist-centric folder structure (non-JAV), e.g.:
	// <root>/<top>/PORNSTARTS/<artist>/<clip-file>
	// Treat each artist as one playlist with multiple clips.
	if len(parts) >= 4 && (strings.EqualFold(parts[1], "PORNSTARTS") || strings.EqualFold(parts[1], "UC")) {
		mediaType = "artist"
		category = filepath.Join(parts[0], parts[1])
		title = parts[2]
		return
	}

	mediaType = "collection"
	category = filepath.Join(parts[0], parts[1])

	seriesName := parts[len(parts)-2]
	parent := parts[len(parts)-3]

	// Handle nested variant folders like v1/v2/special/uncensored
	if isVariantFolder(seriesName) && len(parts) >= 5 {
		seriesName = parent + " - " + strings.ToUpper(parts[len(parts)-2])
	}

	title = seriesName
	return
}

func isVariantFolder(name string) bool {
	n := strings.ToLower(strings.TrimSpace(name))
	if n == "special" || n == "extras" || n == "uncensored" || n == "censored" {
		return true
	}
	if strings.HasPrefix(n, "season ") || strings.HasPrefix(n, "s") {
		if strings.HasPrefix(n, "season ") {
			return true
		}
		if len(n) > 1 {
			if _, err := strconv.Atoi(strings.TrimLeft(n[1:], "0")); err == nil {
				return true
			}
		}
	}
	if strings.HasPrefix(n, "v") && len(n) > 1 {
		if _, err := strconv.Atoi(strings.TrimLeft(n[1:], "0")); err == nil {
			return true
		}
	}
	return false
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
