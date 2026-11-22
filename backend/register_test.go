package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"maps"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Alok-CS-2022/DealForge/backend/internal/db"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"golang.org/x/crypto/argon2"
)

type createUserFunc func(context.Context, db.CreateUserParams) (db.CreateUserRow, error)

func (create createUserFunc) CreateUser(ctx context.Context, params db.CreateUserParams) (db.CreateUserRow, error) {
	return create(ctx, params)
}

const registrationJSON = `{"username":" Alice_1 ","email":" Alice@EXAMPLE.COM ","display_name":" Alice Example ","password":" test-only passphrase "}`

func TestRegisterUser(t *testing.T) {
	const userID = "b108fc21-3825-4a44-a41a-01aadca295b7"
	var id pgtype.UUID
	if err := id.Scan(userID); err != nil {
		t.Fatal(err)
	}
	var previousSalt string
	handler := routes(createUserFunc(func(_ context.Context, params db.CreateUserParams) (db.CreateUserRow, error) {
		if params.Username != "alice_1" || params.Email != "alice@example.com" || params.DisplayName != "Alice Example" {
			t.Fatal("stored fields were not normalized")
		}
		parts := strings.Split(params.PasswordHash, "$")
		if len(parts) != 6 || parts[1] != "argon2id" || parts[2] != "v=19" || parts[3] != "m=65536,t=3,p=4" {
			t.Fatal("stored password is not the expected Argon2id encoding")
		}
		if parts[4] == previousSalt {
			t.Fatal("registration reused a salt")
		}
		previousSalt = parts[4]
		salt, err := base64.RawStdEncoding.DecodeString(parts[4])
		if err != nil || len(salt) != 16 {
			t.Fatal("stored salt is invalid")
		}
		wantHash := argon2.IDKey([]byte(" test-only passphrase "), salt, 3, 65536, 4, 32)
		if parts[5] != base64.RawStdEncoding.EncodeToString(wantHash) {
			t.Fatal("hash did not preserve the original password")
		}
		return db.CreateUserRow{ID: id, Username: params.Username, Email: params.Email, DisplayName: params.DisplayName}, nil
	}))

	want := map[string]string{"id": userID, "username": "alice_1", "email": "alice@example.com", "display_name": "Alice Example"}
	for range 2 {
		response := postRegistration(handler, registrationJSON)
		if response.Code != http.StatusCreated || response.Header().Get("Content-Type") != "application/json" {
			t.Fatalf("unexpected response status or type: %d", response.Code)
		}
		var body map[string]string
		if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil || !maps.Equal(body, want) {
			t.Fatal("response did not contain exactly the expected public fields")
		}
	}
}

func TestRegisterInvalidInput(t *testing.T) {
	handler := routes(createUserFunc(func(context.Context, db.CreateUserParams) (db.CreateUserRow, error) {
		t.Error("invalid registration reached the database")
		return db.CreateUserRow{}, nil
	}))
	for name, body := range map[string]string{
		"malformed JSON":       `{"username":`,
		"missing fields":       `{}`,
		"invalid username":     strings.Replace(registrationJSON, "Alice_1", "x", 1),
		"invalid email":        strings.Replace(registrationJSON, "Alice@EXAMPLE.COM", "invalid-address", 1),
		"missing display name": strings.Replace(registrationJSON, "Alice Example", "", 1),
		"long display name":    strings.Replace(registrationJSON, "Alice Example", strings.Repeat("a", 101), 1),
		"short password":       strings.Replace(registrationJSON, "test-only passphrase", "short", 1),
		"long password":        strings.Replace(registrationJSON, "test-only passphrase", strings.Repeat("p", 257), 1),
		"trailing JSON":        registrationJSON + `{}`,
		"oversized body":       registrationJSON + strings.Repeat(" ", 8192),
	} {
		t.Run(name, func(t *testing.T) {
			if response := postRegistration(handler, body); response.Code != http.StatusBadRequest {
				t.Errorf("status = %d, want 400", response.Code)
			}
		})
	}
}

func TestRegisterDatabaseErrors(t *testing.T) {
	for _, test := range []struct {
		name, constraint, code string
		status                 int
		message                string
	}{
		{"duplicate username", "users_username_key", "23505", http.StatusConflict, "username or email already registered"},
		{"duplicate email", "users_email_key", "23505", http.StatusConflict, "username or email already registered"},
		{"other database error", "users_pkey", "23505", http.StatusInternalServerError, "could not register user"},
	} {
		t.Run(test.name, func(t *testing.T) {
			handler := routes(createUserFunc(func(context.Context, db.CreateUserParams) (db.CreateUserRow, error) {
				return db.CreateUserRow{}, &pgconn.PgError{
					Code: test.code, ConstraintName: test.constraint, Detail: "internal database detail",
				}
			}))
			response := postRegistration(handler, registrationJSON)
			if response.Code != test.status {
				t.Errorf("status = %d, want %d", response.Code, test.status)
			}
			var body map[string]string
			if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil ||
				!maps.Equal(body, map[string]string{"error": test.message}) {
				t.Error("response did not contain exactly the expected public error")
			}
		})
	}
}

func postRegistration(handler http.Handler, body string) *httptest.ResponseRecorder {
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/auth/register", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	handler.ServeHTTP(response, request)
	return response
}
