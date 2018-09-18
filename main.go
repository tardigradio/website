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
	"storj.io/storj/pkg/storage/objects"
	"storj.io/storj/storage"
)

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
	// clientCfg := &miniogw.ClientConfig{
	// 	OverlayAddr:   "satellite.staging.storj.io:7777",
	// 	PointerDBAddr: "satellite.staging.storj.io:7777",
	// 	APIKey:        "CribRetrievableEyebrows",
	// }
	//
	// cfg := &miniogw.Config{
	// 	ClientConfig{clientCfg},
	// }

	context := context.Background()
	port := "8080"

	if len(os.Args) > 1 {
		if matched, _ := regexp.MatchString(`^\d{2,6}$`, os.Args[1]); matched == true {
			port = os.Args[1]
		}
	}

	router := gin.Default()

	store := cookie.NewStore([]byte("secret"))
	router.Use(sessions.Sessions("mysession", store))

	usr, err := user.Current()
	if err != nil {
		log.Fatal(err)
	}

	identityCfg := provider.IdentityConfig{
		CertPath: filepath.Join(usr.HomeDir, ".storj/uplink/identity.cert"),
		KeyPath:  filepath.Join(usr.HomeDir, ".storj/uplink/identity.key"),
		Address:  ":7777",
	}

	minioCfg := miniogw.MinioConfig{
		AccessKey: "3ee4E2vqy3myfKdPnuPKTQQavtqx",
		SecretKey: "3H1BL6sKtiRCrs9VxCbw9xboYsXp",
		MinioDir:  filepath.Join(usr.HomeDir, ".storj/uplink/miniogw"),
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

	fmt.Println(cfg)
	bs, err := cfg.BucketStore(context)

	database, err := db.Open(context, filepath.Join(usr.HomeDir, "/.tardigraudio"))
	if err != nil {
		panic(err)
	}
	defer database.Close()

	router.LoadHTMLGlob("templates/*")

	router.GET("/user/:name", func(c *gin.Context) {
		username := c.Param("name")
		email := ""
		uploads := []string{}
		c.HTML(http.StatusOK, "user.tmpl", gin.H{
			"username": username,
			"email":    email,
			"uploads":  uploads,
		})
	})

	router.GET("/user/:name/*song", func(c *gin.Context) {
		username := c.Param("name")
		song := c.Param("song")
		c.HTML(http.StatusOK, "song.tmpl", gin.H{
			"username": username,
			"song":     song,
		})
	})

	user := router.Group("/useraction")
	user.Use(AuthRequired())
	{
		user.GET("/logout", func(c *gin.Context) {
			session := sessions.Default(c)

			session.Delete("user")
			session.Save()

		})

		user.GET("/upload", func(c *gin.Context) {
			c.HTML(http.StatusOK, "upload.tmpl", gin.H{})
		})

		user.POST("/upload", func(c *gin.Context) {
			session := sessions.Default(c)
			// single file
			title := c.PostForm("songTitle")
			description := c.PostForm("songDesc")

			var user db.User
			user, err := database.GetUser(fmt.Sprintf("%s", session.Get("user")))
			if err != nil {
				log.Fatal(err)
			}

			fileHeader, _ := c.FormFile("file")
			log.Println(fileHeader.Filename, title, description)

			file, err := fileHeader.Open()
			if err != nil {
				log.Fatal(err)
			}
			// Upload the file to STORJ
			// TODO: Add song to bucket sj://username
			o, err := bs.GetObjectStore(context, user.Username)
			if err != nil {
				log.Fatal(err)
			}
			meta := objects.SerializableMeta{}
			expTime := time.Time{}

			_, err = o.Put(context, paths.New(fileHeader.Filename), file, meta, expTime)
			if err != nil {
				log.Fatal(err)
			}

			database.AddSong(title, description, user.ID)

			// Redirect to homepage
			c.Request.URL.Path = "/"
			router.HandleContext(c)
		})
	}

	router.GET("/register", func(c *gin.Context) {
		c.HTML(http.StatusOK, "register.tmpl", gin.H{})
	})

	router.POST("/register", func(c *gin.Context) {
		session := sessions.Default(c)
		email := c.PostForm("email")
		username := c.PostForm("username")
		password := c.PostForm("password")

		h := sha512.New()
		h.Write([]byte(password))

		err := database.AddUser(email, username, h.Sum(nil))
		if err != nil {
			log.Println(err)
			c.String(http.StatusInternalServerError, "Failed to register")
			return
		} else {
			// TODO: Create bucket for user with the same name as the user sj://username
			_, err = bs.Get(context, username)
			if err == nil {
				log.Fatal("Bucket already exists")
			}
			if !storage.ErrKeyNotFound.Has(err) {
				log.Fatal(err)
			}
			_, err = bs.Put(context, username)
			if err != nil {
				log.Fatal(err)
			}

			fmt.Printf("Bucket %s created\n", username)

			session.Set("user", username)
			session.Save()
			c.String(http.StatusOK, fmt.Sprintf("'%s' registered!", username))
		}
	})

	router.GET("/login", func(c *gin.Context) {
		c.HTML(http.StatusOK, "login.tmpl", gin.H{})
	})

	router.POST("/login", func(c *gin.Context) {
		session := sessions.Default(c)

		username := c.PostForm("username")
		password := c.PostForm("password")

		h := sha512.New()
		h.Write([]byte(password))
		hash := h.Sum(nil)

		user, err := database.GetUser(username)
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
	})

	router.GET("/", func(c *gin.Context) {
		session := sessions.Default(c)
		var popular []string
		var recent []string

		c.HTML(http.StatusOK, "index.tmpl", gin.H{
			"title":       "Tardigraud.io",
			"popular":     popular,
			"recent":      recent,
			"currentUser": session.Get("user"),
		})
	})

	router.Run(fmt.Sprintf(":%s", port))
}
