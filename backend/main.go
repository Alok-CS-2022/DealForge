package main

import (
	"io"
	"log"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
)

func main() {
	server := &http.Server{
		Addr:              "127.0.0.1:8080",
		Handler:           routes(),
		ReadHeaderTimeout: 5 * time.Second,
	}
	log.Printf("Listening on http://%s", server.Addr)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}

func routes() http.Handler {
	router := chi.NewRouter()
	router.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if _, err := io.WriteString(w, "{\"status\":\"ok\"}\n"); err != nil {
			log.Printf("write health response: %v", err)
		}
	})
	return router
}
