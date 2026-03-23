package db

import (
	"database/sql"
	_ "github.com/mattn/go-sqlite3"
)

type Media struct {
	ID       int
	Path     string
	Type     string // video, movie, artist
	Category string
	Title    string
	Size     int64
}

func InitDB(dbPath string) (*sql.DB, error) {
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return nil, err
	}

	statement := `
	CREATE TABLE IF NOT EXISTS media (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		path TEXT UNIQUE,
		type TEXT,
		category TEXT,
		title TEXT,
		size INTEGER
	);`
	_, err = db.Exec(statement)
	return db, err
}
