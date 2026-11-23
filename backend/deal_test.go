package main

import (
	"context"
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

const dealTestTeamID = "2012b9d0-c227-4689-a172-1aa7aafe35ea"
const dealTestContactID = "3c440689-d64f-4ee3-8bce-a828107f1e50"
const validDealJSON = `{"title":"  Test deal  ","contact_id":"` + dealTestContactID + `","amount":"123.4567","currency":" usd "}`

type dealCreatorStub struct {
	companyCreatorFunc
	create func(context.Context, pgtype.UUID, db.CreateDealParams) (db.CreateDealRow, error)
}

func (s dealCreatorStub) CreateTeamDeal(ctx context.Context, userID pgtype.UUID, params db.CreateDealParams) (db.CreateDealRow, error) {
	return s.create(ctx, userID, params)
}

func TestCreateDealUsesSessionAndPath(t *testing.T) {
	store := dealCreatorStub{create: func(_ context.Context, userID pgtype.UUID, params db.CreateDealParams) (db.CreateDealRow, error) {
		amount, err := params.Amount.Value()
		if err != nil || amount != "123.4567" || params.Currency != "USD" || params.Stage != "new" ||
			params.Title != "Test deal" || params.ContactID.String() != dealTestContactID ||
			userID.String() != companyTestUserID || params.CompanyID.String() != contactCompanyID || params.TeamID.String() != dealTestTeamID ||
			params.ExpectedCloseDate.Valid || params.LostReason.Valid {
			t.Fatal("deal did not use normalized input and the session actor")
		}
		return db.CreateDealRow{ID: userID, CompanyID: params.CompanyID, TeamID: params.TeamID, ContactID: params.ContactID,
			Title: params.Title, Amount: "123.4567", Currency: params.Currency, Stage: params.Stage}, nil
	}}
	response := postDeal(routes(nil, contactTestSession(t), store, true), contactCompanyID, dealTestTeamID, validDealJSON, companyTestCookie(), companyTestUserID)
	var result dealResponse
	if response.Code != http.StatusCreated || json.Unmarshal(response.Body.Bytes(), &result) != nil ||
		result.Amount != "123.4567" || result.CompanyID.String() != contactCompanyID || result.TeamID.String() != dealTestTeamID ||
		result.ContactID.String() != dealTestContactID || result.AssigneeUserID.Valid || response.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("unexpected deal response, status %d", response.Code)
	}
}

func TestCreateDealRejectsInvalidRequests(t *testing.T) {
	store := dealCreatorStub{create: func(context.Context, pgtype.UUID, db.CreateDealParams) (db.CreateDealRow, error) {
		t.Fatal("invalid request reached the deal store")
		return db.CreateDealRow{}, nil
	}}
	handler := routes(nil, contactTestSession(t), store, true)
	assertUnauthorizedSession(t, postDeal(handler, contactCompanyID, dealTestTeamID, validDealJSON, nil, companyTestUserID))
	assertCompanyPreconditionFailed(t, postDeal(handler, contactCompanyID, dealTestTeamID, validDealJSON, companyTestCookie(), ""))
	assertCompanyPreconditionFailed(t, postDeal(handler, contactCompanyID, dealTestTeamID, validDealJSON, companyTestCookie(), dealTestTeamID))
	for name, body := range map[string]string{
		"blank title":       strings.Replace(validDealJSON, "Test deal", "", 1),
		"bad contact":       strings.Replace(validDealJSON, dealTestContactID, "invalid", 1),
		"negative amount":   strings.Replace(validDealJSON, "123.4567", "-1", 1),
		"excess precision":  strings.Replace(validDealJSON, "123.4567", "0.00001", 1),
		"excess magnitude":  strings.Replace(validDealJSON, "123.4567", "1000000000000000", 1),
		"amount as number":  strings.Replace(validDealJSON, `"123.4567"`, "123.4567", 1),
		"missing amount":    strings.Replace(validDealJSON, `"amount":"123.4567",`, "", 1),
		"nan":               strings.Replace(validDealJSON, "123.4567", "NaN", 1),
		"bad currency":      strings.Replace(validDealJSON, "usd", "us1", 1),
		"bad stage":         strings.TrimSuffix(validDealJSON, "}") + `,"stage":"draft"}`,
		"bad date":          strings.TrimSuffix(validDealJSON, "}") + `,"expected_close_date":"2026-02-30"}`,
		"year zero":         strings.TrimSuffix(validDealJSON, "}") + `,"expected_close_date":"0000-01-01"}`,
		"unexpected reason": strings.TrimSuffix(validDealJSON, "}") + `,"lost_reason":"reason"}`,
		"unexpected owner":  strings.TrimSuffix(validDealJSON, "}") + `,"assignee_user_id":null}`,
	} {
		t.Run(name, func(t *testing.T) {
			if response := postDeal(handler, contactCompanyID, dealTestTeamID, body, companyTestCookie(), companyTestUserID); response.Code != http.StatusBadRequest {
				t.Fatalf("expected 400, got %d", response.Code)
			}
		})
	}
	if response := postDeal(handler, contactCompanyID, "bad-team-id", validDealJSON, companyTestCookie(), companyTestUserID); response.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid team ID, got %d", response.Code)
	}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/companies/"+contactCompanyID+"/teams/"+dealTestTeamID+"/deals", strings.NewReader(validDealJSON))
	request.Header.Set("Origin", "https://unrelated.example")
	request.AddCookie(companyTestCookie())
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for cross-origin write, got %d", response.Code)
	}
}

func TestCreateDealMapsStoreErrors(t *testing.T) {
	for _, test := range []struct {
		err    error
		status int
		body   string
	}{
		{pgx.ErrNoRows, 404, `{"error":"team or contact not found"}`},
		{errors.New("private database failure"), 500, `{"error":"could not create deal"}`},
	} {
		store := dealCreatorStub{create: func(context.Context, pgtype.UUID, db.CreateDealParams) (db.CreateDealRow, error) {
			return db.CreateDealRow{}, test.err
		}}
		response := postDeal(routes(nil, contactTestSession(t), store, true), contactCompanyID, dealTestTeamID, validDealJSON, companyTestCookie(), companyTestUserID)
		if response.Code != test.status || response.Body.String() != test.body+"\n" {
			t.Fatalf("expected safe status %d, got %d", test.status, response.Code)
		}
	}
}

type dealTeamMembershipRemovedTx struct {
	companyTxStub
	queries []string
}

func (tx *dealTeamMembershipRemovedTx) Begin(context.Context) (pgx.Tx, error) { return tx, nil }

func (tx *dealTeamMembershipRemovedTx) QueryRow(_ context.Context, sql string, _ ...any) pgx.Row {
	query, _, _ := strings.Cut(sql, "\n")
	tx.queries = append(tx.queries, query)
	if query == "-- name: LockDealTeamMembership :one" {
		return missingMembershipRow{}
	}
	return companyFixtureRow{}
}

func TestCreateDealChecksMembershipAfterTakingCompanyAndTeamLocks(t *testing.T) {
	tx := &dealTeamMembershipRemovedTx{}
	_, err := (postgresCompanyStore{database: tx}).CreateTeamDeal(t.Context(), pgtype.UUID{Valid: true}, db.CreateDealParams{})
	want := []string{
		"-- name: LockActiveCompanyForMember :one",
		"-- name: LockActiveDealTeam :one",
		"-- name: LockCompanyMembership :one",
		"-- name: LockDealTeamMembership :one",
	}
	if !errors.Is(err, pgx.ErrNoRows) || tx.committed || !tx.rolledBack || !slices.Equal(tx.queries, want) {
		t.Fatalf("membership denial must roll back before insertion, queries %q, error %v", tx.queries, err)
	}
}

func postDeal(handler http.Handler, companyID, teamID, body string, cookie *http.Cookie, expectedID string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodPost, "/api/v1/companies/"+companyID+"/teams/"+teamID+"/deals", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
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
