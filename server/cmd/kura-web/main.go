package main

import (
	"log"
	"net/http"
)

func main() {
	log.Print("Kura frontend: http://localhost:8000")
	if err := http.ListenAndServe("127.0.0.1:8000", http.FileServer(http.Dir("../web"))); err != nil {
		log.Fatal(err)
	}
}
