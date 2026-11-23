package main

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/Alok-CS-2022/DealForge/backend/internal/db"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

type stageDealFixture struct {
	companyID, teamID, dealID pgtype.UUID
}

func TestUpdateDealStageInPostgres(t *testing.T) {
	ctx, tx := companyFixtureTransaction(t)
	queries := db.New(tx)
	actor := createCompanyTestUser(t, ctx, queries, "it_stage_"+rand.Text())
	outsider := createCompanyTestUser(t, ctx, queries, "it_stage_out_"+rand.Text())
	cookie := createCompanyTestSession(t, ctx, queries, actor)
	outsiderCookie := createCompanyTestSession(t, ctx, queries, outsider)
	handler := routes(queries, queries, postgresCompanyStore{database: tx}, true)
	active := createStageDealFixture(t, ctx, tx, actor, "salesperson")
	other := createStageDealFixture(t, ctx, tx, actor, "manager")
	archivedCompany := createStageDealFixture(t, ctx, tx, actor, "manager")
	archivedTeam := createStageDealFixture(t, ctx, tx, actor, "manager")
	adminOnly := createStageDealFixture(t, ctx, tx, actor, "")
	sameCompanyTeam := createStageDealTeam(t, ctx, tx, active.companyID, actor, "manager", "Other team")
	if _, err := tx.Exec(ctx, "UPDATE dealforge.companies SET archived_at = now() WHERE id = $1", archivedCompany.companyID); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, "UPDATE dealforge.teams SET archived_at = now() WHERE id = $1", archivedTeam.teamID); err != nil {
		t.Fatal(err)
	}
	send := func(fixture stageDealFixture, body string, session *http.Cookie, expectedID pgtype.UUID) *httptest.ResponseRecorder {
		return patchDealStage(handler, fixture.companyID.String(), fixture.teamID.String(), fixture.dealID.String(), body, session, expectedID.String())
	}
	won := `{"stage":"won"}`
	assertUnauthorizedSession(t, send(active, won, nil, actor))
	assertCompanyPreconditionFailed(t, send(active, won, cookie, outsider))
	for _, body := range []string{`{"stage":"draft"}`, `{"stage":"won","lost_reason":"not allowed"}`} {
		if response := send(active, body, cookie, actor); response.Code != http.StatusBadRequest {
			t.Fatalf("invalid update returned %d, want 400", response.Code)
		}
	}
	for _, tc := range []struct {
		name    string
		fixture stageDealFixture
		cookie  *http.Cookie
		userID  pgtype.UUID
	}{
		{"deal in another team of the same company", stageDealFixture{active.companyID, sameCompanyTeam, active.dealID}, cookie, actor},
		{"deal in another company", stageDealFixture{active.companyID, active.teamID, other.dealID}, cookie, actor},
		{"team in another company", stageDealFixture{active.companyID, other.teamID, other.dealID}, cookie, actor},
		{"archived company", archivedCompany, cookie, actor},
		{"archived team", archivedTeam, cookie, actor},
		{"company admin outside team", adminOnly, cookie, actor},
		{"outsider", active, outsiderCookie, outsider},
		{"missing deal", stageDealFixture{active.companyID, active.teamID, pgtype.UUID{Valid: true}}, cookie, actor},
		{"missing team", stageDealFixture{active.companyID, pgtype.UUID{Valid: true}, active.dealID}, cookie, actor},
		{"missing company", stageDealFixture{pgtype.UUID{Valid: true}, active.teamID, active.dealID}, cookie, actor},
	} {
		t.Run(tc.name, func(t *testing.T) {
			response := send(tc.fixture, won, tc.cookie, tc.userID)
			if response.Code != http.StatusNotFound || response.Body.String() != "{\"error\":\"deal not found\"}\n" {
				t.Fatalf("expected uniform 404, got %d %s", response.Code, response.Body)
			}
		})
	}
	for _, fixture := range []stageDealFixture{active, other, archivedTeam, archivedCompany, adminOnly} {
		assertPersistedDealStage(t, ctx, tx, fixture.dealID, "new", pgtype.Text{})
	}

	reason := pgtype.Text{String: "Funding cut", Valid: true}
	replacement := pgtype.Text{String: "Timing", Valid: true}
	for _, tc := range []struct {
		body, stage string
		reason      pgtype.Text
	}{
		{won, "won", pgtype.Text{}},
		{`{"stage":"lost","lost_reason":" Funding cut "}`, "lost", reason},
		{`{"stage":"lost"}`, "lost", reason},
		{`{"stage":"lost","lost_reason":null}`, "lost", reason},
		{`{"stage":"lost","lost_reason":"Timing"}`, "lost", replacement},
		{`{"stage":"proposal"}`, "proposal", pgtype.Text{}},
		{`{"stage":"lost"}`, "lost", pgtype.Text{}},
		{`{"stage":"new"}`, "new", pgtype.Text{}},
	} {
		response := send(active, tc.body, cookie, actor)
		result := assertDealStageResponse(t, response, active, tc.stage, tc.reason)
		if !result.CreatedAt.Valid || !result.UpdatedAt.Valid || !result.UpdatedAt.Time.After(result.CreatedAt.Time) {
			t.Fatal("expected the PATCH to refresh updated_at")
		}
		assertPersistedDealStage(t, ctx, tx, active.dealID, tc.stage, tc.reason)
	}
	assertDealStageResponse(t, send(other, `{"stage":"qualified"}`, cookie, actor), other, "qualified", pgtype.Text{})
}

func TestUpdateDealStageConcurrentInPostgres(t *testing.T) {
	if os.Getenv("DEALFORGE_COMPANY_INTEGRATION") != "1" {
		t.Skip("set DEALFORGE_COMPANY_INTEGRATION=1 and the local PostgreSQL environment")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	conn, err := pgx.Connect(ctx, "")
	if err != nil {
		t.Fatal("could not connect to PostgreSQL for concurrent deal stage integration")
	}
	t.Cleanup(func() { _ = conn.Close(context.Background()) })
	setup, err := conn.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = setup.Rollback(context.Background()) }()
	queries := db.New(setup)
	actor := createCompanyTestUser(t, ctx, queries, "it_stage_race_"+rand.Text())
	cookie := createCompanyTestSession(t, ctx, queries, actor)
	fixture := createStageDealFixture(t, ctx, setup, actor, "manager")
	t.Cleanup(func() { cleanupCommittedStageDeal(t, conn, fixture, actor) })
	if err := setup.Commit(ctx); err != nil {
		t.Fatal(err)
	}

	waiter, err := pgx.Connect(ctx, "")
	if err != nil {
		t.Fatal("could not open the second PostgreSQL connection")
	}
	defer func() { _ = waiter.Close(context.Background()) }()
	first, err := conn.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = first.Rollback(context.Background()) }()
	send := func(handler http.Handler, body string) *httptest.ResponseRecorder {
		bounded := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			handler.ServeHTTP(w, r.WithContext(ctx))
		})
		return patchDealStage(bounded, fixture.companyID.String(), fixture.teamID.String(), fixture.dealID.String(), body, cookie, actor.String())
	}
	firstHandler := routes(nil, db.New(first), postgresCompanyStore{database: first}, true)
	secondHandler := routes(nil, db.New(waiter), postgresCompanyStore{database: waiter}, true)
	blockerPID, waiterPID := int32(conn.PgConn().PID()), int32(waiter.PgConn().PID())
	reason := pgtype.Text{String: "Funding cut", Valid: true}
	before := assertDealStageResponse(t, send(firstHandler, `{"stage":"lost","lost_reason":"Funding cut"}`), fixture, "lost", reason)
	// A second request on an independent connection must wait for the first transaction.
	responses := make(chan *httptest.ResponseRecorder, 1)
	done := make(chan struct{})
	go func() {
		defer close(done)
		responses <- send(secondHandler, `{"stage":"lost"}`)
	}()
	defer func() {
		_ = first.Rollback(context.Background())
		<-done
	}()
	waitForDealStageLock(t, ctx, first, blockerPID, waiterPID, done)
	if err := first.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case response := <-responses:
		after := assertDealStageResponse(t, response, fixture, "lost", reason)
		if !after.UpdatedAt.Time.After(before.UpdatedAt.Time) {
			t.Fatal("the waiting update should apply after the first")
		}
	case <-ctx.Done():
		t.Fatal("waiting stage update did not finish")
	}
	assertPersistedDealStage(t, ctx, conn, fixture.dealID, "lost", reason)
}

func waitForDealStageLock(t *testing.T, ctx context.Context, tx pgx.Tx, blockerPID, waiterPID int32, done <-chan struct{}) {
	t.Helper()
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	for {
		var blocked bool
		err := tx.QueryRow(ctx, "SELECT $1::int = ANY(pg_blocking_pids($2::int))", blockerPID, waiterPID).Scan(&blocked)
		if err != nil {
			t.Fatal(err)
		}
		if blocked {
			return
		}
		select {
		case <-done:
			t.Fatal("concurrent update finished without waiting for the first transaction")
		case <-ctx.Done():
			t.Fatal("concurrent update did not reach the PostgreSQL lock")
		case <-ticker.C:
		}
	}
}

func cleanupCommittedStageDeal(t *testing.T, conn *pgx.Conn, fixture stageDealFixture, actor pgtype.UUID) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err := pgx.BeginFunc(ctx, conn, func(tx pgx.Tx) error {
		for _, command := range []struct {
			sql string
			id  pgtype.UUID
		}{
			{"DELETE FROM dealforge.deals WHERE id = $1", fixture.dealID},
			{"DELETE FROM dealforge.contacts WHERE company_id = $1", fixture.companyID},
			{"DELETE FROM dealforge.team_memberships WHERE team_id = $1", fixture.teamID},
			{"DELETE FROM dealforge.teams WHERE id = $1", fixture.teamID},
			{"DELETE FROM dealforge.company_memberships WHERE company_id = $1", fixture.companyID},
			{"DELETE FROM dealforge.companies WHERE id = $1", fixture.companyID},
			{"DELETE FROM dealforge.sessions WHERE user_id = $1", actor},
			{"DELETE FROM dealforge.users WHERE id = $1", actor},
		} {
			if _, err := tx.Exec(ctx, command.sql, command.id); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Errorf("could not clean up committed stage fixtures: %v", err)
	}
}

func createStageDealFixture(t *testing.T, ctx context.Context, tx pgx.Tx, actor pgtype.UUID, role string) stageDealFixture {
	t.Helper()
	queries := db.New(tx)
	company, err := queries.CreateCompany(ctx, "Stage update fixture")
	if err != nil {
		t.Fatal(err)
	}
	if err := queries.AddCompanyAdmin(ctx, db.AddCompanyAdminParams{CompanyID: company.ID, UserID: actor}); err != nil {
		t.Fatal(err)
	}
	teamID := createStageDealTeam(t, ctx, tx, company.ID, actor, role, "Sales")
	contact, err := queries.CreateContact(ctx, db.CreateContactParams{CompanyID: company.ID, Name: "Fixture", Email: "fixture@example.test"})
	if err != nil {
		t.Fatal(err)
	}
	var amount pgtype.Numeric
	if err := amount.Scan("999999999999999.9999"); err != nil {
		t.Fatal(err)
	}
	deal, err := queries.CreateDeal(ctx, db.CreateDealParams{CompanyID: company.ID, TeamID: teamID, ContactID: contact.ID,
		Title: "Stage fixture", Amount: amount, Currency: "USD", Stage: "new"})
	if err != nil {
		t.Fatal(err)
	}
	return stageDealFixture{company.ID, teamID, deal.ID}
}

func createStageDealTeam(t *testing.T, ctx context.Context, tx pgx.Tx, companyID, actor pgtype.UUID, role, name string) pgtype.UUID {
	t.Helper()
	team, err := db.New(tx).CreateTeam(ctx, db.CreateTeamParams{CompanyID: companyID, Name: name})
	if err != nil {
		t.Fatal(err)
	}
	if role != "" {
		if _, err := tx.Exec(ctx, "INSERT INTO dealforge.team_memberships (company_id, team_id, user_id, role) VALUES ($1, $2, $3, $4)",
			companyID, team.ID, actor, role); err != nil {
			t.Fatal(err)
		}
	}
	return team.ID
}

func assertDealStageResponse(t *testing.T, response *httptest.ResponseRecorder, fixture stageDealFixture, stage string, reason pgtype.Text) dealResponse {
	t.Helper()
	var deal dealResponse
	if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &deal) != nil ||
		deal.ID != fixture.dealID || deal.CompanyID != fixture.companyID || deal.TeamID != fixture.teamID ||
		!deal.ContactID.Valid || deal.Title != "Stage fixture" || deal.Amount != "999999999999999.9999" ||
		deal.Currency != "USD" || deal.Stage != stage || deal.LostReason != reason {
		t.Fatalf("unexpected deal stage response: %d %s", response.Code, response.Body)
	}
	return deal
}

func assertPersistedDealStage(t *testing.T, ctx context.Context, database db.DBTX, id pgtype.UUID, stage string, reason pgtype.Text) {
	t.Helper()
	var savedStage string
	var savedReason pgtype.Text
	err := database.QueryRow(ctx, "SELECT stage, lost_reason FROM dealforge.deals WHERE id = $1", id).Scan(&savedStage, &savedReason)
	if err != nil || savedStage != stage || savedReason != reason {
		t.Fatalf("expected stage %q and loss reason %+v, got %q and %+v, error %v", stage, reason, savedStage, savedReason, err)
	}
}
