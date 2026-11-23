package main

import (
	"crypto/rand"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/Alok-CS-2022/DealForge/backend/internal/db"
	"github.com/jackc/pgx/v5/pgtype"
)

func TestListTasksInPostgres(t *testing.T) {
	ctx, tx := companyFixtureTransaction(t)
	queries := db.New(tx)
	seller := createCompanyTestUser(t, ctx, queries, "it_list_task_"+rand.Text())
	manager := createCompanyTestUser(t, ctx, queries, "it_list_task_manager_"+rand.Text())
	sellerCookie := createCompanyTestSession(t, ctx, queries, seller)
	managerCookie := createCompanyTestSession(t, ctx, queries, manager)
	handler := routes(queries, queries, postgresCompanyStore{database: tx}, true)

	target := createStageDealFixture(t, ctx, tx, seller, "salesperson")
	foreign := createStageDealFixture(t, ctx, tx, manager, "manager")
	archivedCompany := createStageDealFixture(t, ctx, tx, seller, "manager")
	archivedTeam := createStageDealFixture(t, ctx, tx, seller, "manager")
	restrictedTeam := createStageDealTeam(t, ctx, tx, target.companyID, manager, "", "Restricted")
	if _, err := tx.Exec(ctx, `INSERT INTO dealforge.company_memberships (company_id, user_id, role)
		VALUES ($1, $2, 'member')`, target.companyID, manager); err != nil {
		t.Fatal(err)
	}
	for _, teamID := range []pgtype.UUID{target.teamID, restrictedTeam} {
		if err := queries.AddTeamManager(ctx, db.AddTeamManagerParams{CompanyID: target.companyID, TeamID: teamID, UserID: manager}); err != nil {
			t.Fatal(err)
		}
	}
	addDeal := func(teamID pgtype.UUID, name string) stageDealFixture {
		t.Helper()
		scope := stageDealFixture{companyID: target.companyID, teamID: teamID}
		err := tx.QueryRow(ctx, `
			INSERT INTO dealforge.deals (company_id, team_id, contact_id, title, amount, currency)
			SELECT company_id, $1, contact_id, $2, amount, currency FROM dealforge.deals WHERE id = $3
			RETURNING id
		`, teamID, name, target.dealID).Scan(&scope.dealID)
		if err != nil {
			t.Fatal(err)
		}
		return scope
	}
	empty := addDeal(target.teamID, "Empty deal")
	sibling := addDeal(target.teamID, "Other deal in the same team")
	restricted := addDeal(restrictedTeam, "Deal in another team")
	if _, err := tx.Exec(ctx, "UPDATE dealforge.companies SET archived_at = now() WHERE id = $1", archivedCompany.companyID); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, "UPDATE dealforge.teams SET archived_at = now() WHERE id = $1", archivedTeam.teamID); err != nil {
		t.Fatal(err)
	}

	addTask := func(scope stageDealFixture, number, day, hour int, assignee pgtype.UUID) taskResponse {
		t.Helper()
		taskID := target.dealID
		taskID.Bytes[15] = byte(number)
		task := taskResponse{
			ID: taskID, DealID: scope.dealID, TeamID: scope.teamID, Title: "Fixture " + strconv.Itoa(number),
			DueDate: pgtype.Date{Time: time.Date(2030, 4, day, 0, 0, 0, 0, time.UTC), Valid: true},
			Status:  "pending", AssigneeUserID: assignee,
			CreatedAt: pgtype.Timestamptz{Time: time.Date(2030, 3, 4, hour, 0, 0, 0, time.UTC), Valid: true},
		}
		task.UpdatedAt = task.CreatedAt
		if assignee.Valid {
			task.Status = "completed"
			task.CompletedAt = pgtype.Timestamptz{Time: task.CreatedAt.Time.Add(time.Hour), Valid: true}
			task.UpdatedAt = task.CompletedAt
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO dealforge.tasks (id, deal_id, team_id, title, due_date, status, completed_at,
			                            assignee_user_id, created_at, updated_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		`, task.ID, task.DealID, task.TeamID, task.Title, task.DueDate, task.Status, task.CompletedAt,
			task.AssigneeUserID, task.CreatedAt, task.UpdatedAt); err != nil {
			t.Fatal(err)
		}
		return task
	}
	// Insert out of order and vary each sort key independently.
	last := addTask(target, 1, 11, 8, pgtype.UUID{})
	third := addTask(target, 4, 10, 10, manager)
	second := addTask(target, 3, 10, 10, pgtype.UUID{})
	first := addTask(target, 5, 10, 9, pgtype.UUID{})
	siblingTask := addTask(sibling, 6, 9, 8, pgtype.UUID{})
	restrictedTask := addTask(restricted, 7, 9, 8, pgtype.UUID{})
	addTask(foreign, 8, 9, 8, pgtype.UUID{})
	addTask(archivedTeam, 9, 9, 8, pgtype.UUID{})
	addTask(archivedCompany, 0, 9, 8, pgtype.UUID{})

	get := func(scope stageDealFixture, cookie *http.Cookie, expectedID string) *httptest.ResponseRecorder {
		return getTasks(handler, scope.companyID.String(), scope.teamID.String(), scope.dealID.String(), cookie, expectedID)
	}
	assertUnauthorizedSession(t, get(target, nil, seller.String()))
	assertCompanyPreconditionFailed(t, get(target, sellerCookie, manager.String()))
	if response := getTasks(handler, target.companyID.String(), target.teamID.String(), "invalid", sellerCookie, ""); response.Code != 400 {
		t.Fatalf("invalid deal ID returned %d, want 400", response.Code)
	}
	if response := get(empty, sellerCookie, ""); response.Code != http.StatusOK || response.Body.String() != "[]\n" {
		t.Fatalf("expected an empty array, got %d %s", response.Code, response.Body)
	}

	missing := pgtype.UUID{Valid: true}
	for _, tc := range []struct {
		name  string
		scope stageDealFixture
	}{
		{"company admin outside the team", restricted},
		{"another deal under the wrong authorized team", stageDealFixture{target.companyID, target.teamID, restricted.dealID}},
		{"foreign company", foreign},
		{"foreign team under the wrong company", stageDealFixture{target.companyID, foreign.teamID, foreign.dealID}},
		{"foreign deal under an authorized team", stageDealFixture{target.companyID, target.teamID, foreign.dealID}},
		{"archived team", archivedTeam},
		{"archived company", archivedCompany},
		{"missing company", stageDealFixture{missing, target.teamID, target.dealID}},
		{"missing team", stageDealFixture{target.companyID, missing, target.dealID}},
		{"missing deal", stageDealFixture{target.companyID, target.teamID, missing}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			response := get(tc.scope, sellerCookie, "")
			if response.Code != http.StatusNotFound || response.Body.String() != "{\"error\":\"deal not found\"}\n" {
				t.Fatalf("expected a uniform 404, got %d %s", response.Code, response.Body)
			}
		})
	}
	path := "/api/v1/companies/" + restricted.companyID.String() + "/teams/" + restricted.teamID.String() +
		"/deals/" + restricted.dealID.String() + "/tasks?user_id=" + manager.String()
	spoofed := httptest.NewRequest(http.MethodGet, path, nil)
	spoofed.AddCookie(sellerCookie)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, spoofed)
	if response.Code != http.StatusNotFound {
		t.Fatalf("request-supplied user ID changed access, got %d", response.Code)
	}

	assertTasks := func(scope stageDealFixture, cookie *http.Cookie, want ...taskResponse) {
		t.Helper()
		response := get(scope, cookie, "")
		var tasks []taskResponse
		if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &tasks) != nil || len(tasks) != len(want) {
			t.Fatalf("unexpected task data or order: %d %s", response.Code, response.Body)
		}
		for i, task := range tasks {
			task.DueDate.Time = task.DueDate.Time.UTC()
			task.CreatedAt.Time = task.CreatedAt.Time.UTC()
			task.UpdatedAt.Time = task.UpdatedAt.Time.UTC()
			task.CompletedAt.Time = task.CompletedAt.Time.UTC()
			if task != want[i] {
				t.Fatalf("unexpected task at index %d: %+v", i, task)
			}
		}
	}
	assertTasks(target, sellerCookie, first, second, third, last)
	assertTasks(target, managerCookie, first, second, third, last)
	assertTasks(sibling, sellerCookie, siblingTask)
	assertTasks(restricted, managerCookie, restrictedTask)
}
