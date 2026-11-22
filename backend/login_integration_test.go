package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Alok-CS-2022/DealForge/backend/internal/db"
	"github.com/jackc/pgx/v5"
)

func TestLoginPersistsSessionInPostgres(t *testing.T) {
	if os.Getenv("DEALFORGE_LOGIN_INTEGRATION") != "1" {
		t.Skip("set DEALFORGE_LOGIN_INTEGRATION=1 and the local PostgreSQL environment")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	conn, err := pgx.Connect(ctx, "")
	if err != nil {
		t.Fatal("could not connect to PostgreSQL for integration test")
	}
	defer conn.Close(context.Background())
	tx, err := conn.Begin(ctx)
	if err != nil {
		t.Fatal("could not start integration test transaction")
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := tx.Rollback(cleanup); err != nil && !errors.Is(err, pgx.ErrTxClosed) {
			t.Error("could not roll back integration test data")
		}
	}()

	queries := db.New(tx)
	username := "it_" + strings.ToLower(rand.Text())[:20]
	hash, err := hashPassword(loginPassword)
	if err != nil {
		t.Fatal("could not hash the fixture password")
	}
	user, err := queries.CreateUser(ctx, db.CreateUserParams{
		Username: username, Email: username + "@example.test",
		DisplayName: "Login integration test", PasswordHash: hash,
	})
	if err != nil {
		t.Fatal("could not create integration test user")
	}
	payload, err := json.Marshal(map[string]string{"username": strings.ToUpper(username), "password": loginPassword})
	if err != nil {
		t.Fatal("could not encode login input")
	}
	response := postLogin(routes(queries, queries, true), string(payload), "", "")
	cookies := response.Result().Cookies()
	if response.Code != http.StatusNoContent || len(cookies) != 1 {
		t.Fatalf("expected a successful PostgreSQL-backed login, got %d", response.Code)
	}
	var storedHash []byte
	var expiresAt time.Time
	err = tx.QueryRow(ctx, `SELECT token_hash, expires_at FROM dealforge.sessions WHERE user_id = $1`, user.ID).
		Scan(&storedHash, &expiresAt)
	if err != nil {
		t.Fatal("could not read the created session")
	}
	want := sha256.Sum256([]byte(cookies[0].Value))
	if !bytes.Equal(storedHash, want[:]) || !expiresAt.Equal(cookies[0].Expires) {
		t.Fatal("stored session did not match the cookie digest and expiry")
	}
}
