package db

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

type DB struct {
	DB *sql.DB
	mu sync.Mutex
}

type User struct {
	id       int
	created  int
	email    string
	hash     []byte
	username string
}

func Open(ctx context.Context, DBPath string) (*DB, error) {
	if err := os.MkdirAll(filepath.Dir(DBPath), 0700); err != nil {
		return nil, err
	}

	sqlite, err := sql.Open("sqlite3", fmt.Sprintf("file:%s?cache=shared&mode=rwc&mutex=full", DBPath))
	if err != nil {
		return nil, err
	}

	// try to enable write-ahead-logging
	_, _ = sqlite.Exec(`PRAGMA journal_mode = WAL`)

	defer func() {
		if err != nil {
			_ = sqlite.Close()
		}
	}()

	tx, err := sqlite.Begin()
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	_, err = tx.Exec("CREATE TABLE IF NOT EXISTS `users` (`id` INTEGER PRIMARY KEY, `created` INTEGER, `email` TEXT UNIQUE, `hash` BLOB, `username` TEXT UNIQUE);")
	if err != nil {
		return nil, err
	}

	_, err = tx.Exec("CREATE TABLE IF NOT EXISTS `songs` (`title` TEXT, `description` TEXT, `created` INTEGER, `user_id` INTEGER);")
	if err != nil {
		return nil, err
	}

	_, err = tx.Exec("CREATE TABLE IF NOT EXISTS `comments` (`id` INTEGER PRIMARY KEY, `text` TEXT, `created` INTEGER, `user_id` INTEGER, `comment_id` INTEGER, `song_id` INTEGER);")
	if err != nil {
		return nil, err
	}

	_, err = tx.Exec("CREATE INDEX IF NOT EXISTS idx_songs_created ON songs (created);")
	if err != nil {
		return nil, err
	}

	err = tx.Commit()
	if err != nil {
		return nil, err
	}

	db := &DB{
		DB: sqlite,
	}

	return db, nil
}

// AddSong to the database
func (db *DB) AddSong(title, description string, userID int) error {
	defer db.locked()()

	created := time.Now().Unix()
	_, err := db.DB.Exec("INSERT INTO songs (title, description, created, user_id) VALUES (?, ?, ?, ?)", title, description, created, userID)
	return err
}

// AddComment to the database
func (db *DB) AddComment(text string, userID, commentID, songID int) error {
	defer db.locked()()

	created := time.Now().Unix()
	_, err := db.DB.Exec("INSERT INTO comments (text, created, user_id, comment_id, song_id) VALUES (?, ?, ?, ?, ?)", text, created, userID, commentID, songID)
	return err
}

// AddUser to the database
func (db *DB) AddUser(email, username string, hash []byte) error {
	defer db.locked()()

	created := time.Now().Unix()
	_, err := db.DB.Exec("INSERT INTO users (created, email, hash, username) VALUES (?, ?, ?, ?)", created, email, hash, username)
	return err
}

// GetUser checks if user and password combo exist in the database
func (db *DB) GetUser(user string, hash []byte) (result User, err error) {
	defer db.locked()()

	row := db.DB.QueryRow("SELECT * FROM users WHERE hash=? && username=?;", hash, user)
	err = row.Scan(&result.id, &result.created, &result.email, &result.hash, &result.username)
	fmt.Println(result)
	return result, err
}

// Close the database
func (db *DB) Close() error {
	return db.DB.Close()
}

func (db *DB) locked() func() {
	db.mu.Lock()
	return db.mu.Unlock
}
