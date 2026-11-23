package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Alok-CS-2022/DealForge/backend/internal/db"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

const validTaskJSON = `{"title":"  Follow up  ","due_date":"2000-02-29"}`

type taskCreatorStub struct {
	companyCreatorFunc
	create func(context.Context, pgtype.UUID, db.CreateTaskParams) (db.DealforgeTask, error)
}

func (s taskCreatorStub) CreateDealTask(ctx context.Context, userID pgtype.UUID, params db.CreateTaskParams) (db.DealforgeTask, error) {
	return s.create(ctx, userID, params)
}

func TestCreateTaskUsesSessionAndScope(t *testing.T) {
	createdAt := pgtype.Timestamptz{Time: time.Date(2026, 11, 23, 1, 2, 3, 456000, time.UTC), Valid: true}
	store := taskCreatorStub{create: func(_ context.Context, userID pgtype.UUID, params db.CreateTaskParams) (db.DealforgeTask, error) {
		if userID.String() != companyTestUserID || params.CompanyID.String() != contactCompanyID ||
			params.TeamID.String() != dealTestTeamID || params.DealID.String() != dealStageTestID ||
			params.Title != "Follow up" || !params.DueDate.Valid || params.DueDate.Time.Format(time.DateOnly) != "2000-02-29" {
			t.Fatal("task creation did not use the validated session, scope and input")
		}
		return db.DealforgeTask{
			ID: userID, TeamID: params.TeamID, DealID: params.DealID, Title: params.Title, DueDate: params.DueDate,
			Status: "pending", CreatedAt: createdAt, UpdatedAt: createdAt,
		}, nil
	}}
	handler := routes(nil, contactTestSession(t), store, true)
	request := taskRequest(contactCompanyID, dealTestTeamID, dealStageTestID, validTaskJSON, companyTestCookie(), companyTestUserID)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	want := `{"id":"` + companyTestUserID + `","deal_id":"` + dealStageTestID + `","team_id":"` + dealTestTeamID +
		`","title":"Follow up","due_date":"2000-02-29","status":"pending","completed_at":null,"assignee_user_id":null,` +
		`"created_at":"2026-11-23T01:02:03.000456Z","updated_at":"2026-11-23T01:02:03.000456Z"}` + "\n"
	if response.Code != http.StatusCreated || response.Body.String() != want || response.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("unexpected creation response: %d %s", response.Code, response.Body)
	}
}

func TestCreateTaskRejectsInvalidRequests(t *testing.T) {
	store := taskCreatorStub{create: func(context.Context, pgtype.UUID, db.CreateTaskParams) (db.DealforgeTask, error) {
		t.Fatal("invalid request reached the task store")
		return db.DealforgeTask{}, nil
	}}
	handler := routes(nil, contactTestSession(t), store, true)
	send := func(body string, cookie *http.Cookie, expectedUsers ...string) *httptest.ResponseRecorder {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, taskRequest(contactCompanyID, dealTestTeamID, dealStageTestID, body, cookie, expectedUsers...))
		return response
	}
	assertUnauthorizedSession(t, send(validTaskJSON, nil, companyTestUserID))
	assertCompanyPreconditionFailed(t, send(validTaskJSON, companyTestCookie()))
	assertCompanyPreconditionFailed(t, send(validTaskJSON, companyTestCookie(), dealTestTeamID))
	assertCompanyPreconditionFailed(t, send(validTaskJSON, companyTestCookie(), companyTestUserID, companyTestUserID))
	for name, body := range map[string]string{
		"missing title":     `{"due_date":"2000-02-29"}`,
		"blank title":       `{"title":"  ","due_date":"2000-02-29"}`,
		"long title":        `{"title":"` + strings.Repeat("é", 101) + `","due_date":"2000-02-29"}`,
		"null character":    `{"title":"Follow\u0000up","due_date":"2000-02-29"}`,
		"missing date":      `{"title":"Follow up"}`,
		"null date":         `{"title":"Follow up","due_date":null}`,
		"non-leap day":      strings.Replace(validTaskJSON, "2000-02-29", "1900-02-29", 1),
		"not padded":        strings.Replace(validTaskJSON, "2000-02-29", "2000-2-29", 1),
		"year zero":         strings.Replace(validTaskJSON, "2000-02-29", "0000-01-01", 1),
		"date with time":    strings.Replace(validTaskJSON, "2000-02-29", "2000-02-29T00:00:00Z", 1),
		"supplied status":   strings.TrimSuffix(validTaskJSON, "}") + `,"status":"completed"}`,
		"supplied assignee": strings.TrimSuffix(validTaskJSON, "}") + `,"assignee_user_id":null}`,
		"second JSON value": validTaskJSON + "{}",
	} {
		t.Run(name, func(t *testing.T) {
			if response := send(body, companyTestCookie(), companyTestUserID); response.Code != http.StatusBadRequest {
				t.Errorf("invalid task returned %d, want 400", response.Code)
			}
		})
	}
	for _, scope := range [][3]string{
		{"bad-id", dealTestTeamID, dealStageTestID},
		{contactCompanyID, "bad-id", dealStageTestID},
		{contactCompanyID, dealTestTeamID, "bad-id"},
	} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, taskRequest(scope[0], scope[1], scope[2], validTaskJSON, companyTestCookie(), companyTestUserID))
		if response.Code != http.StatusBadRequest {
			t.Errorf("malformed scope returned %d, want 400", response.Code)
		}
	}
	crossOrigin := taskRequest(contactCompanyID, dealTestTeamID, dealStageTestID, validTaskJSON, companyTestCookie(), companyTestUserID)
	crossOrigin.Header.Set("Origin", "https://unrelated.example")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, crossOrigin)
	if response.Code != http.StatusForbidden {
		t.Fatalf("cross-origin task creation returned %d, want 403", response.Code)
	}
}

func TestCreateTaskMapsStoreErrors(t *testing.T) {
	for _, tc := range []struct {
		err    error
		status int
		body   string
	}{
		{pgx.ErrNoRows, 404, `{"error":"deal not found"}`},
		{errors.New("private database failure"), 500, `{"error":"could not create task"}`},
	} {
		store := taskCreatorStub{create: func(context.Context, pgtype.UUID, db.CreateTaskParams) (db.DealforgeTask, error) {
			return db.DealforgeTask{}, tc.err
		}}
		response := httptest.NewRecorder()
		routes(nil, contactTestSession(t), store, true).ServeHTTP(response,
			taskRequest(contactCompanyID, dealTestTeamID, dealStageTestID, validTaskJSON, companyTestCookie(), companyTestUserID))
		if response.Code != tc.status || response.Body.String() != tc.body+"\n" {
			t.Errorf("expected safe status %d, got %d %s", tc.status, response.Code, response.Body)
		}
	}
}

func taskRequest(companyID, teamID, dealID, body string, cookie *http.Cookie, expectedUsers ...string) *http.Request {
	path := "/api/v1/companies/" + companyID + "/teams/" + teamID + "/deals/" + dealID + "/tasks"
	request := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	for _, id := range expectedUsers {
		request.Header.Add(expectedUserHeader, id)
	}
	if cookie != nil {
		request.AddCookie(cookie)
	}
	return request
}

func createdTask(t *testing.T, response *httptest.ResponseRecorder) taskResponse {
	t.Helper()
	var task taskResponse
	if response.Code != http.StatusCreated || json.Unmarshal(response.Body.Bytes(), &task) != nil {
		t.Fatalf("unexpected task response: %d %s", response.Code, response.Body)
	}
	return task
}
