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

// AuthRequired is a handler requires users to be logged in for access to specific routes
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

// GuestRequired is a handler requires users to be logged out for access to specific routes
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

	// Determine port to run server at from command line arguments
	if len(os.Args) > 1 {
		if matched, _ := regexp.MatchString(`^\d{2,6}$`, os.Args[1]); matched == true {
			port = os.Args[1]
		}
	}

	// Initialize the Server Struct
	server := Initialize(ctx)
	defer server.Close() // Cleanly shutdown server

	// Homepage
	server.r.GET("/", server.GetRoot)

	// Load Assets
	server.r.LoadHTMLGlob("templates/*")
	server.r.Static("/css", "assets/css")

	// Routes that require users to be logged in
	private := server.r.Group("/active")
	private.Use(AuthRequired())
	{
		private.GET("/logout", server.GetLogout)
		private.GET("/upload", server.GetUpload)
		private.POST("/upload", server.PostUpload)
		private.POST("/delete", server.DeleteUser)
	}

	// Public routes for user pages
	server.r.GET("/user/:name", server.GetUser)
	server.r.GET("/user/:name/*song", server.GetSong)
	server.r.POST("/user/:name/*song", server.DownloadSong)
	server.r.GET("/download/:name/*song", server.DownloadSong)

	// Routes that are only accessible if not logged in
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
