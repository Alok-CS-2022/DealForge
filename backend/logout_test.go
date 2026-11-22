package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"
)

type revocationStoreStub struct {
	loginStoreStub
	revoked [][]byte
	err     error
}

func (s *revocationStoreStub) RevokeSession(_ context.Context, hash []byte) error {
	s.revoked = append(s.revoked, bytes.Clone(hash))
	return s.err
}

func TestLogoutRevokesCookieAndClearsIt(t *testing.T) {
	for _, test := range []struct {
		name, origin, cookieName string
		secure                   bool
	}{
		{"production", "https://dealforge.example", "__Host-dealforge_session", true},
		{"development", "http://127.0.0.1:8080", "dealforge_session", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			store := &revocationStoreStub{}
			handler := routes(nil, store, test.secure)
			cookie := &http.Cookie{
				Name: test.cookieName, Value: sessionFixtureToken, Path: "/",
				HttpOnly: true, Secure: test.secure, SameSite: http.SameSiteLaxMode,
			}
			request := httptest.NewRequest(http.MethodPost, test.origin+"/api/v1/auth/logout", nil)
			request.Header.Set("Origin", test.origin)
			request.AddCookie(cookie)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			deletion := assertLogoutClearsCookie(t, response, cookie)
			want := sha256.Sum256([]byte(cookie.Value))
			if len(store.revoked) != 1 || !bytes.Equal(store.revoked[0], want[:]) {
				t.Fatal("logout did not revoke exactly the supplied cookie digest")
			}
			target, err := url.Parse(test.origin + "/api/v1/auth/me")
			if err != nil {
				t.Fatal(err)
			}
			jar, err := cookiejar.New(nil)
			if err != nil {
				t.Fatal(err)
			}
			jar.SetCookies(target, []*http.Cookie{cookie})
			if len(jar.Cookies(target)) != 1 {
				t.Fatal("fixture cookie was not stored")
			}
			jar.SetCookies(target, []*http.Cookie{deletion})
			if len(jar.Cookies(target)) != 0 {
				t.Fatal("logout cookie did not remove the existing browser cookie")
			}
		})
	}
}

func TestLogoutIgnoresUnusableCookies(t *testing.T) {
	current := &http.Cookie{Name: "__Host-dealforge_session", Path: "/", HttpOnly: true, Secure: true, SameSite: http.SameSiteLaxMode}
	store := &revocationStoreStub{}
	handler := routes(nil, store, true)
	token := &http.Cookie{Name: current.Name, Value: sessionFixtureToken}
	for _, cookies := range [][]*http.Cookie{
		nil,
		{{Name: current.Name, Value: "malformed"}},
		{{Name: "dealforge_session", Value: sessionFixtureToken}},
		{token, token},
	} {
		assertLogoutClearsCookie(t, postLogout(handler, cookies...), current)
	}
	if len(store.revoked) != 0 {
		t.Fatal("logout tried to revoke a missing, malformed, or ambiguous cookie")
	}
}

func TestLogoutRejectsCrossOriginRequests(t *testing.T) {
	for _, headers := range []map[string]string{
		{"Sec-Fetch-Site": "cross-site"},
		{"Origin": "https://foreign.example"},
	} {
		store := &revocationStoreStub{}
		request := httptest.NewRequest(http.MethodPost, "https://dealforge.example/api/v1/auth/logout", nil)
		request.AddCookie(&http.Cookie{Name: "__Host-dealforge_session", Value: sessionFixtureToken})
		for name, value := range headers {
			request.Header.Set(name, value)
		}
		response := httptest.NewRecorder()
		routes(nil, store, true).ServeHTTP(response, request)
		if response.Code != http.StatusForbidden || len(store.revoked) != 0 || response.Header().Get("Set-Cookie") != "" {
			t.Fatal("cross-origin logout was allowed to change session state")
		}
	}
}

func TestLogoutDatabaseFailureDoesNotClearCookie(t *testing.T) {
	store := &revocationStoreStub{err: errors.New("private database details")}
	response := postLogout(routes(nil, store, true), &http.Cookie{
		Name: "__Host-dealforge_session", Value: sessionFixtureToken,
	})
	if response.Code != http.StatusInternalServerError ||
		response.Body.String() != "{\"error\":\"could not sign out\"}\n" ||
		response.Header().Get("Set-Cookie") != "" {
		t.Fatal("database failure did not return a safe, retryable response")
	}
}

func postLogout(handler http.Handler, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodPost, "/api/v1/auth/logout", nil)
	for _, cookie := range cookies {
		request.AddCookie(cookie)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func assertLogoutClearsCookie(t *testing.T, response *httptest.ResponseRecorder, current *http.Cookie) *http.Cookie {
	t.Helper()
	cookies := response.Result().Cookies()
	if response.Code != http.StatusNoContent || response.Body.Len() != 0 ||
		len(cookies) != 1 || response.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("expected an empty 204 and one clearing cookie, got status %d", response.Code)
	}
	cleared := cookies[0]
	if cleared.Name != current.Name || cleared.Path != current.Path || cleared.Domain != current.Domain ||
		cleared.HttpOnly != current.HttpOnly || cleared.Secure != current.Secure || cleared.SameSite != current.SameSite ||
		cleared.Value != "" || cleared.MaxAge >= 0 || !cleared.Expires.Equal(time.Unix(0, 0).UTC()) {
		t.Fatal("clearing cookie did not match the session cookie attributes")
	}
	return cleared
}
