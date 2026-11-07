package main

import (
	"log"
	"os"

	"github.com/gofiber/fiber/v2"
)

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "3000"
	}
	host := os.Getenv("HOST")
	if host == "" {
		host = "127.0.0.1"
	}
	app := fiber.New(fiber.Config{DisableStartupMessage: true})

	app.Get("/text", func(c *fiber.Ctx) error {
		return c.SendString("hello world")
	})
	app.Get("/json", func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{"message": "hello world"})
	})

	log.Printf("fiber listening on :%s", port)
	log.Fatal(app.Listen(host + ":" + port))
}
