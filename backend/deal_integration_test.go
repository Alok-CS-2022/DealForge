package main

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/Alok-CS-2022/DealForge/backend/internal/db"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
)

func TestCreateDealInPostgres(t *testing.T) {
	ctx, tx := companyFixtureTransaction(t)
	queries := db.New(tx)
	tag := strings.ToLower(rand.Text())[:18]
	actor := createCompanyTestUser(t, ctx, queries, "it_dealer_"+tag)
	manager := createCompanyTestUser(t, ctx, queries, "it_manager_"+tag)
	cookie := createCompanyTestSession(t, ctx, queries, actor)
	managerCookie := createCompanyTestSession(t, ctx, queries, manager)
	store := postgresCompanyStore{database: tx}
	handler := routes(queries, queries, store, true)

	company := func(name string) pgtype.UUID {
		t.Helper()
		row, err := queries.CreateCompany(ctx, name+" "+tag)
		if err != nil {
			t.Fatal(err)
		}
		if err := queries.AddCompanyAdmin(ctx, db.AddCompanyAdminParams{CompanyID: row.ID, UserID: actor}); err != nil {
			t.Fatal(err)
		}
		return row.ID
	}
	team := func(companyID pgtype.UUID, name string) pgtype.UUID {
		t.Helper()
		row, err := queries.CreateTeam(ctx, db.CreateTeamParams{CompanyID: companyID, Name: name})
		if err != nil {
			t.Fatal(err)
		}
		return row.ID
	}
	contact := func(companyID pgtype.UUID) pgtype.UUID {
		t.Helper()
		row, err := queries.CreateContact(ctx, db.CreateContactParams{CompanyID: companyID, Name: "Fixture", Email: "fixture@example.test"})
		if err != nil {
			t.Fatal(err)
		}
		return row.ID
	}
	addTeamMember := func(companyID, teamID, userID pgtype.UUID, role string) {
		t.Helper()
		if _, err := tx.Exec(ctx, `INSERT INTO dealforge.team_memberships (company_id, team_id, user_id, role) VALUES ($1, $2, $3, $4)`,
			companyID, teamID, userID, role); err != nil {
			t.Fatal(err)
		}
	}

	target := company("Deal company")
	other := company("Other company")
	archived := company("Archived company")
	activeTeam := team(target, "Sales")
	restrictedTeam := team(target, "Restricted")
	archivedTeam := team(target, "Archived")
	otherTeam := team(other, "Other team")
	teamInArchivedCompany := team(archived, "Team in archived company")
	targetContact := contact(target)
	otherContact := contact(other)
	archivedContact := contact(archived)
	addTeamMember(target, activeTeam, actor, "salesperson")
	addTeamMember(target, archivedTeam, actor, "manager")
	addTeamMember(other, otherTeam, actor, "manager")
	addTeamMember(archived, teamInArchivedCompany, actor, "manager")
	if _, err := tx.Exec(ctx, `INSERT INTO dealforge.company_memberships (company_id, user_id, role) VALUES ($1, $2, 'member')`, target, manager); err != nil {
		t.Fatal(err)
	}
	addTeamMember(target, activeTeam, manager, "manager")
	addTeamMember(target, restrictedTeam, manager, "manager")
	if _, err := tx.Exec(ctx, `UPDATE dealforge.teams SET archived_at = now() WHERE id = $1`, archivedTeam); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `UPDATE dealforge.companies SET archived_at = now() WHERE id = $1`, archived); err != nil {
		t.Fatal(err)
	}

	body := func(contactID pgtype.UUID) string {
		return strings.Replace(validDealJSON, dealTestContactID, contactID.String(), 1)
	}
	assertUnauthorizedSession(t, postDeal(handler, target.String(), activeTeam.String(), body(targetContact), nil, actor.String()))
	assertCompanyPreconditionFailed(t, postDeal(handler, target.String(), activeTeam.String(), body(targetContact), cookie, manager.String()))
	invalid := strings.Replace(body(targetContact), "123.4567", "123.45678", 1)
	if response := postDeal(handler, target.String(), activeTeam.String(), invalid, cookie, actor.String()); response.Code != http.StatusBadRequest {
		t.Fatalf("invalid amount returned %d, want 400", response.Code)
	}
	for _, test := range []struct {
		name                         string
		companyID, teamID, contactID pgtype.UUID
		session                      *http.Cookie
		userID                       pgtype.UUID
	}{
		{"cross-company contact", target, activeTeam, otherContact, cookie, actor},
		{"missing contact", target, activeTeam, pgtype.UUID{Valid: true}, cookie, actor},
		{"company admin outside team", target, restrictedTeam, targetContact, cookie, actor},
		{"missing team", target, pgtype.UUID{Valid: true}, targetContact, cookie, actor},
		{"company and team disagree", target, otherTeam, targetContact, cookie, actor},
		{"outsider", other, otherTeam, otherContact, managerCookie, manager},
		{"archived team", target, archivedTeam, targetContact, cookie, actor},
		{"archived company", archived, teamInArchivedCompany, archivedContact, cookie, actor},
	} {
		t.Run(test.name, func(t *testing.T) {
			response := postDeal(handler, test.companyID.String(), test.teamID.String(), body(test.contactID), test.session, test.userID.String())
			if response.Code != http.StatusNotFound || response.Body.String() != "{\"error\":\"team or contact not found\"}\n" {
				t.Fatalf("expected indistinguishable 404, got %d", response.Code)
			}
		})
	}
	checkCount := func(want int) {
		t.Helper()
		var count int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM dealforge.deals WHERE company_id IN ($1, $2, $3)`, target, other, archived).Scan(&count); err != nil || count != want {
			t.Fatalf("expected %d fixture deals, got %d (error: %v)", want, count, err)
		}
	}
	checkCount(0)

	// A real failed insert must roll back its savepoint and release it for the next call.
	params := db.CreateDealParams{CompanyID: target, TeamID: activeTeam, ContactID: targetContact, Title: "Fail",
		Currency: "USD", Stage: "invalid"}
	if err := params.Amount.Scan("1"); err != nil {
		t.Fatal(err)
	}
	var pgErr *pgconn.PgError
	if _, err := store.CreateTeamDeal(ctx, actor, params); !errors.As(err, &pgErr) || pgErr.Code != "23514" {
		t.Fatalf("expected PostgreSQL stage constraint violation, got %v", err)
	}
	checkCount(0)

	lost := strings.TrimSuffix(strings.Replace(body(targetContact), "123.4567", "999999999999999.9999", 1), "}") +
		`,"stage":" Lost ","lost_reason":" Customer declined ","expected_close_date":"2028-02-29"}`
	response := postDeal(handler, target.String(), activeTeam.String(), lost, cookie, actor.String())
	var result dealResponse
	if response.Code != http.StatusCreated || json.Unmarshal(response.Body.Bytes(), &result) != nil ||
		!result.ID.Valid || result.CompanyID != target || result.TeamID != activeTeam || result.ContactID != targetContact ||
		result.Title != "Test deal" || result.Amount != "999999999999999.9999" || result.Currency != "USD" || result.Stage != "lost" ||
		!result.ExpectedCloseDate.Valid || result.ExpectedCloseDate.Time.Format("2006-01-02") != "2028-02-29" ||
		result.LostReason != (pgtype.Text{String: "Customer declined", Valid: true}) ||
		result.AssigneeUserID.Valid || !result.CreatedAt.Valid || !result.UpdatedAt.Valid {
		t.Fatalf("unexpected salesperson creation after rollback, status %d, body %s", response.Code, response.Body.String())
	}
	if response := postDeal(handler, target.String(), activeTeam.String(), body(targetContact), managerCookie, manager.String()); response.Code != http.StatusCreated {
		t.Fatalf("team manager creation returned %d, want 201", response.Code)
	}
	checkCount(2)
}
