package main

import (
	"context"
	"io"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/Alok-CS-2022/DealForge/backend/internal/db"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func main() {
	var users userCreator
	if os.Getenv("PGDATABASE") != "" {
		pool, err := pgxpool.New(context.Background(), "")
		if err != nil {
			log.Fatal("invalid PostgreSQL configuration")
		}
		defer pool.Close()
		users = db.New(pool)
	}

	server := &http.Server{
		Addr:              "127.0.0.1:8080",
		Handler:           routes(users),
		ReadHeaderTimeout: 5 * time.Second,
	}
	log.Printf("Listening on http://%s", server.Addr)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}

func routes(users userCreator) http.Handler {
	router := chi.NewRouter()
	router.Post("/api/v1/auth/register", registerUser(users))
	router.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if _, err := io.WriteString(w, "{\"status\":\"ok\"}\n"); err != nil {
			log.Printf("write health response: %v", err)
		}
	})
	return router
}
