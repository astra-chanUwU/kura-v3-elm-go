package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/astra-chanUwU/kura-v3-elm-go/server/internal/httpapi"
	"github.com/astra-chanUwU/kura-v3-elm-go/server/internal/posts"
)

func main() {
	addr := os.Getenv("HTTP_ADDR")
	if addr == "" {
		addr = ":8080"
	}

	var searcher posts.Searcher
	var closeSearcher func()
	if databaseURL := strings.TrimSpace(os.Getenv("DATABASE_URL")); databaseURL != "" {
		pgSearcher, err := posts.NewPostgresSearcher(context.Background(), databaseURL)
		if err != nil {
			log.Printf("postgres search unavailable: %v", err)
		} else {
			searcher = pgSearcher
			closeSearcher = pgSearcher.Close
		}
	}
	if closeSearcher != nil {
		defer closeSearcher()
	}

	log.Printf("kura-server listening on %s", addr)
	if err := http.ListenAndServe(addr, httpapi.NewRouterWithToken(os.Getenv("KURA_API_TOKEN"), searcher)); err != nil {
		log.Fatal(err)
	}
}
