package main

import (
	"log"
	"net/http"
	"time"

	"shortener/internal/app"
	"shortener/internal/auth"
	"shortener/internal/storage"
)

func main() {
	const addr = "localhost:8080"
	const baseURL = "http://" + addr

	cookies, err := auth.New(false)
	if err != nil {
		log.Fatalf("create cookie signer: %v", err)
	}

	server := &http.Server{
		Addr:              addr,
		Handler:           app.New(baseURL, storage.New(), cookies),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	log.Printf("listening on %s", baseURL)
	if err := server.ListenAndServe(); err != nil {
		log.Fatal(err)
	}
}
