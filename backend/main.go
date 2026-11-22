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
	var sessions authSessionStore
	if os.Getenv("PGDATABASE") != "" {
		pool, err := pgxpool.New(context.Background(), "")
		if err != nil {
			log.Fatal("invalid PostgreSQL configuration")
		}
		defer pool.Close()
		queries := db.New(pool)
		users = queries
		sessions = queries
	}

	server := &http.Server{
		Addr:              "127.0.0.1:8080",
		Handler:           routes(users, sessions, os.Getenv("DEALFORGE_INSECURE_DEV_COOKIE") != "true"),
		ReadHeaderTimeout: 5 * time.Second,
	}
	log.Printf("Listening on http://%s", server.Addr)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}

func routes(users userCreator, sessions authSessionStore, secureCookie bool) http.Handler {
	router := chi.NewRouter()
	// Share the Argon2id memory budget across both auth endpoints.
	hashSlots := make(chan struct{}, 2)
	router.Post("/api/v1/auth/register", registerUser(users, hashSlots))
	router.Post("/api/v1/auth/login", loginUser(sessions, hashSlots, secureCookie))
	router.Get("/api/v1/auth/me", requireSession(sessions, secureCookie, currentUser))
	router.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if _, err := io.WriteString(w, "{\"status\":\"ok\"}\n"); err != nil {
			log.Printf("write health response: %v", err)
		}
	})
	return router
}
