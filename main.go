package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/user"
	"path/filepath"
	"regexp"

	"github.com/gin-gonic/gin"
	"github.com/tardigraudio/website/db"
)

func main() {
	context := context.Background()
	port := "8080"

	if len(os.Args) > 1 {
		if matched, _ := regexp.MatchString(`^\d{2,6}$`, os.Args[1]); matched == true {
			port = os.Args[1]
		}
	}

	router := gin.Default()

	usr, err := user.Current()
	if err != nil {
		log.Fatal(err)
	}

	database, err := db.Open(context, filepath.Join(usr.HomeDir, "/.tardigraudio"))
	if err != nil {
		panic(err)
	}

	router.LoadHTMLGlob("templates/*")

	router.GET("/user/:name", func(c *gin.Context) {
		name := c.Param("name")
		c.String(http.StatusOK, "Hello %s", name)
	})

	router.GET("/user/:name/*song", func(c *gin.Context) {
		name := c.Param("name")
		song := c.Param("song")
		c.String(http.StatusOK, "%s: %s", name, song)
	})

	router.GET("/upload", func(c *gin.Context) {
		c.HTML(http.StatusOK, "upload.tmpl", gin.H{})
	})

	router.POST("/upload", func(c *gin.Context) {
		// single file
		title := c.PostForm("songTitle")
		description := c.PostForm("songDesc")
    user_id := 0

		file, _ := c.FormFile("file")
		log.Println(file.Filename, title, description)

		// Upload the file to STORJ
		database.AddSong(title, description, user_id)

		c.String(http.StatusOK, fmt.Sprintf("'%s' uploaded!", file.Filename))
	})

	router.GET("/register", func(c *gin.Context) {
		c.String(http.StatusOK, "Register")
	})

	router.GET("/login", func(c *gin.Context) {
		c.String(http.StatusOK, "login")
	})

	router.GET("/", func(c *gin.Context) {
		var popular []string
		var recent []string
		c.HTML(http.StatusOK, "index.tmpl", gin.H{
			"title":   "Tardigraud.io",
			"popular": popular,
			"recent":  recent,
		})
	})

	router.Run(fmt.Sprintf(":%s", port))
}
