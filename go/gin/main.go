package main

import (
	"log"
	"net/http"
	"os"

	"github.com/gin-gonic/gin"
)

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "3000"
	}
	gin.SetMode(gin.ReleaseMode)
	r := gin.New() // no logger / recovery middleware

	r.GET("/text", func(c *gin.Context) {
		c.String(http.StatusOK, "hello world")
	})
	r.GET("/json", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"message": "hello world"})
	})

	log.Printf("gin listening on :%s", port)
	log.Fatal(r.Run("127.0.0.1:" + port))
}
