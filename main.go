package main

import (
	"log"
	"net/http"

	"forum-authentication/internal/chat"
	"forum-authentication/internal/repository"
	"forum-authentication/internal/server"
	"forum-authentication/internal/templating"
)

const (
	addr       = ":8080"
	dbPath     = "./forum.db"
	staticDir  = "./ui/static/"
	templatesD = "./ui/templates/"
)

func main() {
	repo, err := repository.Open(dbPath)
	if err != nil {
		log.Fatalf("init db: %v", err)
	}
	defer repo.Close()

	templates, err := templating.Load(templatesD)
	if err != nil {
		log.Fatalf("load templates: %v", err)
	}

	hub := chat.NewHub()
	mux := server.New(templates, repo, hub, staticDir)

	log.Printf("Forum запущен на http://localhost%s", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatalf("server: %v", err)
	}
}
