package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"regexp"

	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
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

func GuestRequired() gin.HandlerFunc {
	return func(c *gin.Context) {
		session := sessions.Default(c)
		_, err := getCurrentUserFrom(session)
		if err == nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "You are already logged in"})
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

	private := server.r.Group("/active")
	private.Use(AuthRequired())
	{
		private.GET("/logout", server.GetLogout)
		private.GET("/upload", server.GetUpload)
		private.POST("/upload", server.PostUpload)
		private.POST("/delete", server.DeleteUser)
	}

	server.r.GET("/user/:name", server.GetUser)
	server.r.GET("/user/:name/*song", server.GetSong)

	guest := server.r.Group("/guest")
	guest.Use(GuestRequired())
	{
		guest.GET("/register", server.GetRegister)
		guest.POST("/register", server.PostRegister)
		guest.GET("/login", server.GetLogin)
		guest.POST("/login", server.PostLogin)
	}

	server.Run(fmt.Sprintf(":%s", port))
}
