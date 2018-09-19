package main

import (
	"bytes"
	"context"
	"crypto/sha512"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/user"
	"path/filepath"
	"regexp"
	"time"

	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	"github.com/tardigraudio/website/db"
	"storj.io/storj/cmd/uplink/cmd"
	"storj.io/storj/pkg/miniogw"
	"storj.io/storj/pkg/paths"
	"storj.io/storj/pkg/provider"
	"storj.io/storj/pkg/storage/buckets"
	"storj.io/storj/pkg/storage/objects"
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

	database, err := db.Open(ctx, filepath.Join(usr.HomeDir, "/.tardigraudio"))
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
	var popular []string
	var recent []string

	c.HTML(http.StatusOK, "index.tmpl", gin.H{
		"title":       "Tardigraud.io",
		"popular":     popular,
		"recent":      recent,
		"currentUser": session.Get("user"),
	})
}

func (s *Server) PostLogin(c *gin.Context) {
	session := sessions.Default(c)

	username := c.PostForm("username")
	password := c.PostForm("password")

	h := sha512.New()
	h.Write([]byte(password))
	hash := h.Sum(nil)

	user, err := s.DB.GetUser(username)
	if err != nil {
		c.String(http.StatusInternalServerError, "Invalid username or password")
		return
	}

	if !bytes.Equal(hash, user.Hash) {
		c.String(http.StatusInternalServerError, "Invalid username or password")
		return
	}

	session.Set("user", username)
	session.Save()

	c.String(http.StatusOK, fmt.Sprintf("'%s' logged in!", username))
}

func (s *Server) GetLogin(c *gin.Context) {
	c.HTML(http.StatusOK, "login.tmpl", gin.H{})
}

func (s *Server) PostRegister(c *gin.Context) {
	session := sessions.Default(c)
	email := c.PostForm("email")
	username := c.PostForm("username")
	password := c.PostForm("password")

	h := sha512.New()
	h.Write([]byte(password))

	err := s.DB.AddUser(email, username, h.Sum(nil))
	if err != nil {
		log.Println(err)
		c.String(http.StatusInternalServerError, "Failed to register")
		return
	} else {
		// TODO: Create bucket for user with the same name as the user sj://username
		_, err = s.bs.Get(c, username)
		if err == nil {
			log.Fatal("Bucket already exists")
		}
		if !storage.ErrKeyNotFound.Has(err) {
			log.Fatal(err)
		}
		_, err = s.bs.Put(c, username)
		if err != nil {
			log.Fatal(err)
		}

		fmt.Printf("Bucket %s created\n", username)

		session.Set("user", username)
		session.Save()
		c.String(http.StatusOK, fmt.Sprintf("'%s' registered!", username))
	}
}

func (s *Server) GetRegister(c *gin.Context) {
	c.HTML(http.StatusOK, "register.tmpl", gin.H{})
}

func (s *Server) GetLogout(c *gin.Context) {
	session := sessions.Default(c)

	session.Delete("user")
	session.Save()

	c.String(http.StatusOK, "Successfully Logged out")
	s.r.HandleContext(c)
}

func (s *Server) GetSong(c *gin.Context) {
	// session := sessions.Default(c)
	username := c.Param("name")
	song := c.Param("song")
	c.HTML(http.StatusOK, "song.tmpl", gin.H{
		"username": username,
		"song":     song,
	})
}

func (s *Server) GetUpload(c *gin.Context) {
	session := sessions.Default(c)

	c.HTML(http.StatusOK, "upload.tmpl", gin.H{
		"currentUser": session.Get("user"),
	})
}

func (s *Server) PostUpload(c *gin.Context) {
	session := sessions.Default(c)
	// single file
	title := c.PostForm("songTitle")
	description := c.PostForm("songDesc")

	var user db.User
	user, err := s.DB.GetUser(fmt.Sprintf("%s", session.Get("user")))
	if err != nil {
		log.Fatal(err)
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
		log.Fatal(err)
	}
	meta := objects.SerializableMeta{}
	expTime := time.Time{}

	_, err = o.Put(c, paths.New(fileHeader.Filename), file, meta, expTime)
	if err != nil {
		log.Fatal(err)
	}

	err = s.DB.AddSong(title, description, user.ID)
	if err != nil {
		log.Fatal(err)
	}

	c.String(http.StatusOK, fmt.Sprintf("'%s' uploaded!", title))
	s.r.HandleContext(c)
}

func (s *Server) GetUser(c *gin.Context) {
	session := sessions.Default(c)
	username := c.Param("name")

	var user db.User
	user, err := s.DB.GetUser(fmt.Sprintf("%s", session.Get("user")))
	if err != nil {
		log.Fatal(err)
	}

	uploads := []string{}
	c.HTML(http.StatusOK, "user.tmpl", gin.H{
		"username": username,
		"email":    user.Email,
		"uploads":  uploads,
	})
}

func getBucketStore(ctx context.Context, homeDir string) (buckets.Store, error) {
	identityCfg := provider.IdentityConfig{
		CertPath: filepath.Join(homeDir, ".storj/uplink/identity.cert"),
		KeyPath:  filepath.Join(homeDir, ".storj/uplink/identity.key"),
		Address:  ":7777",
	}

	minioCfg := miniogw.MinioConfig{
		AccessKey: "3ee4E2vqy3myfKdPnuPKTQQavtqx",
		SecretKey: "3H1BL6sKtiRCrs9VxCbw9xboYsXp",
		MinioDir:  filepath.Join(homeDir, ".storj/uplink/miniogw"),
	}

	clientCfg := miniogw.ClientConfig{
		OverlayAddr:   "satellite.staging.storj.io:7777",
		PointerDBAddr: "satellite.staging.storj.io:7777",
		APIKey:        "CribRetrievableEyebrows",
		MaxInlineSize: 4096,
		SegmentSize:   64000000,
	}

	rsCfg := miniogw.RSConfig{
		MaxBufferMem:     0x400000,
		ErasureShareSize: 1024,
		MinThreshold:     20,
		RepairThreshold:  30,
		SuccessThreshold: 40,
		MaxThreshold:     50,
	}

	storjCfg := miniogw.Config{
		identityCfg,
		minioCfg,
		clientCfg,
		rsCfg,
	}

	cfg := &cmd.Config{storjCfg}

	return cfg.BucketStore(ctx)
}

func AuthRequired() gin.HandlerFunc {
	return func(c *gin.Context) {
		session := sessions.Default(c)
		user := session.Get("user")
		if user == nil {
			// You'd normally redirect to login page
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid session token"})
		} else {
			// Continue down the chain to handler etc
			c.Next()
		}
	}
}

func main() {
	ctx := context.Background()
	port := "8080"

	if len(os.Args) > 1 {
		if matched, _ := regexp.MatchString(`^\d{2,6}$`, os.Args[1]); matched == true {
			port = os.Args[1]
		}
	}

	server := Initialize(ctx)
	defer server.Close()

	server.r.GET("/", server.GetRoot)

	server.r.LoadHTMLGlob("templates/*")
	server.r.Static("assets/css", "assets/css")

	private := server.r.Group("/useraction")
	private.Use(AuthRequired())
	{
		private.GET("/logout", server.GetLogout)
		private.GET("/upload", server.GetUpload)
		private.POST("/upload", server.PostUpload)
	}

	server.r.GET("/user/:name", server.GetUser)
	server.r.GET("/user/:name/*song", server.GetSong)
	server.r.GET("/register", server.GetRegister)
	server.r.POST("/register", server.PostRegister)
	server.r.GET("/login", server.GetLogin)
	server.r.POST("/login", server.PostLogin)

	server.Run(fmt.Sprintf(":%s", port))
}
