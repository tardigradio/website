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

// DB
type DB struct {
	DB *sql.DB
	mu sync.Mutex
}

// User struct matches row on `users` table
type User struct {
	ID       int
	Created  int
	Email    string
	Username string
	Hash     []byte
}

// Song struct matches row on `songs` table
type Song struct {
	ID          int
	Title       string
	Description string
	Created     int
	UserID      int
	Filename    string
}

// Comment struct matches row on `comments` table
type Comment struct {
	ID        int
	Text      string
	Created   int
	UserID    int
	CommentID int
	SongID    int
}

// Open the database by path and setup tables
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

	_, err = tx.Exec("CREATE TABLE IF NOT EXISTS `songs` (`id` INTEGER PRIMARY KEY, `title` TEXT, `description` TEXT, `created` INTEGER, `user_id` INTEGER, `filename` TEXT);")
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

	_, err = tx.Exec("CREATE INDEX IF NOT EXISTS idx_songs_user_id ON songs (user_id);")
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
func (db *DB) AddSong(title, description, filename string, userID int) error {
	defer db.locked()()

	created := time.Now().Unix()
	_, err := db.DB.Exec("INSERT INTO songs (title, description, created, user_id, filename) VALUES (?, ?, ?, ?, ?)", title, description, created, userID, filename)
	return err
}

// GetSong returns a song by id
func (db *DB) GetSong(id int) (result Song, err error) {
	defer db.locked()()

	row := db.DB.QueryRow("SELECT * FROM songs WHERE id=? LIMIT 1;", id)
	err = row.Scan(&result.ID, &result.Title, &result.Description, &result.Created, &result.UserID, &result.Filename)
	return result, err
}

// GetSongByNameForUser returns a song by title + user's id
func (db *DB) GetSongByNameForUser(title string, userID int) (result Song, err error) {
	defer db.locked()()

	row := db.DB.QueryRow("SELECT * FROM songs WHERE title=? AND user_id=?;", title, userID)
	err = row.Scan(&result.ID, &result.Title, &result.Description, &result.Created, &result.UserID, &result.Filename)
	return result, err
}

// DeleteSongByID from the database
// Also deletes associated comments
func (db *DB) DeleteSongByID(userID int, songID int) error {
	defer db.locked()()

	tx, err := db.DB.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	_, err = tx.Exec(`DELETE FROM songs WHERE id=? AND user_id=?`, songID, userID)
	if err == sql.ErrNoRows {
		err = nil
	}

	_, err = tx.Exec(`DELETE FROM comments WHERE song_id=?`, songID)
	if err == sql.ErrNoRows {
		err = nil
	}

	return tx.Commit()
}

// AddComment to the database
func (db *DB) AddComment(text string, userID, commentID, songID int) error {
	defer db.locked()()

	created := time.Now().Unix()
	_, err := db.DB.Exec("INSERT INTO comments (text, created, user_id, comment_id, song_id) VALUES (?, ?, ?, ?, ?)", text, created, userID, commentID, songID)
	return err
}

// AddUser to the database
func (db *DB) AddUser(email, username string, hash []byte) (int64, error) {
	defer db.locked()()

	created := time.Now().Unix()
	res, err := db.DB.Exec("INSERT INTO users (created, email, hash, username) VALUES (?, ?, ?, ?);", created, email, hash, username)
	if err != nil {
		return 0, err
	}

	return res.LastInsertId()
}

// DeleteUser from the database
func (db *DB) DeleteUser(userID int) error {
	defer db.locked()()
	_, err := db.DB.Exec(`DELETE FROM users WHERE id=?`, userID)
	if err == sql.ErrNoRows {
		err = nil
	}
	return err
}

// GetUserByID checks if user exists in the database
func (db *DB) GetUserByID(userID int) (result User, err error) {
	defer db.locked()()

	row := db.DB.QueryRow("SELECT id,created,email,username FROM users WHERE id=? LIMIT 1;", userID)
	err = row.Scan(&result.ID, &result.Created, &result.Email, &result.Username)
	return result, err
}

// GetUserByName checks if user exists in the database
func (db *DB) GetUserByName(user string) (result User, err error) {
	defer db.locked()()

	row := db.DB.QueryRow("SELECT id,created,email,username FROM users WHERE username=? LIMIT 1;", user)
	err = row.Scan(&result.ID, &result.Created, &result.Email, &result.Username)
	return result, err
}

// GetSongsForUser returns all songs for a specific user
func (db *DB) GetSongsForUser(userID int) (songs []Song, err error) {
	defer db.locked()()

	rows, err := db.DB.Query("SELECT * FROM songs WHERE user_id=?;", userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var song Song

		if err := rows.Scan(&song.ID, &song.Title, &song.Description, &song.Created, &song.UserID, &song.Filename); err != nil {
			return nil, err
		}

		songs = append(songs, song)
	}

	return songs, err
}

// GetRecentSongs returns last 100 songs created
func (db *DB) GetRecentSongs() (songs []Song, err error) {
	defer db.locked()()

	rows, err := db.DB.Query("SELECT * FROM songs ORDER BY created DESC LIMIT 100")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var song Song

		if err := rows.Scan(&song.ID, &song.Title, &song.Description, &song.Created, &song.UserID, &song.Filename); err != nil {
			return nil, err
		}

		songs = append(songs, song)
	}

	return songs, err
}

// GetUserHash returns hash for a specific user for validation
func (db *DB) GetUserHash(userID int) (hash []byte, err error) {
	defer db.locked()()

	row := db.DB.QueryRow("SELECT hash FROM users WHERE id=? LIMIT 1;", userID)
	err = row.Scan(&hash)
	return hash, err
}

// Close the database
func (db *DB) Close() error {
	return db.DB.Close()
}

// lock the database mutex
func (db *DB) locked() func() {
	db.mu.Lock()
	return db.mu.Unlock
}
