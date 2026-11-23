package main

import (
	"context"
	"crypto/rand"
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

func TestCreateTaskInPostgres(t *testing.T) {
	ctx, tx := companyFixtureTransaction(t)
	queries := db.New(tx)
	actor := createCompanyTestUser(t, ctx, queries, "it_task_"+rand.Text())
	manager := createCompanyTestUser(t, ctx, queries, "it_task_manager_"+rand.Text())
	actorCookie := createCompanyTestSession(t, ctx, queries, actor)
	managerCookie := createCompanyTestSession(t, ctx, queries, manager)
	target := createStageDealFixture(t, ctx, tx, actor, "salesperson")
	foreign := createStageDealFixture(t, ctx, tx, actor, "manager")
	archivedCompany := createStageDealFixture(t, ctx, tx, actor, "manager")
	archivedTeam := createStageDealFixture(t, ctx, tx, actor, "manager")
	adminOnly := createStageDealFixture(t, ctx, tx, actor, "")
	otherTeam := createStageDealTeam(t, ctx, tx, target.companyID, actor, "manager", "Other team")
	if _, err := tx.Exec(ctx, `INSERT INTO dealforge.company_memberships (company_id, user_id, role)
		VALUES ($1, $2, 'member')`, target.companyID, manager); err != nil {
		t.Fatal(err)
	}
	if err := queries.AddTeamManager(ctx, db.AddTeamManagerParams{CompanyID: target.companyID, TeamID: target.teamID, UserID: manager}); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, "UPDATE dealforge.companies SET archived_at = now() WHERE id = $1", archivedCompany.companyID); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, "UPDATE dealforge.teams SET archived_at = now() WHERE id = $1", archivedTeam.teamID); err != nil {
		t.Fatal(err)
	}
	handler := routes(queries, queries, postgresCompanyStore{database: tx}, true)
	send := func(handler http.Handler, scope stageDealFixture, cookie *http.Cookie, userID pgtype.UUID, body string) *httptest.ResponseRecorder {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, taskRequest(scope.companyID.String(), scope.teamID.String(), scope.dealID.String(), body, cookie, userID.String()).WithContext(ctx))
		return response
	}

	assertUnauthorizedSession(t, send(handler, target, nil, actor, validTaskJSON))
	assertCompanyPreconditionFailed(t, send(handler, target, actorCookie, manager, validTaskJSON))
	badDate := strings.Replace(validTaskJSON, "2000-02-29", "2025-02-29", 1)
	if response := send(handler, target, actorCookie, actor, badDate); response.Code != http.StatusBadRequest {
		t.Fatalf("invalid date returned %d, want 400", response.Code)
	}
	for _, tc := range []struct {
		name   string
		scope  stageDealFixture
		cookie *http.Cookie
		userID pgtype.UUID
	}{
		{"deal in another team", stageDealFixture{target.companyID, otherTeam, target.dealID}, actorCookie, actor},
		{"deal in another company", stageDealFixture{target.companyID, target.teamID, foreign.dealID}, actorCookie, actor},
		{"team in another company", stageDealFixture{target.companyID, foreign.teamID, foreign.dealID}, actorCookie, actor},
		{"archived company", archivedCompany, actorCookie, actor},
		{"archived team", archivedTeam, actorCookie, actor},
		{"company admin outside team", adminOnly, actorCookie, actor},
		{"outsider", foreign, managerCookie, manager},
		{"missing deal", stageDealFixture{target.companyID, target.teamID, pgtype.UUID{Valid: true}}, actorCookie, actor},
		{"missing team", stageDealFixture{target.companyID, pgtype.UUID{Valid: true}, target.dealID}, actorCookie, actor},
		{"missing company", stageDealFixture{pgtype.UUID{Valid: true}, target.teamID, target.dealID}, actorCookie, actor},
	} {
		t.Run(tc.name, func(t *testing.T) {
			response := send(handler, tc.scope, tc.cookie, tc.userID, validTaskJSON)
			if response.Code != http.StatusNotFound || response.Body.String() != "{\"error\":\"deal not found\"}\n" {
				t.Fatalf("expected uniform 404, got %d %s", response.Code, response.Body)
			}
		})
	}
	checkCounts := func(want int) {
		t.Helper()
		companies := []pgtype.UUID{target.companyID, foreign.companyID, archivedTeam.companyID, archivedCompany.companyID, adminOnly.companyID}
		var tasks, logs int
		err := tx.QueryRow(ctx, `
			SELECT (SELECT count(*) FROM dealforge.tasks t JOIN dealforge.deals d ON d.id = t.deal_id
				WHERE d.company_id = ANY($1::uuid[])),
			       (SELECT count(*) FROM dealforge.activity_logs WHERE company_id = ANY($1::uuid[]))
		`, companies).Scan(&tasks, &logs)
		if err != nil || tasks != want || logs != want {
			t.Fatalf("expected %d fixture tasks and logs, got %d tasks, %d logs (%v)", want, tasks, logs, err)
		}
	}
	checkCounts(0)

	t.Run("audit insert failure rolls back the task", func(t *testing.T) {
		failure := &taskAuditFailure{Tx: tx}
		if err := tx.QueryRow(ctx, "SELECT gen_random_uuid()").Scan(&failure.invalidActor); err != nil {
			t.Fatal(err)
		}
		failingHandler := routes(queries, queries, postgresCompanyStore{database: failure}, true)
		response := send(failingHandler, target, actorCookie, actor, validTaskJSON)
		if response.Code != http.StatusInternalServerError || response.Body.String() != "{\"error\":\"could not create task\"}\n" ||
			failure.sqlState != "23503" || !failure.taskID.Valid {
			t.Fatalf("expected a generic 500 from a real audit FK violation, got %d (SQLSTATE %q)", response.Code, failure.sqlState)
		}
		checkCounts(0)
	})

	for index, stage := range []string{"new", "qualified", "proposal", "won", "lost"} {
		if _, err := tx.Exec(ctx, "UPDATE dealforge.deals SET stage = $1 WHERE id = $2", stage, target.dealID); err != nil {
			t.Fatal(err)
		}
		userID, cookie := actor, actorCookie
		if index%2 == 1 {
			userID, cookie = manager, managerCookie
		}
		response := send(handler, target, cookie, userID, validTaskJSON)
		task := createdTask(t, response)
		assertCreatedTaskAndLog(t, ctx, tx, task, target, userID)
		checkCounts(index + 1)
	}
}

func assertCreatedTaskAndLog(t *testing.T, ctx context.Context, tx pgx.Tx, task taskResponse, scope stageDealFixture, userID pgtype.UUID) {
	t.Helper()
	var stored db.DealforgeTask
	err := tx.QueryRow(ctx, `
		SELECT id, deal_id, team_id, title, due_date, status, completed_at, assignee_user_id, created_at, updated_at
		FROM dealforge.tasks WHERE id = $1
	`, task.ID).Scan(&stored.ID, &stored.DealID, &stored.TeamID, &stored.Title, &stored.DueDate, &stored.Status,
		&stored.CompletedAt, &stored.AssigneeUserID, &stored.CreatedAt, &stored.UpdatedAt)
	if err != nil || !task.ID.Valid || task.DealID != scope.dealID || task.TeamID != scope.teamID ||
		task.Title != "Follow up" || !task.DueDate.Valid || task.DueDate.Time.Format("2006-01-02") != "2000-02-29" ||
		task.Status != "pending" || task.CompletedAt.Valid || task.AssigneeUserID.Valid ||
		!task.CreatedAt.Valid || !task.UpdatedAt.Valid {
		t.Fatalf("unexpected task response or persisted row: %+v (%v)", task, err)
	}
	storedJSON, err := json.Marshal(taskResponse(stored))
	if err != nil {
		t.Fatal(err)
	}
	responseJSON, err := json.Marshal(task)
	if err != nil || string(responseJSON) != string(storedJSON) {
		t.Fatalf("response did not match the saved task: %s != %s (%v)", responseJSON, storedJSON, err)
	}
	var logCompanyID, logActorID pgtype.UUID
	var action, entityType string
	var loggedAt pgtype.Timestamptz
	var changesMatch bool
	err = tx.QueryRow(ctx, `
		SELECT company_id, actor_user_id, action, entity_type, created_at,
		       changes = jsonb_build_object('team_id', $2::uuid, 'deal_id', $3::uuid,
		                                   'title', $4::text, 'due_date', $5::date, 'status', $6::text)
		FROM dealforge.activity_logs WHERE entity_id = $1
	`, task.ID, stored.TeamID, stored.DealID, stored.Title, stored.DueDate, stored.Status).
		Scan(&logCompanyID, &logActorID, &action, &entityType, &loggedAt, &changesMatch)
	if err != nil || logCompanyID != scope.companyID || logActorID != userID ||
		action != "created" || entityType != "task" || !loggedAt.Valid || !changesMatch {
		t.Fatalf("activity log did not match the created task and session actor (%v)", err)
	}
}

// Cause a real foreign-key violation at the audit write after PostgreSQL has inserted the task.
type taskAuditFailure struct {
	pgx.Tx
	invalidActor pgtype.UUID
	taskID       pgtype.UUID
	sqlState     string
}

func (f *taskAuditFailure) Begin(ctx context.Context) (pgx.Tx, error) {
	tx, err := f.Tx.Begin(ctx)
	if err != nil {
		return nil, err
	}
	return &taskAuditFailureTx{Tx: tx, failure: f}, nil
}

type taskAuditFailureTx struct {
	pgx.Tx
	failure *taskAuditFailure
}

func (tx *taskAuditFailureTx) Exec(ctx context.Context, query string, args ...any) (pgconn.CommandTag, error) {
	if !strings.HasPrefix(query, "-- name: LogTaskCreation :exec\n") {
		return tx.Tx.Exec(ctx, query, args...)
	}
	tx.failure.taskID = args[2].(pgtype.UUID)
	args[1] = tx.failure.invalidActor
	tag, err := tx.Tx.Exec(ctx, query, args...)
	var pgError *pgconn.PgError
	if errors.As(err, &pgError) {
		tx.failure.sqlState = pgError.Code
	}
	return tag, err
}
