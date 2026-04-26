package main

import (
	"log"

	"markdown-viewer/internal/server"
	"markdown-viewer/internal/session"
)

func main() {
	if err := session.RestoreFromDisk(); err != nil {
		log.Printf("Failed to restore sessions: %v", err)
	}

	srv := server.New()
	if err := srv.Run(); err != nil {
		log.Fatal(err)
	}
}
