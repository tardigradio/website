package main

import (
	"fmt"
  "log"
	"os"
	"regexp"
  "net/http"

	"github.com/gin-gonic/gin"
)


func main() {
	port := "8080"

	if len(os.Args) > 1 {
		if matched, _ := regexp.MatchString(`^\d{2,6}$`, os.Args[1]); matched == true {
			port = os.Args[1]
		}
	}

  router := gin.Default()

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

  router.POST("/upload", func(c *gin.Context) {
		// single file
		file, _ := c.FormFile("file")
		log.Println(file.Filename)

		// Upload the file to specific dst.
		// c.SaveUploadedFile(file, dst)

		c.String(http.StatusOK, fmt.Sprintf("'%s' uploaded!", file.Filename))
	})

  router.GET("/register", func(c *gin.Context) {
    c.String(http.StatusOK, "Register")
  })

  router.GET("/login", func(c *gin.Context) {
    c.String(http.StatusOK, "login")
  })

  router.GET("/", func(c *gin.Context) {
  		c.HTML(http.StatusOK, "index.tmpl", gin.H{
  			"title": "Tardigraud.io",
  		})
  	})

	router.Run(fmt.Sprintf(":%s", port))
}
