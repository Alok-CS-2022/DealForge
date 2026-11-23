package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Alok-CS-2022/DealForge/backend/internal/db"
	"github.com/jackc/pgx/v5/pgtype"
)

type taskListerStub struct {
	companyCreatorFunc
	list func(context.Context, db.ListDealTasksParams) ([]db.ListDealTasksRow, error)
}

func (s taskListerStub) ListDealTasks(ctx context.Context, params db.ListDealTasksParams) ([]db.ListDealTasksRow, error) {
	return s.list(ctx, params)
}

func TestListTasksResponses(t *testing.T) {
	companyID, _ := parseResourceID(contactCompanyID)
	teamID, _ := parseResourceID(dealTestTeamID)
	dealID, _ := parseResourceID(dealStageTestID)
	taskID, _ := parseResourceID(dealTestContactID)
	dueDate := pgtype.Date{Time: time.Date(2000, 2, 29, 0, 0, 0, 0, time.UTC), Valid: true}
	createdAt := pgtype.Timestamptz{Time: time.Date(2031, 2, 3, 4, 5, 6, 0, time.UTC), Valid: true}
	row := db.ListDealTasksRow{
		ID: taskID, DealID: dealID, TeamID: teamID, Title: "Follow up", DueDate: dueDate,
		Status: "pending", CreatedAt: createdAt, UpdatedAt: createdAt,
	}
	for _, tc := range []struct {
		name   string
		rows   []db.ListDealTasksRow
		err    error
		status int
		body   string
	}{
		{"listed", []db.ListDealTasksRow{row}, nil, http.StatusOK,
			`[{"id":"` + dealTestContactID + `","deal_id":"` + dealStageTestID + `","team_id":"` + dealTestTeamID +
				`","title":"Follow up","due_date":"2000-02-29","status":"pending","completed_at":null,"assignee_user_id":null,` +
				`"created_at":"2031-02-03T04:05:06Z","updated_at":"2031-02-03T04:05:06Z"}]`},
		{"empty", []db.ListDealTasksRow{{DealID: dealID, TeamID: teamID}}, nil, http.StatusOK, `[]`},
		{"unavailable", nil, nil, http.StatusNotFound, `{"error":"deal not found"}`},
		{"database error", nil, errors.New("private database details"), http.StatusInternalServerError, `{"error":"could not list tasks"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := taskListerStub{list: func(_ context.Context, params db.ListDealTasksParams) ([]db.ListDealTasksRow, error) {
				if params.CompanyID != companyID || params.TeamID != teamID || params.DealID != dealID ||
					params.UserID.String() != companyTestUserID {
					t.Fatal("task listing did not use the path scope and session user")
				}
				return tc.rows, tc.err
			}}
			handler := routes(nil, contactTestSession(t), store, true)
			response := getTasks(handler, contactCompanyID, dealTestTeamID, dealStageTestID, companyTestCookie(), companyTestUserID)
			if response.Code != tc.status || response.Body.String() != tc.body+"\n" ||
				response.Header().Get("Cache-Control") != "no-store" || response.Header().Get("Content-Type") != "application/json" {
				t.Fatalf("unexpected task list response: %d %s", response.Code, response.Body)
			}
		})
	}
}

func TestListTasksRejectsInvalidRequests(t *testing.T) {
	store := taskListerStub{list: func(context.Context, db.ListDealTasksParams) ([]db.ListDealTasksRow, error) {
		t.Fatal("invalid request reached the task store")
		return nil, nil
	}}
	handler := routes(nil, contactTestSession(t), store, true)
	assertUnauthorizedSession(t, getTasks(handler, contactCompanyID, dealTestTeamID, dealStageTestID, nil, companyTestUserID))
	assertCompanyPreconditionFailed(t, getTasks(handler, contactCompanyID, dealTestTeamID, dealStageTestID, companyTestCookie(), dealTestTeamID))
	for _, ids := range [][3]string{
		{"invalid", dealTestTeamID, dealStageTestID},
		{contactCompanyID, "invalid", dealStageTestID},
		{contactCompanyID, dealTestTeamID, "invalid"},
	} {
		response := getTasks(handler, ids[0], ids[1], ids[2], companyTestCookie(), "")
		if response.Code != http.StatusBadRequest || response.Body.String() != "{\"error\":\"invalid company, team or deal ID\"}\n" {
			t.Errorf("malformed UUID returned %d, want 400", response.Code)
		}
	}
}

func getTasks(handler http.Handler, companyID, teamID, dealID string, cookie *http.Cookie, expectedID string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodGet, "/api/v1/companies/"+companyID+"/teams/"+teamID+"/deals/"+dealID+"/tasks", nil)
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
