package main

import (
	"log"

	"github.com/joho/godotenv"

	"markdown-viewer/internal/server"
	"markdown-viewer/internal/session"
)

func main() {
	err := godotenv.Load()
	if err != nil {
		log.Fatal("Error loading .env file")
	}

	if err := session.RestoreFromDisk(); err != nil {
		log.Printf("Failed to restore sessions: %v", err)
	}

	srv := server.New()
	if err := srv.Run(); err != nil {
		log.Fatal(err)
	}
}
