package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"maps"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Alok-CS-2022/DealForge/backend/internal/db"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

const sessionFixtureToken = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"

type sessionStoreStub struct {
	loginStoreStub
	lookup func([]byte) (db.GetActiveSessionUserRow, error)
}

func (s *sessionStoreStub) GetActiveSessionUser(_ context.Context, hash []byte) (db.GetActiveSessionUserRow, error) {
	return s.lookup(hash)
}

func TestCurrentUserAcceptsSessionCookie(t *testing.T) {
	const userID = "b108fc21-3825-4a44-a41a-01aadca295b7"
	var id pgtype.UUID
	if err := id.Scan(userID); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256([]byte(sessionFixtureToken))
	store := &sessionStoreStub{lookup: func(hash []byte) (db.GetActiveSessionUserRow, error) {
		if !bytes.Equal(hash, digest[:]) {
			t.Fatal("database lookup did not receive the cookie digest")
		}
		return db.GetActiveSessionUserRow{ID: id, Username: "alice", Email: "alice@example.test", DisplayName: "Alice"}, nil
	}}
	for _, test := range []struct {
		name   string
		secure bool
		cookie string
	}{
		{"Secure", true, "__Host-dealforge_session"},
		{"Development", false, "dealforge_session"},
	} {
		t.Run(test.name, func(t *testing.T) {
			response := getCurrentUser(routes(nil, store, nil, test.secure), &http.Cookie{Name: test.cookie, Value: sessionFixtureToken})
			if response.Code != http.StatusOK || response.Header().Get("Cache-Control") != "no-store" ||
				response.Header().Get("Set-Cookie") != "" {
				t.Fatalf("unexpected profile response, status %d", response.Code)
			}
			assertPublicProfile(t, response, userID, "alice", "alice@example.test", "Alice")
		})
	}
}

func TestCurrentUserRejectsInvalidSessions(t *testing.T) {
	validCookie := &http.Cookie{Name: "__Host-dealforge_session", Value: sessionFixtureToken}
	for _, test := range []struct {
		name    string
		cookies []*http.Cookie
		lookup  bool
	}{
		{name: "missing"},
		{name: "wrong cookie name", cookies: []*http.Cookie{{Name: "dealforge_session", Value: sessionFixtureToken}}},
		{name: "malformed", cookies: []*http.Cookie{{Name: validCookie.Name, Value: "invalid"}}},
		{name: "noncanonical encoding", cookies: []*http.Cookie{{Name: validCookie.Name, Value: sessionFixtureToken[:42] + "B"}}},
		{name: "duplicate cookies", cookies: []*http.Cookie{validCookie, validCookie}},
		{name: "unknown", cookies: []*http.Cookie{validCookie}, lookup: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			called := false
			store := &sessionStoreStub{lookup: func([]byte) (db.GetActiveSessionUserRow, error) {
				called = true
				return db.GetActiveSessionUserRow{}, pgx.ErrNoRows
			}}
			response := getCurrentUser(routes(nil, store, nil, true), test.cookies...)
			assertUnauthorizedSession(t, response)
			if called != test.lookup {
				t.Errorf("database lookup called = %t, want %t", called, test.lookup)
			}
		})
	}
}

func TestCurrentUserDatabaseError(t *testing.T) {
	store := &sessionStoreStub{lookup: func([]byte) (db.GetActiveSessionUserRow, error) {
		return db.GetActiveSessionUserRow{}, errors.New("private database details")
	}}
	response := getCurrentUser(routes(nil, store, nil, true), &http.Cookie{
		Name: "__Host-dealforge_session", Value: base64.RawURLEncoding.EncodeToString(make([]byte, sessionTokenSize)),
	})
	if response.Code != http.StatusInternalServerError ||
		response.Body.String() != "{\"error\":\"could not validate session\"}\n" {
		t.Fatalf("expected a generic 500, got status %d", response.Code)
	}
}

func getCurrentUser(handler http.Handler, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodGet, "/api/v1/auth/me", nil)
	for _, cookie := range cookies {
		request.AddCookie(cookie)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func assertPublicProfile(t *testing.T, response *httptest.ResponseRecorder, id, username, email, name string) {
	t.Helper()
	var body map[string]string
	want := map[string]string{"id": id, "username": username, "email": email, "display_name": name}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil || !maps.Equal(body, want) {
		t.Fatal("profile did not contain exactly the expected public fields")
	}
}

func assertUnauthorizedSession(t *testing.T, response *httptest.ResponseRecorder) {
	t.Helper()
	if response.Code != http.StatusUnauthorized || response.Body.String() != "{\"error\":\"invalid or missing session\"}\n" {
		t.Fatalf("expected a generic 401, got status %d", response.Code)
	}
}
