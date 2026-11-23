package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Alok-CS-2022/DealForge/backend/internal/db"
)

type dealListerStub struct {
	companyCreatorFunc
	list func(context.Context, db.ListTeamDealsParams) ([]db.ListTeamDealsRow, error)
}

func (s dealListerStub) ListTeamDeals(ctx context.Context, params db.ListTeamDealsParams) ([]db.ListTeamDealsRow, error) {
	return s.list(ctx, params)
}

func TestListDealsResponses(t *testing.T) {
	companyID, _ := parseResourceID(contactCompanyID)
	teamID, _ := parseResourceID(dealTestTeamID)
	contactID, _ := parseResourceID(dealTestContactID)
	dealID, _ := parseResourceID(companyTestUserID)
	row := db.ListTeamDealsRow{ID: dealID, CompanyID: companyID, TeamID: teamID, ContactID: contactID,
		Title: "Test deal", Amount: "999999999999999.9999", Currency: "USD", Stage: "new"}
	for _, tc := range []struct {
		name   string
		rows   []db.ListTeamDealsRow
		err    error
		status int
		body   string
	}{
		{"listed", []db.ListTeamDealsRow{row}, nil, http.StatusOK,
			`[{"id":"` + companyTestUserID + `","company_id":"` + contactCompanyID + `","team_id":"` + dealTestTeamID +
				`","contact_id":"` + dealTestContactID + `","title":"Test deal","amount":"999999999999999.9999","currency":"USD","stage":"new","expected_close_date":null,"lost_reason":null,"assignee_user_id":null,"created_at":null,"updated_at":null}]`},
		{"empty", []db.ListTeamDealsRow{{CompanyID: companyID, TeamID: teamID}}, nil, http.StatusOK, `[]`},
		{"unavailable", nil, nil, http.StatusNotFound, `{"error":"team not found"}`},
		{"database error", nil, errors.New("private details"), http.StatusInternalServerError, `{"error":"could not list deals"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := dealListerStub{list: func(_ context.Context, params db.ListTeamDealsParams) ([]db.ListTeamDealsRow, error) {
				if params.CompanyID != companyID || params.TeamID != teamID || params.UserID.String() != companyTestUserID {
					t.Fatal("deal listing did not use the scoped path and session actor")
				}
				return tc.rows, tc.err
			}}
			handler := routes(nil, contactTestSession(t), store, true)
			response := getDeals(handler, contactCompanyID, dealTestTeamID, companyTestCookie(), companyTestUserID)
			if response.Code != tc.status || response.Body.String() != tc.body+"\n" ||
				response.Header().Get("Cache-Control") != "no-store" || response.Header().Get("Content-Type") != "application/json" {
				t.Fatalf("unexpected response, status %d, body %s", response.Code, response.Body.String())
			}
		})
	}
}

func TestListDealsRejectsInvalidRequests(t *testing.T) {
	store := dealListerStub{list: func(context.Context, db.ListTeamDealsParams) ([]db.ListTeamDealsRow, error) {
		t.Fatal("invalid request reached the deal store")
		return nil, nil
	}}
	handler := routes(nil, contactTestSession(t), store, true)
	assertUnauthorizedSession(t, getDeals(handler, contactCompanyID, dealTestTeamID, nil, companyTestUserID))
	assertCompanyPreconditionFailed(t, getDeals(handler, contactCompanyID, dealTestTeamID, companyTestCookie(), dealTestContactID))
	for _, ids := range [][2]string{{"bad-company", dealTestTeamID}, {contactCompanyID, "bad-team"}} {
		response := getDeals(handler, ids[0], ids[1], companyTestCookie(), "")
		if response.Code != http.StatusBadRequest || response.Body.String() != "{\"error\":\"invalid company or team ID\"}\n" {
			t.Fatalf("malformed UUID returned %d, want 400", response.Code)
		}
	}
}

func getDeals(handler http.Handler, companyID, teamID string, cookie *http.Cookie, expectedID string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodGet, "/api/v1/companies/"+companyID+"/teams/"+teamID+"/deals", nil)
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
