package main

import (
	"context"
	"log"
	"net/http"
	"time"
)

type sessionRevoker interface {
	RevokeSession(context.Context, []byte) error
}

func logoutUser(store sessionRevoker, secureCookie bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		cookie := sessionCookie(secureCookie)
		if digest, ok := sessionCookieDigest(r, cookie.Name); ok {
			if store == nil {
				writeAuthError(w, http.StatusInternalServerError, "could not sign out")
				return
			}
			if err := store.RevokeSession(r.Context(), digest[:]); err != nil {
				log.Print("session revocation failed")
				writeAuthError(w, http.StatusInternalServerError, "could not sign out")
				return
			}
		}
		cookie.MaxAge = -1
		cookie.Expires = time.Unix(0, 0).UTC()
		http.SetCookie(w, &cookie)
		w.WriteHeader(http.StatusNoContent)
	}
}
