package main

import (
	"context"
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

type companyCreatorFunc func(context.Context, string, pgtype.UUID) (createdCompany, error)

const companyTestUserID = "befaf392-110c-4db5-b582-f8e76660823d"

func (f companyCreatorFunc) CreateCompanyWithTeam(ctx context.Context, name string, id pgtype.UUID) (createdCompany, error) {
	return f(ctx, name, id)
}

func (f companyCreatorFunc) ListCompaniesForUser(context.Context, pgtype.UUID) ([]db.ListCompaniesForUserRow, error) {
	return nil, errors.New("unexpected company list call")
}

func (f companyCreatorFunc) CreateCompanyContact(context.Context, pgtype.UUID, db.CreateContactParams) (db.DealforgeContact, error) {
	return db.DealforgeContact{}, errors.New("unexpected contact creation call")
}

func (f companyCreatorFunc) ListCompanyContacts(context.Context, db.ListCompanyContactsParams) ([]db.ListCompanyContactsRow, error) {
	return nil, errors.New("unexpected contact listing call")
}

func TestCreateCompanyUsesAuthenticatedCreator(t *testing.T) {
	var creator pgtype.UUID
	if err := creator.Scan(companyTestUserID); err != nil {
		t.Fatal(err)
	}
	store := &sessionStoreStub{lookup: func([]byte) (db.GetActiveSessionUserRow, error) {
		return db.GetActiveSessionUserRow{ID: creator}, nil
	}}
	companies := companyCreatorFunc(func(_ context.Context, name string, id pgtype.UUID) (createdCompany, error) {
		if name != "DealForge Studio" || id != creator {
			t.Fatal("company creation did not use the normalized name and session user")
		}
		var result createdCompany
		result.Company.ID, result.Company.Name = "d8cd82a9-2fa5-477a-b713-8762a8d7b27d", name
		result.Team.ID, result.Team.Name = "2b855f87-7956-40ae-847a-278c88bb7b4c", initialTeamName
		result.Team.CompanyID = result.Company.ID
		return result, nil
	})
	response := postCompany(routes(nil, store, companies, true), `{"name":"  DealForge Studio  "}`, companyTestCookie(), creator.String())
	want := `{"company":{"id":"d8cd82a9-2fa5-477a-b713-8762a8d7b27d","name":"DealForge Studio"},"team":{"id":"2b855f87-7956-40ae-847a-278c88bb7b4c","company_id":"d8cd82a9-2fa5-477a-b713-8762a8d7b27d","name":"General"}}` + "\n"
	if response.Code != http.StatusCreated || response.Header().Get("Content-Type") != "application/json" || response.Body.String() != want {
		t.Fatalf("unexpected company response, status %d", response.Code)
	}
}

func TestCreateCompanyRejectsInvalidRequests(t *testing.T) {
	companies := companyCreatorFunc(func(context.Context, string, pgtype.UUID) (createdCompany, error) {
		t.Error("invalid company request reached the store")
		return createdCompany{}, nil
	})
	store := &sessionStoreStub{lookup: func([]byte) (db.GetActiveSessionUserRow, error) {
		return db.GetActiveSessionUserRow{ID: pgtype.UUID{Valid: true}}, nil
	}}
	handler := routes(nil, store, companies, true)
	expected := (pgtype.UUID{Valid: true}).String()
	assertUnauthorizedSession(t, postCompany(handler, `{"name":"Studio"}`, nil, expected))
	for name, body := range map[string]string{
		"missing name": `{}`,
		"blank name":   `{"name":"  "}`,
		"long name":    `{"name":"` + strings.Repeat("é", 101) + `"}`,
		"null byte":    `{"name":"foo\u0000bar"}`,
		"client owner": `{"name":"Studio","owner_id":"befaf392-110c-4db5-b582-f8e76660823d"}`,
	} {
		t.Run(name, func(t *testing.T) {
			if response := postCompany(handler, body, companyTestCookie(), expected); response.Code != http.StatusBadRequest {
				t.Errorf("invalid company returned %d, want 400", response.Code)
			}
		})
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/companies", strings.NewReader(`{"name":"Studio"}`))
	req.Header.Set("Origin", "https://unrelated.example")
	req.AddCookie(companyTestCookie())
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, req)
	if response.Code != http.StatusForbidden {
		t.Errorf("cross-origin company request returned %d, want 403", response.Code)
	}
}

func TestCreateCompanyRejectsMissingOrWrongExpectedUser(t *testing.T) {
	var creator pgtype.UUID
	if err := creator.Scan(companyTestUserID); err != nil {
		t.Fatal(err)
	}
	store := &sessionStoreStub{lookup: func([]byte) (db.GetActiveSessionUserRow, error) {
		return db.GetActiveSessionUserRow{ID: creator}, nil
	}}
	companies := companyCreatorFunc(func(context.Context, string, pgtype.UUID) (createdCompany, error) {
		t.Fatal("failed identity precondition reached the company store")
		return createdCompany{}, nil
	})
	handler := routes(nil, store, companies, true)
	for name, values := range map[string][]string{
		"missing":    nil,
		"empty":      {""},
		"malformed":  {"not-a-user-id"},
		"wrong user": {"d8cd82a9-2fa5-477a-b713-8762a8d7b27d"},
		"repeated":   {creator.String(), creator.String()},
		"combined":   {creator.String() + ", " + creator.String()},
	} {
		t.Run(name, func(t *testing.T) {
			response := postCompany(handler, `{"name":"Studio"}`, companyTestCookie(), values...)
			assertCompanyPreconditionFailed(t, response)
		})
	}
}

func assertCompanyPreconditionFailed(t *testing.T, response *httptest.ResponseRecorder) {
	t.Helper()
	if response.Code != http.StatusPreconditionFailed ||
		response.Body.String() != "{\"error\":\"session identity precondition failed\"}\n" ||
		response.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("expected a safe 412, got status %d", response.Code)
	}
}

func TestCreateCompanyHidesDatabaseErrors(t *testing.T) {
	var creator pgtype.UUID
	if err := creator.Scan(companyTestUserID); err != nil {
		t.Fatal(err)
	}
	store := &sessionStoreStub{lookup: func([]byte) (db.GetActiveSessionUserRow, error) {
		return db.GetActiveSessionUserRow{ID: creator}, nil
	}}
	companies := companyCreatorFunc(func(context.Context, string, pgtype.UUID) (createdCompany, error) {
		return createdCompany{}, errors.New("private database details")
	})
	response := postCompany(routes(nil, store, companies, true), `{"name":"Studio"}`, companyTestCookie(), creator.String())
	if response.Code != http.StatusInternalServerError || response.Body.String() != "{\"error\":\"could not create company\"}\n" {
		t.Fatalf("expected a generic 500, got status %d", response.Code)
	}
}

type companyTxStub struct {
	pgx.Tx
	inserts, execs int
	committed      bool
	rolledBack     bool
	managerError   error
}

func (tx *companyTxStub) Begin(context.Context) (pgx.Tx, error) { return tx, nil }

func (tx *companyTxStub) QueryRow(context.Context, string, ...any) pgx.Row {
	tx.inserts++
	return companyFixtureRow{}
}

func (tx *companyTxStub) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	tx.execs++
	if tx.execs == 2 {
		return pgconn.CommandTag{}, tx.managerError
	}
	return pgconn.CommandTag{}, nil
}

func (tx *companyTxStub) Commit(context.Context) error {
	tx.committed = true
	return nil
}

func (tx *companyTxStub) Rollback(context.Context) error {
	tx.rolledBack = true
	return nil
}

type companyFixtureRow struct{}

func (companyFixtureRow) Scan(dest ...any) error {
	for _, field := range dest {
		switch value := field.(type) {
		case *pgtype.UUID:
			*value = pgtype.UUID{Valid: true}
		case *string:
			*value = "Fixture"
		default:
			return errors.New("unexpected company fixture column")
		}
	}
	return nil
}

func TestCreateCompanyRollsBackWhenManagerInsertFails(t *testing.T) {
	failure := errors.New("team manager insert failed")
	tx := &companyTxStub{managerError: failure}
	store := postgresCompanyStore{database: tx}
	result, err := store.CreateCompanyWithTeam(t.Context(), "Fixture", pgtype.UUID{Valid: true})
	if !errors.Is(err, failure) || result != (createdCompany{}) ||
		tx.inserts != 2 || tx.execs != 2 || !tx.rolledBack || tx.committed {
		t.Fatal("failed final write did not roll back the company transaction")
	}
}

func postCompany(handler http.Handler, body string, cookie *http.Cookie, expectedUsers ...string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/companies", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	for _, id := range expectedUsers {
		req.Header.Add(expectedUserHeader, id)
	}
	if cookie != nil {
		req.AddCookie(cookie)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, req)
	return response
}

func companyTestCookie() *http.Cookie {
	return &http.Cookie{Name: "__Host-dealforge_session", Value: sessionFixtureToken}
}
