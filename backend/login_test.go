package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Alok-CS-2022/DealForge/backend/internal/db"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

const loginPassword = " test-only login passphrase "
const validLoginJSON = `{"username":" Alice ","password":" test-only login passphrase "}`

type loginStoreStub struct {
	user      db.GetLoginUserRow
	lookupErr error
	insertErr error
	username  string
	inserted  []db.CreateSessionParams
}

func (s *loginStoreStub) GetLoginUser(_ context.Context, username string) (db.GetLoginUserRow, error) {
	s.username = username
	return s.user, s.lookupErr
}

func (s *loginStoreStub) CreateSession(_ context.Context, session db.CreateSessionParams) error {
	if s.insertErr == nil {
		s.inserted = append(s.inserted, session)
	}
	return s.insertErr
}

func loginTestUser(t *testing.T) db.GetLoginUserRow {
	t.Helper()
	var id pgtype.UUID
	if err := id.Scan("b108fc21-3825-4a44-a41a-01aadca295b7"); err != nil {
		t.Fatal(err)
	}
	hash, err := hashPassword(loginPassword)
	if err != nil {
		t.Fatal("could not hash the fixture password")
	}
	return db.GetLoginUserRow{ID: id, PasswordHash: hash}
}

func TestLoginCreatesFreshSession(t *testing.T) {
	store := &loginStoreStub{user: loginTestUser(t)}
	handler := routes(nil, store, true)
	incoming := strings.Repeat("x", 43)
	for i := range 2 {
		response := postLogin(handler, validLoginJSON, "__Host-dealforge_session", incoming)
		if response.Code != http.StatusNoContent || response.Body.Len() != 0 {
			t.Fatalf("login returned status %d or a nonempty body", response.Code)
		}
		if store.username != "Alice" || len(store.inserted) != i+1 {
			t.Fatal("login did not look up the trimmed username and insert one session")
		}
		cookies := response.Result().Cookies()
		if len(cookies) != 1 {
			t.Fatal("expected one session cookie")
		}
		cookie := cookies[0]
		if cookie.Name != "__Host-dealforge_session" || !cookie.HttpOnly || !cookie.Secure ||
			cookie.SameSite != http.SameSiteLaxMode || cookie.Path != "/" || cookie.Domain != "" {
			t.Fatal("wrong session cookie security attributes")
		}
		token, err := base64.RawURLEncoding.DecodeString(cookie.Value)
		if err != nil || len(token) != 32 || cookie.Value == incoming {
			t.Fatal("login did not issue a fresh 32-byte token")
		}
		if response.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("missing no-store header")
		}
		saved := store.inserted[i]
		digest := sha256.Sum256([]byte(cookie.Value))
		if saved.UserID != store.user.ID || !bytes.Equal(saved.TokenHash, digest[:]) ||
			bytes.Equal(saved.TokenHash, token) {
			t.Fatal("session was not saved using only the cookie token digest")
		}
		if !saved.ExpiresAt.Valid || !saved.ExpiresAt.Time.Equal(cookie.Expires) ||
			cookie.MaxAge != int(sessionLifetime/time.Second) ||
			time.Until(cookie.Expires) <= sessionLifetime-time.Minute || time.Until(cookie.Expires) > sessionLifetime {
			t.Fatal("session and cookie expiration do not agree")
		}
		incoming = cookie.Value
	}
}

func TestLoginInsecureDevelopmentCookie(t *testing.T) {
	store := &loginStoreStub{user: loginTestUser(t)}
	response := postLogin(routes(nil, store, false), validLoginJSON, "", "")
	cookies := response.Result().Cookies()
	if response.Code != http.StatusNoContent || len(cookies) != 1 ||
		cookies[0].Name != "dealforge_session" || cookies[0].Secure || !cookies[0].HttpOnly ||
		cookies[0].SameSite != http.SameSiteLaxMode {
		t.Fatal("development cookie configuration did not preserve the other protections")
	}
}

func TestLoginRejectsInvalidCredentials(t *testing.T) {
	user := loginTestUser(t)
	for _, test := range []struct {
		name  string
		store loginStoreStub
		body  string
	}{
		{"wrong password", loginStoreStub{user: user}, `{"username":"Alice","password":"incorrect"}`},
		{"unknown username", loginStoreStub{lookupErr: pgx.ErrNoRows}, validLoginJSON},
		{"unsupported stored cost", loginStoreStub{user: db.GetLoginUserRow{
			ID: user.ID, PasswordHash: strings.Replace(user.PasswordHash, "m=65536", "m=999999999", 1),
		}}, validLoginJSON},
	} {
		t.Run(test.name, func(t *testing.T) {
			response := postLogin(routes(nil, &test.store, true), test.body, "", "")
			if response.Code != http.StatusUnauthorized ||
				response.Body.String() != "{\"error\":\"invalid username or password\"}\n" ||
				response.Header().Get("Set-Cookie") != "" || len(test.store.inserted) != 0 {
				t.Fatalf("expected generic 401 without a session, got %d", response.Code)
			}
		})
	}
}

func TestLoginDatabaseFailures(t *testing.T) {
	failure := errors.New("private database details")
	for name, store := range map[string]*loginStoreStub{
		"user lookup":    {lookupErr: failure},
		"session insert": {user: loginTestUser(t), insertErr: failure},
	} {
		t.Run(name, func(t *testing.T) {
			response := postLogin(routes(nil, store, true), validLoginJSON, "", "")
			if response.Code != http.StatusInternalServerError ||
				response.Body.String() != "{\"error\":\"could not sign in\"}\n" ||
				response.Header().Get("Set-Cookie") != "" {
				t.Fatalf("expected safe 500 without a cookie, got %d", response.Code)
			}
		})
	}
}

func TestLoginRequiresJSON(t *testing.T) {
	store := &loginStoreStub{}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(validLoginJSON))
	request.Header.Set("Content-Type", "text/plain")
	response := httptest.NewRecorder()
	routes(nil, store, true).ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest || store.username != "" {
		t.Fatal("login accepted a non-JSON request")
	}
}

func postLogin(handler http.Handler, body, cookieName, token string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	if cookieName != "" {
		request.AddCookie(&http.Cookie{Name: cookieName, Value: token})
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}
