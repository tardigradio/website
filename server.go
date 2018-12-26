package main

import (
	"bytes"
	"context"
	"crypto/sha512"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os/user"
	"path/filepath"
	"strings"
	"time"

	humanize "github.com/dustin/go-humanize"
	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	"github.com/tardigradio/website/db"

	"storj.io/storj/pkg/storage/streams"
	"storj.io/storj/pkg/storj"
	"storj.io/storj/pkg/stream"
	"storj.io/storj/pkg/utils"
	"storj.io/storj/storage"
)

// Server holds important info for accessing storj API and Tardigradio database
type Server struct {
	DB       *db.DB
	r        *gin.Engine
	metainfo storj.Metainfo
	ss       streams.Store
	rs       storj.RedundancyScheme
	es       storj.EncryptionScheme
}

// Initialize the Tardigradio Server
func Initialize(ctx context.Context) *Server {
	router := gin.Default()

	// Initialize the cookie store
	store := cookie.NewStore([]byte("secret"))
	router.Use(sessions.Sessions("mysession", store))

	// Get current user account for determining Server Home Directory
	usr, err := user.Current()
	if err != nil {
		panic(err)
	}

	// Get Storj Config
	cfg := initConfig(usr.HomeDir)

	meta, ss, err := cfg.Metainfo(ctx)
	if err != nil {
		panic(err)
	}

	// TODO: Derive ID from bs config
	satelliteid := "satelliteid"

	// Open Database for storing tardigradio user data and upload meta
	dbpath := filepath.Join(usr.HomeDir, fmt.Sprintf("/.tardigradio/%s/db.sqlite", satelliteid))
	database, err := db.Open(ctx, dbpath)
	if err != nil {
		panic(err)
	}

	return &Server{DB: database, r: router, metainfo: meta, ss: ss, rs: cfg.GetRedundancyScheme(), es: cfg.GetEncryptionScheme()}
}

// Run the Server using the gin Engine
func (s *Server) Run(address string) {
	s.r.Run(address)
}

// Close will cleanly shutdown the Server
func (s *Server) Close() error {
	return s.DB.Close()
}

// GetRoot will Get Request the "/" endpoint
func (s *Server) GetRoot(c *gin.Context) {
	session := sessions.Default(c)

	// Determine the current user
	var username string
	user, err := s.getCurrentUserFromDbBy(session)
	if err == nil {
		username = user.Username
	}

	// Get all songs uploaded in the last 24 hours
	recent, err := s.DB.GetRecentSongs()
	if err != nil {
		c.String(http.StatusInternalServerError, err.Error())
		return
	}

	type SongWithArtist struct {
		Song    db.Song
		Artist  string
		Created string
	}

	var songs []*SongWithArtist

	// Create array of Recent Songs+Artist
	for _, song := range recent {
		user, err := s.DB.GetUserByID(song.UserID)
		if err != nil {
			c.String(http.StatusInternalServerError, err.Error())
			return
		}

		songs = append(songs, &SongWithArtist{Song: song, Artist: user.Username, Created: humanize.Time(time.Unix(int64(song.Created), 0))})
	}

	c.HTML(http.StatusOK, "index.tmpl", gin.H{
		"recent":      songs,
		"currentUser": username,
	})
	return
}

// GetSong will Get the "/user/:name/*song" endpoint
func (s *Server) GetSong(c *gin.Context) {
	session := sessions.Default(c)

	var currentUserName string
	currentUser, err := s.getCurrentUserFromDbBy(session)
	if err == nil {
		currentUserName = currentUser.Username
	}

	username := c.Param("name")
	title := strings.TrimPrefix(c.Param("song"), "/")

	user, err := s.DB.GetUserByName(username)
	if err != nil {
		c.String(http.StatusInternalServerError, "Invalid username or password")
		return
	}

	song, err := s.DB.GetSongByNameForUser(title, user.ID)
	if err != nil {
		c.String(http.StatusInternalServerError, err.Error())
		return
	}

	c.HTML(http.StatusOK, "song.tmpl", gin.H{
		"currentUser": currentUserName,
		"username":    username,
		"song":        song,
	})
}

// DownloadSong will Post the "/user/:name/*song" endpoint
// This endpoint downloads the song
func (s *Server) DownloadSong(c *gin.Context) {
	username := c.Param("name")
	title := strings.TrimPrefix(c.Param("song"), "/")

	// Look up song artist's ID by username
	user, err := s.DB.GetUserByName(username)
	if err != nil {
		c.String(http.StatusInternalServerError, err.Error())
		return
	}

	song, err := s.DB.GetSongByNameForUser(title, user.ID)
	if err != nil {
		c.String(http.StatusInternalServerError, err.Error())
		return
	}

	// Storj: Get access to user bucket
	o, err := s.bs.GetObjectStore(c, username)
	if err != nil {
		c.String(http.StatusInternalServerError, err.Error())
		return
	}

	// Storj: Get ranger for downloading song from bucket
	rr, _, err := o.Get(c, paths.New(song.Filename))
	if err != nil {
		c.String(http.StatusInternalServerError, err.Error())
		return
	}

	// Storj: Range Song
	r, err := rr.Range(c, 0, rr.Size())
	if err != nil {
		c.String(http.StatusInternalServerError, err.Error())
		return
	}
	defer utils.LogClose(r)

	extraHeaders := map[string]string{
		"Content-Disposition": fmt.Sprintf(`attachment; filename="%s"`, song.Filename),
	}

	c.DataFromReader(http.StatusOK, rr.Size(), "audio/*", r, extraHeaders)
	return
}

func (s *Server) GetUpload(c *gin.Context) {
	session := sessions.Default(c)

	user, err := s.getCurrentUserFromDbBy(session)
	if err != nil {
		c.String(http.StatusInternalServerError, err.Error())
		return
	}

	c.HTML(http.StatusOK, "upload.tmpl", gin.H{
		"currentUser": user.Username,
	})
	return
}

// PostUpload uploads a song to the storj network and saves metainfo to Tardigrade database
func (s *Server) PostUpload(c *gin.Context) {
	session := sessions.Default(c)
	title := c.PostForm("songTitle")
	description := c.PostForm("songDesc")

	user, err := s.getCurrentUserFromDbBy(session)
	if err != nil {
		c.String(http.StatusInternalServerError, err.Error())
		return
	}

	fileHeader, _ := c.FormFile("file")

	file, err := fileHeader.Open()
	if err != nil {
		log.Fatal(err)
	}
	defer file.Close()

	createInfo := storj.CreateObject{
		RedundancyScheme: s.rs,
		EncryptionScheme: s.es,
	}

	obj, err := s.metainfo.CreateObject(c, user.Username, fileHeader.Filename, &createInfo)
	if err != nil {
		c.String(http.StatusInternalServerError, err.Error())
	}

	reader := io.Reader(file)

	mutableStream, err := obj.CreateStream(c)
	if err != nil {
		c.String(http.StatusInternalServerError, err.Error())
		return
	}

	upload := stream.NewUpload(c, mutableStream, s.ss)

	_, err = io.Copy(upload, reader)
	if err != nil {
		c.String(http.StatusInternalServerError, err.Error())
		return
	}

	err = s.DB.AddSong(title, description, fileHeader.Filename, user.ID)
	if err != nil {
		c.String(http.StatusInternalServerError, err.Error())
		return
	}

	c.HTML(http.StatusOK, "song.tmpl", gin.H{
		"currentUser": user.Username,
		"username":    user.Username,
		"song":        title,
	})
	return
}

func (s *Server) GetUser(c *gin.Context) {
	session := sessions.Default(c)
	username := c.Param("name")

	var user db.User
	user, err := s.DB.GetUserByName(username)
	if err != nil {
		c.String(http.StatusInternalServerError, err.Error())
		return
	}

	type SongWithReadableCreated struct {
		Song    db.Song
		Created string
	}

	uploads, err := s.DB.GetSongsForUser(user.ID)
	if err != nil {
		c.String(http.StatusInternalServerError, err.Error())
		return
	}

	var songs []*SongWithReadableCreated

	for _, song := range uploads {
		songs = append(songs, &SongWithReadableCreated{Song: song, Created: humanize.Time(time.Unix(int64(song.Created), 0))})
	}

	var currentUserName string
	currentUser, err := s.getCurrentUserFromDbBy(session)
	if err == nil {
		currentUserName = currentUser.Username
	}

	c.HTML(http.StatusOK, "user.tmpl", gin.H{
		"currentUser": currentUserName,
		"username":    username,
		"email":       user.Email,
		"uploads":     songs,
	})
	return
}

func (s *Server) DeleteUser(c *gin.Context) {
	session := sessions.Default(c)

	user, err := s.getCurrentUserFromDbBy(session)
	if err != nil {
		c.String(http.StatusInternalServerError, err.Error())
		return
	}

	password := c.PostForm("password")
	hash := getHashFrom([]byte(password))

	if !s.Validated(user.ID, hash) {
		c.String(http.StatusInternalServerError, "Invalid username or password")
		return
	}

	err = s.DB.DeleteUser(user.ID)
	if err != nil {
		c.String(http.StatusInternalServerError, err.Error())
		return
	}

	// TODO: Delete all songs in bucket
	// TODO: Delete Bucket

	session.Delete("user")
	session.Save()

	c.HTML(http.StatusOK, "index.tmpl", gin.H{
		"Success": "Successfully logged out",
	})
	return
}

// Validate a user
func (s *Server) Validated(userID int, hash []byte) bool {
	userhash, err := s.DB.GetUserHash(userID)
	if err != nil {
		return false
	}

	if !bytes.Equal(hash, userhash) {
		return false
	}

	return true
}

// getHashFrom will sha512 hash a byte slice
func getHashFrom(salt []byte) []byte {
	h := sha512.New()
	h.Write(salt)
	return h.Sum(nil)
}

// getCurrentUserFrom will Get Current User ID from cookies session
func getCurrentUserFrom(session sessions.Session) (int, error) {
	sessionUser := session.Get("user")
	if sessionUser == nil {
		return 0, errors.New("User is not logged in")
	}

	switch sessionUser := interface{}(sessionUser).(type) {
	case int:
		return int(sessionUser), nil
	case int64:
		return int(int64(sessionUser)), nil
	default:
		return 0, errors.New("User is not logged in")
	}
}

// getCurrentUserFromDbBy will get a User object from the database by the current session user
func (s *Server) getCurrentUserFromDbBy(session sessions.Session) (db.User, error) {
	var user db.User

	// Get User ID from Session
	userID, err := getCurrentUserFrom(session)
	if err != nil {
		return user, err
	}

	// Get User meta from database by user id
	user, err = s.DB.GetUserByID(userID)
	if err != nil {
		return user, err
	}

	return user, nil
}

// PostLogin is a Post Request to the /guest/login enpoint
func (s *Server) PostLogin(c *gin.Context) {
	session := sessions.Default(c)

	username := c.PostForm("username")
	password := c.PostForm("password")

	hash := getHashFrom([]byte(password))

	// Look up User in database
	user, err := s.DB.GetUserByName(username)
	if err != nil {
		c.HTML(http.StatusUnauthorized, "login.tmpl", gin.H{
			"Error": "Invalid username or password",
		})
		return
	}

	// Verify user password
	if !s.Validated(user.ID, hash) {
		c.HTML(http.StatusUnauthorized, "login.tmpl", gin.H{
			"Error": "Invalid username or password",
		})
		return
	}

	session.Set("user", user.ID)
	session.Save()

	c.HTML(http.StatusOK, "index.tmpl", gin.H{
		"Success":     "Successfully logged in",
		"currentUser": username,
	})
	return
}

// GetLogin is a Get Request to the /guest/login enpoint
func (s *Server) GetLogin(c *gin.Context) {
	c.HTML(http.StatusOK, "login.tmpl", gin.H{})
	return
}

// PostRegister is a Post Request to the /guest/register enpoint
func (s *Server) PostRegister(c *gin.Context) {
	session := sessions.Default(c)
	email := c.PostForm("email")
	username := c.PostForm("username")
	password := c.PostForm("password")

	hash := getHashFrom([]byte(password))

	// Storj: Check if Bucket already exists
	_, err := s.bs.Get(c, username)
	if err == nil {
		c.HTML(http.StatusInternalServerError, "register.tmpl", gin.H{
			"Error": "Failed to register user: Bucket already exists",
		})
		return
	}

	if !storage.ErrKeyNotFound.Has(err) {
		c.HTML(http.StatusInternalServerError, "register.tmpl", gin.H{
			"Error": fmt.Sprintf("Failed to register user: %s", err.Error()),
		})
		return
	}

	// Storj: Create bucket tied to username
	_, err = s.bs.Put(c, username)
	if err != nil {
		c.HTML(http.StatusInternalServerError, "register.tmpl", gin.H{
			"Error": fmt.Sprintf("Failed to register user: %s", err.Error()),
		})
		return
	}

	log.Printf("Bucket %s created\n", username)

	// Add user to database
	id, err := s.DB.AddUser(email, username, hash)
	if err != nil {
		c.HTML(http.StatusInternalServerError, "register.tmpl", gin.H{
			"Error": fmt.Sprintf("Failed to register user: %s", err.Error()),
		})
		return
	}

	session.Set("user", id)
	session.Save()
	c.HTML(http.StatusOK, "index.tmpl", gin.H{
		"Success":     "Successfully registered",
		"currentUser": username,
	})
	return
}

// GetRegister is a Get Request to the /guest/register enpoint
func (s *Server) GetRegister(c *gin.Context) {
	c.HTML(http.StatusOK, "register.tmpl", gin.H{})
	return
}

func (s *Server) GetLogout(c *gin.Context) {
	session := sessions.Default(c)

	session.Delete("user")
	session.Save()

	c.HTML(http.StatusOK, "index.tmpl", gin.H{
		"Success": "Successfully logged out",
	})
	return
}
