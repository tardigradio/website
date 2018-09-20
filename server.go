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
	"os"
	"os/user"
	"path/filepath"
	"strings"
	"time"

	"github.com/dustin/go-humanize"
	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	"github.com/tardigradio/website/db"

	"storj.io/storj/pkg/paths"
	"storj.io/storj/pkg/storage/buckets"
	"storj.io/storj/pkg/storage/objects"
	"storj.io/storj/pkg/utils"
	"storj.io/storj/storage"
)

type Server struct {
	DB *db.DB
	r  *gin.Engine
	bs buckets.Store
}

func Initialize(ctx context.Context) *Server {
	router := gin.Default()
	store := cookie.NewStore([]byte("secret"))
	router.Use(sessions.Sessions("mysession", store))

	usr, err := user.Current()
	if err != nil {
		panic(err)
	}

	database, err := db.Open(ctx, filepath.Join(usr.HomeDir, "/.tardigradio/db.sqlite"))
	if err != nil {
		panic(err)
	}

	bs, err := getBucketStore(ctx, usr.HomeDir)
	if err != nil {
		panic(err)
	}

	return &Server{DB: database, r: router, bs: bs}
}

func (s *Server) Run(address string) {
	s.r.Run(address)
}

func (s *Server) Close() error {
	return s.DB.Close()
}

func (s *Server) GetRoot(c *gin.Context) {
	session := sessions.Default(c)

	var username string
	user, err := s.getCurrentUserFromDbBy(session)
	if err == nil {
		username = user.Username
	}

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

func (s *Server) PostLogin(c *gin.Context) {
	session := sessions.Default(c)

	username := c.PostForm("username")
	password := c.PostForm("password")

	hash := getHashFrom([]byte(password))

	user, err := s.DB.GetUserByName(username)
	if err != nil {
		c.String(http.StatusInternalServerError, "Invalid username or password")
		return
	}

	if !s.Validated(user.ID, hash) {
		c.String(http.StatusInternalServerError, "Invalid username or password")
		return
	}

	session.Set("user", user.ID)
	session.Save()

	c.String(http.StatusOK, fmt.Sprintf("'%s' logged in!", username))
	return
}

func (s *Server) GetLogin(c *gin.Context) {
	c.HTML(http.StatusOK, "login.tmpl", gin.H{})
	return
}

func (s *Server) PostRegister(c *gin.Context) {
	session := sessions.Default(c)
	email := c.PostForm("email")
	username := c.PostForm("username")
	password := c.PostForm("password")

	hash := getHashFrom([]byte(password))

	_, err := s.bs.Get(c, username)
	if err == nil {
		c.String(http.StatusInternalServerError, "Bucket already exists")
		return
	}
	if !storage.ErrKeyNotFound.Has(err) {
		c.String(http.StatusInternalServerError, err.Error())
		return
	}
	_, err = s.bs.Put(c, username)
	if err != nil {
		c.String(http.StatusInternalServerError, err.Error())
		return
	}

	log.Printf("Bucket %s created\n", username)

	id, err := s.DB.AddUser(email, username, hash)
	if err != nil {
		c.String(http.StatusInternalServerError, "Failed to register")
		return
	}

	session.Set("user", id)
	session.Save()
	c.String(http.StatusOK, fmt.Sprintf("'%s' registered!", username))
	return
}

func (s *Server) GetRegister(c *gin.Context) {
	c.HTML(http.StatusOK, "register.tmpl", gin.H{})
	return
}

func (s *Server) GetLogout(c *gin.Context) {
	session := sessions.Default(c)

	session.Delete("user")
	session.Save()

	c.String(http.StatusOK, "Successfully Logged out")
	return
}

func (s *Server) GetSong(c *gin.Context) {
	username := c.Param("name")
	song := strings.TrimPrefix(c.Param("song"), "/")

	c.HTML(http.StatusOK, "song.tmpl", gin.H{
		"username": username,
		"song":     song,
	})
}

func (s *Server) PostDownload(c *gin.Context) {
	usr, err := user.Current()
	if err != nil {
		panic(err)
	}

	username := c.Param("name")
	title := strings.TrimPrefix(c.Param("song"), "/")

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

	srcObj := song.Filename
	destFile := filepath.Join(usr.HomeDir, "Downloads", srcObj)

	o, err := s.bs.GetObjectStore(c, username)
	if err != nil {
		c.String(http.StatusInternalServerError, err.Error())
		return
	}

	_, err = os.Stat(destFile)
	if err == nil {
		c.String(http.StatusOK, "Filename already exists in Downloads folder")
		return
	}

	var f *os.File

	f, err = os.Open(destFile)
	if err != nil {
		c.String(http.StatusInternalServerError, err.Error())
		return
	}
	defer utils.LogClose(f)

	rr, _, err := o.Get(c, paths.New(srcObj))
	if err != nil {
		c.String(http.StatusInternalServerError, err.Error())
		return
	}

	r, err := rr.Range(c, 0, rr.Size())
	if err != nil {
		c.String(http.StatusInternalServerError, err.Error())
		return
	}
	defer utils.LogClose(r)

	_, err = io.Copy(f, r)
	if err != nil {
		c.String(http.StatusInternalServerError, err.Error())
		return
	}

	if destFile != "-" {
		fmt.Printf("Downloaded %s to %s\n", srcObj, destFile)
	}

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

	// Upload the file to STORJ
	o, err := s.bs.GetObjectStore(c, user.Username)
	if err != nil {
		c.String(http.StatusInternalServerError, err.Error())
		return
	}
	meta := objects.SerializableMeta{}
	expTime := time.Time{}

	_, err = o.Put(c, paths.New(fileHeader.Filename), file, meta, expTime)
	if err != nil {
		c.String(http.StatusInternalServerError, err.Error())
		return
	}

	err = s.DB.AddSong(title, description, fileHeader.Filename, user.ID)
	if err != nil {
		c.String(http.StatusInternalServerError, err.Error())
		return
	}

	c.String(http.StatusOK, fmt.Sprintf("'%s' uploaded!", title))
	return
}

func (s *Server) GetUser(c *gin.Context) {
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

	c.HTML(http.StatusOK, "user.tmpl", gin.H{
		"username": username,
		"email":    user.Email,
		"uploads":  songs,
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

	c.String(http.StatusOK, "Successfully deleted your account")
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

func getHashFrom(salt []byte) []byte {
	h := sha512.New()
	h.Write(salt)
	return h.Sum(nil)
}

func (s *Server) getCurrentUserFromDbBy(session sessions.Session) (db.User, error) {
	var user db.User

	userID, err := getCurrentUserFrom(session)
	if err != nil {
		return user, err
	}

	user, err = s.DB.GetUserByID(userID)
	if err != nil {
		return user, err
	}

	return user, nil
}
