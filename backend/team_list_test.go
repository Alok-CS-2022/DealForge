package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Alok-CS-2022/DealForge/backend/internal/db"
)

type teamListerStub struct {
	companyCreatorFunc
	list func(context.Context, db.ListCompanyTeamsParams) ([]db.ListCompanyTeamsRow, error)
}

func (s teamListerStub) ListCompanyTeams(ctx context.Context, params db.ListCompanyTeamsParams) ([]db.ListCompanyTeamsRow, error) {
	return s.list(ctx, params)
}

func TestListTeamsResponses(t *testing.T) {
	companyID, _ := parseResourceID(contactCompanyID)
	firstID, _ := parseResourceID(dealTestTeamID)
	secondID, _ := parseResourceID(dealTestContactID)
	first := db.ListCompanyTeamsRow{ID: firstID, CompanyID: companyID, Name: "Alpha", Role: "salesperson"}
	second := db.ListCompanyTeamsRow{ID: secondID, CompanyID: companyID, Name: "Zulu", Role: "manager"}
	firstJSON := `{"id":"` + dealTestTeamID + `","company_id":"` + contactCompanyID + `","name":"Alpha","role":"salesperson"}`
	secondJSON := `{"id":"` + dealTestContactID + `","company_id":"` + contactCompanyID + `","name":"Zulu","role":"manager"}`
	for _, tc := range []struct {
		name   string
		rows   []db.ListCompanyTeamsRow
		err    error
		status int
		body   string
	}{
		{"zero teams", []db.ListCompanyTeamsRow{{CompanyID: companyID}}, nil, http.StatusOK, `[]`},
		{"one team", []db.ListCompanyTeamsRow{first}, nil, http.StatusOK, "[" + firstJSON + "]"},
		{"multiple teams", []db.ListCompanyTeamsRow{first, second}, nil, http.StatusOK, "[" + firstJSON + "," + secondJSON + "]"},
		{"unavailable", nil, nil, http.StatusNotFound, `{"error":"company not found"}`},
		{"database error", nil, errors.New("private database details"), http.StatusInternalServerError, `{"error":"could not list teams"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := teamListerStub{list: func(_ context.Context, params db.ListCompanyTeamsParams) ([]db.ListCompanyTeamsRow, error) {
				if params.CompanyID != companyID || params.UserID.String() != companyTestUserID {
					t.Fatal("team listing did not use the company path and session identity")
				}
				return tc.rows, tc.err
			}}
			handler := routes(nil, contactTestSession(t), store, true)
			response := getTeams(handler, contactCompanyID, companyTestCookie(), companyTestUserID)
			if response.Code != tc.status || response.Body.String() != tc.body+"\n" ||
				response.Header().Get("Content-Type") != "application/json" || response.Header().Get("Cache-Control") != "no-store" {
				t.Fatalf("unexpected team listing, status %d, body %s", response.Code, response.Body.String())
			}
		})
	}
}

func TestListTeamsRejectsInvalidRequests(t *testing.T) {
	store := teamListerStub{list: func(context.Context, db.ListCompanyTeamsParams) ([]db.ListCompanyTeamsRow, error) {
		t.Fatal("invalid request reached the team store")
		return nil, nil
	}}
	handler := routes(nil, contactTestSession(t), store, true)
	assertUnauthorizedSession(t, getTeams(handler, contactCompanyID, nil, companyTestUserID))
	assertCompanyPreconditionFailed(t, getTeams(handler, contactCompanyID, companyTestCookie(), dealTestContactID))
	response := getTeams(handler, "invalid", companyTestCookie(), "")
	if response.Code != http.StatusBadRequest || response.Body.String() != "{\"error\":\"invalid company ID\"}\n" {
		t.Fatalf("malformed ID returned %d, want 400", response.Code)
	}
}

func getTeams(handler http.Handler, companyID string, cookie *http.Cookie, expectedID string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodGet, "/api/v1/companies/"+companyID+"/teams", nil)
	if expectedID != "" {
		request.Header.Set(expectedUserHeader, expectedID)
	}
	if cookie != nil {
		request.AddCookie(cookie)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}
