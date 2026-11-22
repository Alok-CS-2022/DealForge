package main

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"log"
	"net/http"

	"github.com/Alok-CS-2022/DealForge/backend/internal/db"
	"github.com/jackc/pgx/v5"
)

const sessionTokenSize = 32

type sessionReader interface {
	GetActiveSessionUser(context.Context, []byte) (db.GetActiveSessionUserRow, error)
}

type authSessionStore interface {
	loginStore
	sessionReader
}

func sessionCookieName(secure bool) string {
	if secure {
		return "__Host-dealforge_session"
	}
	return "dealforge_session"
}

func requireSession(store sessionReader, secureCookie bool, next func(http.ResponseWriter, *http.Request, publicUser)) http.HandlerFunc {
	cookieName := sessionCookieName(secureCookie)
	encoding := base64.RawURLEncoding.Strict()
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		cookies := r.CookiesNamed(cookieName)
		if len(cookies) != 1 || len(cookies[0].Value) != encoding.EncodedLen(sessionTokenSize) {
			writeAuthError(w, http.StatusUnauthorized, "invalid or missing session")
			return
		}
		token := cookies[0].Value
		var decoded [sessionTokenSize]byte
		if n, err := encoding.Decode(decoded[:], []byte(token)); err != nil || n != len(decoded) {
			writeAuthError(w, http.StatusUnauthorized, "invalid or missing session")
			return
		}
		if store == nil {
			writeAuthError(w, http.StatusInternalServerError, "could not validate session")
			return
		}

		// Login hashes the encoded cookie value, so validation hashes those same bytes.
		digest := sha256.Sum256([]byte(token))
		user, err := store.GetActiveSessionUser(r.Context(), digest[:])
		if errors.Is(err, pgx.ErrNoRows) {
			writeAuthError(w, http.StatusUnauthorized, "invalid or missing session")
			return
		}
		if err != nil {
			log.Print("session lookup failed")
			writeAuthError(w, http.StatusInternalServerError, "could not validate session")
			return
		}
		next(w, r, publicUser{
			ID: user.ID.String(), Username: user.Username, Email: user.Email, DisplayName: user.DisplayName,
		})
	}
}

func currentUser(w http.ResponseWriter, _ *http.Request, user publicUser) {
	writeAuthJSON(w, http.StatusOK, user)
}
