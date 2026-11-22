package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Alok-CS-2022/DealForge/backend/internal/db"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
)

const contactCompanyID = "d8cd82a9-2fa5-477a-b713-8762a8d7b27d"
const validContactJSON = `{"name":"Ada Lovelace","email":"ada@example.test"}`

type contactCreatorFunc struct {
	companyCreatorFunc
	create func(context.Context, pgtype.UUID, db.CreateContactParams) (db.DealforgeContact, error)
}

func (f contactCreatorFunc) CreateCompanyContact(ctx context.Context, user pgtype.UUID, params db.CreateContactParams) (db.DealforgeContact, error) {
	return f.create(ctx, user, params)
}

func contactTestSession(t *testing.T) *sessionStoreStub {
	t.Helper()
	var userID pgtype.UUID
	if err := userID.Scan(companyTestUserID); err != nil {
		t.Fatal(err)
	}
	return &sessionStoreStub{lookup: func([]byte) (db.GetActiveSessionUserRow, error) {
		return db.GetActiveSessionUserRow{ID: userID}, nil
	}}
}

func TestCreateContactNormalizesInputAndUsesSessionActor(t *testing.T) {
	store := contactCreatorFunc{create: func(_ context.Context, user pgtype.UUID, params db.CreateContactParams) (db.DealforgeContact, error) {
		if user.String() != companyTestUserID || params.CompanyID.String() != contactCompanyID ||
			params.Name != "Ada Lovelace" || params.Email != "ada@example.test" ||
			params.Phone != (pgtype.Text{Valid: true, String: "+44 20 1234"}) || params.CompanyName.Valid {
			t.Fatal("unexpected contact parameters")
		}
		return db.DealforgeContact{
			ID: user, CompanyID: params.CompanyID, Name: params.Name, Email: params.Email, Phone: params.Phone,
		}, nil
	}}
	handler := routes(nil, contactTestSession(t), store, true)
	response := postContact(handler, contactCompanyID,
		`{"name":" Ada Lovelace ","email":" ADA@EXAMPLE.TEST ","phone":" +44 20 1234 ","company_name":" "}`, companyTestCookie())
	var result map[string]any
	if response.Code != http.StatusCreated || json.Unmarshal(response.Body.Bytes(), &result) != nil ||
		response.Header().Get("Content-Type") != "application/json" || response.Header().Get("Cache-Control") != "no-store" ||
		len(result) != 9 || result["company_id"] != contactCompanyID || result["email"] != "ada@example.test" ||
		result["phone"] != "+44 20 1234" || result["company_name"] != nil || result["assignee_user_id"] != nil {
		t.Fatalf("unexpected contact response, status %d", response.Code)
	}
}

func TestCreateContactRejectsInvalidRequests(t *testing.T) {
	store := contactCreatorFunc{create: func(context.Context, pgtype.UUID, db.CreateContactParams) (db.DealforgeContact, error) {
		t.Fatal("request should never reach the contact store")
		return db.DealforgeContact{}, nil
	}}
	handler := routes(nil, contactTestSession(t), store, true)
	assertUnauthorizedSession(t, postContact(handler, contactCompanyID, validContactJSON, nil))
	for name, body := range map[string]string{
		"blank name":    `{"name":"  ","email":"ada@example.test"}`,
		"invalid email": `{"name":"Ada","email":"Ada <ada@example.test>"}`,
		"long name":     `{"name":"` + strings.Repeat("é", 101) + `","email":"ada@example.test"}`,
		"phone nul":     `{"name":"Ada","email":"ada@example.test","phone":"1\u0000"}`,
		"long company":  `{"name":"Ada","email":"ada@example.test","company_name":"` + strings.Repeat("a", 101) + `"}`,
		"assignee":      `{"name":"Ada","email":"ada@example.test","assignee_user_id":null}`,
		"tenant field":  `{"name":"Ada","email":"ada@example.test","company_id":"` + contactCompanyID + `"}`,
		"trailing JSON": validContactJSON + `{}`,
	} {
		t.Run(name, func(t *testing.T) {
			if response := postContact(handler, contactCompanyID, body, companyTestCookie()); response.Code != http.StatusBadRequest {
				t.Fatalf("expected 400, got %d", response.Code)
			}
		})
	}
	if response := postContact(handler, "not-a-uuid", validContactJSON, companyTestCookie()); response.Code != http.StatusBadRequest {
		t.Fatalf("invalid company ID returned %d, want 400", response.Code)
	}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/companies/"+contactCompanyID+"/contacts", strings.NewReader(validContactJSON))
	request.AddCookie(companyTestCookie())
	request.Header.Set("Origin", "https://unrelated.example")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("cross-origin request returned %d, want 403", response.Code)
	}
	request = httptest.NewRequest(http.MethodPost, "/api/v1/companies/"+contactCompanyID+"/contacts", strings.NewReader(validContactJSON))
	request.AddCookie(companyTestCookie())
	request.Header.Set(expectedUserHeader, contactCompanyID)
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	assertCompanyPreconditionFailed(t, response)
}

func TestCreateContactMapsStoreErrors(t *testing.T) {
	for _, test := range []struct {
		name    string
		err     error
		status  int
		message string
	}{
		{"unavailable company", errContactCompanyUnavailable, 404, "company not found"},
		{"duplicate email", &pgconn.PgError{Code: "23505", ConstraintName: "contacts_company_email_key"}, 409, "contact email already exists in this company"},
		{"unrelated constraint", &pgconn.PgError{Code: "23505", ConstraintName: "other_constraint"}, 500, "could not create contact"},
		{"database failure", errors.New("private details"), 500, "could not create contact"},
	} {
		t.Run(test.name, func(t *testing.T) {
			store := contactCreatorFunc{create: func(context.Context, pgtype.UUID, db.CreateContactParams) (db.DealforgeContact, error) {
				return db.DealforgeContact{}, test.err
			}}
			response := postContact(routes(nil, contactTestSession(t), store, true), contactCompanyID, validContactJSON, companyTestCookie())
			if response.Code != test.status || response.Body.String() != `{"error":"`+test.message+`"}`+"\n" {
				t.Fatalf("expected safe %d, got %d", test.status, response.Code)
			}
		})
	}
}

type contactMembershipChangedTx struct {
	companyTxStub
	queries []string
}

func (tx *contactMembershipChangedTx) Begin(context.Context) (pgx.Tx, error) { return tx, nil }

func (tx *contactMembershipChangedTx) QueryRow(_ context.Context, query string, _ ...any) pgx.Row {
	tx.queries = append(tx.queries, query)
	if strings.Contains(query, "FOR SHARE") {
		return missingContactMembership{}
	}
	return companyFixtureRow{}
}

type missingContactMembership struct{}

func (missingContactMembership) Scan(...any) error { return pgx.ErrNoRows }

func TestCreateContactRechecksMembershipAfterLockingCompany(t *testing.T) {
	tx := &contactMembershipChangedTx{}
	store := postgresCompanyStore{database: tx}
	contact, err := store.CreateCompanyContact(t.Context(), pgtype.UUID{Valid: true}, db.CreateContactParams{})
	if !errors.Is(err, errContactCompanyUnavailable) || contact != (db.DealforgeContact{}) || tx.committed || !tx.rolledBack ||
		len(tx.queries) != 2 || !strings.Contains(tx.queries[0], "FOR UPDATE") || !strings.Contains(tx.queries[1], "FOR SHARE") {
		t.Fatal("contact creation must recheck membership after locking the company and roll back on denial")
	}
}

func postContact(handler http.Handler, companyID, body string, cookie *http.Cookie) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodPost, "/api/v1/companies/"+companyID+"/contacts", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	if cookie != nil {
		request.AddCookie(cookie)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}
