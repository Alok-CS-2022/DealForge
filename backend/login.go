package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"log"
	"mime"
	"net/http"
	"strings"
	"time"

	"github.com/Alok-CS-2022/DealForge/backend/internal/db"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

const sessionLifetime = 24 * time.Hour

type loginStore interface {
	GetLoginUser(context.Context, string) (db.GetLoginUserRow, error)
	CreateSession(context.Context, db.CreateSessionParams) error
}

func loginUser(store loginStore, hashSlots chan struct{}, secureCookie bool) http.HandlerFunc {
	cookieName := sessionCookieName(secureCookie)
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		var input struct {
			Username string `json:"username"`
			Password string `json:"password"`
		}
		mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if err != nil || mediaType != "application/json" || !decodeAuthRequest(w, r, &input) {
			writeAuthError(w, http.StatusBadRequest, "invalid login JSON")
			return
		}
		username := strings.TrimSpace(input.Username)
		if username == "" || len(username) > 32 || strings.ContainsRune(username, 0) ||
			input.Password == "" || len(input.Password) > 256 {
			writeAuthError(w, http.StatusBadRequest, "invalid username or password input")
			return
		}
		if store == nil {
			writeAuthError(w, http.StatusInternalServerError, "could not sign in")
			return
		}
		select {
		case hashSlots <- struct{}{}:
		default:
			writeAuthError(w, http.StatusServiceUnavailable, "sign-in is busy, try again")
			return
		}
		user, err := store.GetLoginUser(r.Context(), username)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			<-hashSlots
			log.Print("login user lookup failed")
			writeAuthError(w, http.StatusInternalServerError, "could not sign in")
			return
		}
		matches := verifyPassword(input.Password, user.PasswordHash)
		<-hashSlots
		if err != nil || !matches {
			writeAuthError(w, http.StatusUnauthorized, "invalid username or password")
			return
		}
		createLoginSession(w, r, store, user.ID, cookieName, secureCookie)
	}
}

func createLoginSession(w http.ResponseWriter, r *http.Request, store loginStore, userID pgtype.UUID, cookieName string, secureCookie bool) {
	// Always create a fresh token. Incoming cookies cannot choose or promote a session.
	var randomToken [sessionTokenSize]byte
	if _, err := rand.Read(randomToken[:]); err != nil {
		log.Print("login session token generation failed")
		writeAuthError(w, http.StatusInternalServerError, "could not sign in")
		return
	}
	token := base64.RawURLEncoding.EncodeToString(randomToken[:])
	digest := sha256.Sum256([]byte(token))
	expiresAt := time.Now().UTC().Truncate(time.Second).Add(sessionLifetime)
	if err := store.CreateSession(r.Context(), db.CreateSessionParams{
		UserID: userID, TokenHash: digest[:], ExpiresAt: pgtype.Timestamptz{Time: expiresAt, Valid: true},
	}); err != nil {
		log.Print("login session insert failed")
		writeAuthError(w, http.StatusInternalServerError, "could not sign in")
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name: cookieName, Value: token, Path: "/", HttpOnly: true, Secure: secureCookie,
		SameSite: http.SameSiteLaxMode, Expires: expiresAt, MaxAge: int(sessionLifetime / time.Second),
	})
	w.WriteHeader(http.StatusNoContent)
}
