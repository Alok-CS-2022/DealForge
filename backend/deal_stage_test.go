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
	"github.com/jackc/pgx/v5/pgtype"
)

const dealStageTestID = "62933f65-c4c7-4113-9a44-d66af1907b05"

type dealStageUpdaterStub struct {
	companyCreatorFunc
	update func(context.Context, pgtype.UUID, db.UpdateDealStageParams) (db.UpdateDealStageRow, error)
}

func (s dealStageUpdaterStub) UpdateTeamDealStage(ctx context.Context, userID pgtype.UUID, params db.UpdateDealStageParams) (db.UpdateDealStageRow, error) {
	return s.update(ctx, userID, params)
}

func TestUpdateDealStageUsesSessionAndPath(t *testing.T) {
	store := dealStageUpdaterStub{update: func(_ context.Context, userID pgtype.UUID, params db.UpdateDealStageParams) (db.UpdateDealStageRow, error) {
		if userID.String() != companyTestUserID || params.CompanyID.String() != contactCompanyID ||
			params.TeamID.String() != dealTestTeamID || params.DealID.String() != dealStageTestID ||
			params.Stage != "lost" || params.LostReason != (pgtype.Text{String: "Customer declined", Valid: true}) {
			t.Fatalf("unexpected actor or update params: %s, %+v", userID, params)
		}
		return db.UpdateDealStageRow{ID: params.DealID, CompanyID: params.CompanyID, TeamID: params.TeamID,
			Amount: "999999999999999.9999", Currency: "USD", Stage: params.Stage, LostReason: params.LostReason}, nil
	}}
	handler := routes(nil, contactTestSession(t), store, true)
	response := patchDealStage(handler, contactCompanyID, dealTestTeamID, dealStageTestID,
		`{"stage":" Lost ","lost_reason":" Customer declined "}`, companyTestCookie(), companyTestUserID)
	var result dealResponse
	if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &result) != nil ||
		result.ID.String() != dealStageTestID || result.CompanyID.String() != contactCompanyID || result.TeamID.String() != dealTestTeamID ||
		result.Amount != "999999999999999.9999" || result.Stage != "lost" || result.LostReason.String != "Customer declined" ||
		response.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("unexpected update response: %d %s", response.Code, response.Body)
	}
}

func TestUpdateDealStageRejectsInvalidRequests(t *testing.T) {
	store := dealStageUpdaterStub{update: func(context.Context, pgtype.UUID, db.UpdateDealStageParams) (db.UpdateDealStageRow, error) {
		t.Fatal("invalid request reached the deal store")
		return db.UpdateDealStageRow{}, nil
	}}
	handler := routes(nil, contactTestSession(t), store, true)
	validBody := `{"stage":"proposal"}`
	send := func(body string, cookie *http.Cookie, expectedID string) *httptest.ResponseRecorder {
		return patchDealStage(handler, contactCompanyID, dealTestTeamID, dealStageTestID, body, cookie, expectedID)
	}
	assertUnauthorizedSession(t, send(validBody, nil, companyTestUserID))
	assertCompanyPreconditionFailed(t, send(validBody, companyTestCookie(), ""))
	assertCompanyPreconditionFailed(t, send(validBody, companyTestCookie(), dealStageTestID))
	for _, body := range []string{
		`{}`, `{"stage":null}`, `{"stage":"unknown"}`, `{"stage":1}`, `{"stage":"won","lost_reason":"A reason"}`,
		`{"stage":"lost","lost_reason":" "}`, `{"stage":"lost","lost_reason":"\u0000"}`,
		`{"stage":"lost","lost_reason":"` + strings.Repeat("a", 501) + `"}`,
		`{"stage":"new","amount":"1.0000"}`, `{"stage":"won"}{}`,
	} {
		if response := send(body, companyTestCookie(), companyTestUserID); response.Code != 400 {
			t.Errorf("invalid body %q returned %d, want 400", body, response.Code)
		}
	}
	for _, ids := range [][3]string{
		{"invalid", dealTestTeamID, dealStageTestID},
		{contactCompanyID, "invalid", dealStageTestID},
		{contactCompanyID, dealTestTeamID, "invalid"},
	} {
		if response := patchDealStage(handler, ids[0], ids[1], ids[2], validBody, companyTestCookie(), companyTestUserID); response.Code != 400 {
			t.Errorf("invalid path returned %d, want 400", response.Code)
		}
	}
	request := httptest.NewRequest(http.MethodPatch, dealStagePath(contactCompanyID, dealTestTeamID, dealStageTestID), strings.NewReader(validBody))
	request.Header.Set("Origin", "https://unrelated.example")
	request.AddCookie(companyTestCookie())
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("cross-origin request returned %d, want 403", response.Code)
	}
}

func TestUpdateDealStageMapsStoreErrors(t *testing.T) {
	for _, tc := range []struct {
		err    error
		status int
		body   string
	}{
		{pgx.ErrNoRows, 404, `{"error":"deal not found"}`},
		{errors.New("private database failure"), 500, `{"error":"could not update deal stage"}`},
	} {
		store := dealStageUpdaterStub{update: func(context.Context, pgtype.UUID, db.UpdateDealStageParams) (db.UpdateDealStageRow, error) {
			return db.UpdateDealStageRow{}, tc.err
		}}
		response := patchDealStage(routes(nil, contactTestSession(t), store, true), contactCompanyID, dealTestTeamID, dealStageTestID,
			`{"stage":"qualified"}`, companyTestCookie(), companyTestUserID)
		if response.Code != tc.status || response.Body.String() != tc.body+"\n" {
			t.Errorf("expected safe status %d, got %d %s", tc.status, response.Code, response.Body)
		}
	}
}

func dealStagePath(companyID, teamID, dealID string) string {
	return "/api/v1/companies/" + companyID + "/teams/" + teamID + "/deals/" + dealID + "/stage"
}

func patchDealStage(handler http.Handler, companyID, teamID, dealID, body string, cookie *http.Cookie, expectedID string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodPatch, dealStagePath(companyID, teamID, dealID), strings.NewReader(body))
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
