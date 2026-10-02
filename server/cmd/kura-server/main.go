package main

import (
	"log"
	"net/http"
	"os"

	"github.com/astra-chanUwU/kura-v3-elm-go/server/internal/httpapi"
)

func main() {
	addr := os.Getenv("HTTP_ADDR")
	if addr == "" {
		addr = ":8080"
	}

	log.Printf("kura-server listening on %s", addr)
	if err := http.ListenAndServe(addr, httpapi.NewRouter()); err != nil {
		log.Fatal(err)
	}
}
