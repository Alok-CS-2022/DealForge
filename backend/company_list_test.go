package main

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/Alok-CS-2022/DealForge/backend/internal/db"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

type companyListStoreStub struct {
	companyCreatorFunc
	list func(context.Context, pgtype.UUID) ([]db.ListCompaniesForUserRow, error)
}

func (s companyListStoreStub) ListCompaniesForUser(ctx context.Context, userID pgtype.UUID) ([]db.ListCompaniesForUserRow, error) {
	return s.list(ctx, userID)
}

func TestListCompaniesUsesSessionIdentity(t *testing.T) {
	var userID pgtype.UUID
	if err := userID.Scan("befaf392-110c-4db5-b582-f8e76660823d"); err != nil {
		t.Fatal(err)
	}
	var firstID, secondID pgtype.UUID
	if err := firstID.Scan("d8cd82a9-2fa5-477a-b713-8762a8d7b27d"); err != nil {
		t.Fatal(err)
	}
	if err := secondID.Scan("2b855f87-7956-40ae-847a-278c88bb7b4c"); err != nil {
		t.Fatal(err)
	}
	first := db.ListCompaniesForUserRow{ID: firstID, Name: "Alpha", Role: "member"}
	second := db.ListCompaniesForUserRow{ID: secondID, Name: "Zulu", Role: "admin"}
	for _, test := range []struct {
		name string
		rows []db.ListCompaniesForUserRow
		want []companyMembership
	}{
		{name: "none"},
		{name: "one", rows: []db.ListCompaniesForUserRow{first}, want: []companyMembership{{ID: firstID.String(), Name: "Alpha", Role: "member"}}},
		{name: "multiple", rows: []db.ListCompaniesForUserRow{first, second}, want: []companyMembership{
			{ID: firstID.String(), Name: "Alpha", Role: "member"},
			{ID: secondID.String(), Name: "Zulu", Role: "admin"},
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			session := &sessionStoreStub{lookup: func([]byte) (db.GetActiveSessionUserRow, error) {
				return db.GetActiveSessionUserRow{ID: userID}, nil
			}}
			store := companyListStoreStub{list: func(_ context.Context, id pgtype.UUID) ([]db.ListCompaniesForUserRow, error) {
				if id != userID {
					t.Fatal("list did not use the session's user ID")
				}
				return test.rows, nil
			}}
			handler := routes(nil, session, store, true)
			assertCompanyList(t, getCompanies(handler, companyTestCookie(), "?user_id="+firstID.String()), test.want)
		})
	}
}

func TestListCompaniesAuthenticationAndDatabaseErrors(t *testing.T) {
	missing := &sessionStoreStub{lookup: func([]byte) (db.GetActiveSessionUserRow, error) {
		return db.GetActiveSessionUserRow{}, pgx.ErrNoRows
	}}
	handler := routes(nil, missing, nil, true)
	assertUnauthorizedSession(t, getCompanies(handler, nil, ""))
	assertUnauthorizedSession(t, getCompanies(handler, companyTestCookie(), ""))

	session := &sessionStoreStub{lookup: func([]byte) (db.GetActiveSessionUserRow, error) {
		return db.GetActiveSessionUserRow{ID: pgtype.UUID{Valid: true}}, nil
	}}
	store := companyListStoreStub{list: func(context.Context, pgtype.UUID) ([]db.ListCompaniesForUserRow, error) {
		return nil, errors.New("private database details")
	}}
	response := getCompanies(routes(nil, session, store, true), companyTestCookie(), "")
	if response.Code != http.StatusInternalServerError || response.Body.String() != "{\"error\":\"could not list companies\"}\n" {
		t.Fatalf("expected generic failure, got %d", response.Code)
	}
}

func TestListCompaniesInPostgres(t *testing.T) {
	ctx, tx := companyFixtureTransaction(t)
	queries := db.New(tx)
	tag := strings.ToLower(rand.Text())[:18]
	userID := createCompanyTestUser(t, ctx, queries, "it_list_"+tag)
	otherID := createCompanyTestUser(t, ctx, queries, "it_other_"+tag)
	cookie := createCompanyTestSession(t, ctx, queries, userID)
	otherCookie := createCompanyTestSession(t, ctx, queries, otherID)
	handler := routes(queries, queries, postgresCompanyStore{database: tx}, true)

	addCompany := func(name string, member pgtype.UUID, role string, archived bool) companyMembership {
		t.Helper()
		company, err := queries.CreateCompany(ctx, name)
		if err != nil {
			t.Fatal("could not create company list fixture")
		}
		if _, err := tx.Exec(ctx, `INSERT INTO dealforge.company_memberships (company_id,user_id,role) VALUES ($1,$2,$3)`,
			company.ID, member, role); err != nil {
			t.Fatal("could not assign fixture membership")
		}
		if archived {
			if _, err := tx.Exec(ctx, `UPDATE dealforge.companies SET archived_at = now() WHERE id = $1`, company.ID); err != nil {
				t.Fatal("could not archive fixture company")
			}
		}
		return companyMembership{ID: company.ID.String(), Name: name, Role: role}
	}

	assertUnauthorizedSession(t, getCompanies(handler, nil, ""))
	assertCompanyList(t, getCompanies(handler, cookie, ""), nil) // No memberships.
	other := addCompany("Other "+tag, otherID, "admin", false)
	addCompany("Archived "+tag, userID, "admin", true)
	assertCompanyList(t, getCompanies(handler, cookie, "?user_id="+otherID.String()), nil)
	assertCompanyList(t, getCompanies(handler, otherCookie, ""), []companyMembership{other})

	zulu := addCompany("Zulu "+tag, userID, "admin", false)
	assertCompanyList(t, getCompanies(handler, cookie, ""), []companyMembership{zulu})
	alphaOne := addCompany("Alpha "+tag, userID, "member", false)
	alphaTwo := addCompany("Alpha "+tag, userID, "admin", false)
	if alphaTwo.ID < alphaOne.ID {
		alphaOne, alphaTwo = alphaTwo, alphaOne
	}
	assertCompanyList(t, getCompanies(handler, cookie, ""), []companyMembership{alphaOne, alphaTwo, zulu})
}

func getCompanies(handler http.Handler, cookie *http.Cookie, query string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/companies"+query, nil)
	if cookie != nil {
		req.AddCookie(cookie)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, req)
	return response
}

func assertCompanyList(t *testing.T, response *httptest.ResponseRecorder, want []companyMembership) {
	t.Helper()
	if response.Code != http.StatusOK || response.Header().Get("Content-Type") != "application/json" ||
		response.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("unexpected list response, status %d", response.Code)
	}
	var got []companyMembership
	if err := json.Unmarshal(response.Body.Bytes(), &got); err != nil || got == nil || !slices.Equal(got, want) {
		t.Fatal("company list did not match the expected non-null array")
	}
	var fields []map[string]json.RawMessage
	if err := json.Unmarshal(response.Body.Bytes(), &fields); err != nil {
		t.Fatal("company list was not valid JSON")
	}
	for _, company := range fields {
		if len(company) != 3 || company["id"] == nil || company["name"] == nil || company["role"] == nil {
			t.Fatal("company list exposed fields outside the documented contract")
		}
	}
}
