package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Alok-CS-2022/DealForge/backend/internal/db"
	"github.com/jackc/pgx/v5/pgtype"
)

type contactListerStub struct {
	companyCreatorFunc
	list func(context.Context, db.ListCompanyContactsParams) ([]db.ListCompanyContactsRow, error)
}

func (s contactListerStub) ListCompanyContacts(ctx context.Context, params db.ListCompanyContactsParams) ([]db.ListCompanyContactsRow, error) {
	return s.list(ctx, params)
}

func TestListContactsResponses(t *testing.T) {
	var id pgtype.UUID
	if err := id.Scan(contactCompanyID); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name   string
		rows   []db.ListCompanyContactsRow
		err    error
		status int
		body   string
	}{
		{"listed", []db.ListCompanyContactsRow{{ID: id, CompanyID: id, Name: "Ada", Email: "ada@example.test"}}, nil, 200,
			`[{"id":"` + contactCompanyID + `","company_id":"` + contactCompanyID + `","name":"Ada","email":"ada@example.test","phone":null,"company_name":null,"assignee_user_id":null,"created_at":null,"updated_at":null}]`},
		{"authorized empty", []db.ListCompanyContactsRow{{CompanyID: id}}, nil, 200, `[]`},
		{"unavailable", nil, nil, 404, `{"error":"company not found"}`},
		{"database failure", nil, errors.New("internal details"), 500, `{"error":"could not list contacts"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := contactListerStub{list: func(_ context.Context, params db.ListCompanyContactsParams) ([]db.ListCompanyContactsRow, error) {
				if params.CompanyID != id || params.UserID.String() != companyTestUserID {
					t.Fatal("listing did not use the company path and session user")
				}
				return tc.rows, tc.err
			}}
			handler := routes(nil, contactTestSession(t), store, true)
			response := getContacts(handler, contactCompanyID, companyTestCookie())
			if response.Code != tc.status || response.Body.String() != tc.body+"\n" ||
				response.Header().Get("Content-Type") != "application/json" || response.Header().Get("Cache-Control") != "no-store" {
				t.Fatalf("unexpected listing response, status %d, body %s", response.Code, response.Body.String())
			}
		})
	}
}

func TestListContactsRejectsInvalidRequests(t *testing.T) {
	store := contactListerStub{list: func(context.Context, db.ListCompanyContactsParams) ([]db.ListCompanyContactsRow, error) {
		t.Fatal("invalid request reached the contact store")
		return nil, nil
	}}
	handler := routes(nil, contactTestSession(t), store, true)
	assertUnauthorizedSession(t, getContacts(handler, contactCompanyID, nil))
	for _, companyID := range []string{"not-a-uuid", "d8cd82a9_2fa5_477a_b713_8762a8d7b27d"} {
		if response := getContacts(handler, companyID, companyTestCookie()); response.Code != http.StatusBadRequest {
			t.Errorf("invalid company ID returned %d, want 400", response.Code)
		}
	}
	request := httptest.NewRequest(http.MethodGet, "/api/v1/companies/"+contactCompanyID+"/contacts", nil)
	request.AddCookie(companyTestCookie())
	request.Header.Set(expectedUserHeader, contactCompanyID)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	assertCompanyPreconditionFailed(t, response)
}

func getContacts(handler http.Handler, companyID string, cookie *http.Cookie) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodGet, "/api/v1/companies/"+companyID+"/contacts", nil)
	if cookie != nil {
		request.AddCookie(cookie)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}
