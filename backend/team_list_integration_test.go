package main

import (
	"crypto/rand"
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/Alok-CS-2022/DealForge/backend/internal/db"
	"github.com/jackc/pgx/v5/pgtype"
)

func TestListTeamsInPostgres(t *testing.T) {
	ctx, tx := companyFixtureTransaction(t)
	queries := db.New(tx)
	tag := strings.ToLower(rand.Text())[:18]
	actor := createCompanyTestUser(t, ctx, queries, "it_teams_"+tag)
	other := createCompanyTestUser(t, ctx, queries, "it_other_"+tag)
	cookie := createCompanyTestSession(t, ctx, queries, actor)
	otherCookie := createCompanyTestSession(t, ctx, queries, other)
	handler := routes(queries, queries, postgresCompanyStore{database: tx}, true)

	addCompany := func(name string, admin pgtype.UUID) pgtype.UUID {
		t.Helper()
		row, err := queries.CreateCompany(ctx, name+" "+tag)
		if err != nil {
			t.Fatal(err)
		}
		if err := queries.AddCompanyAdmin(ctx, db.AddCompanyAdminParams{CompanyID: row.ID, UserID: admin}); err != nil {
			t.Fatal(err)
		}
		return row.ID
	}
	addTeam := func(companyID, userID pgtype.UUID, name, role string) teamMembership {
		t.Helper()
		row, err := queries.CreateTeam(ctx, db.CreateTeamParams{CompanyID: companyID, Name: name})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := tx.Exec(ctx, `INSERT INTO dealforge.team_memberships (company_id, team_id, user_id, role) VALUES ($1, $2, $3, $4)`,
			companyID, row.ID, userID, role); err != nil {
			t.Fatal(err)
		}
		return teamMembership{ID: row.ID.String(), CompanyID: companyID.String(), Name: name, Role: role}
	}
	assertTeams := func(companyID pgtype.UUID, session *http.Cookie, want ...teamMembership) {
		t.Helper()
		response := getTeams(handler, companyID.String(), session, "")
		var teams []teamMembership
		if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &teams) != nil ||
			teams == nil || !slices.Equal(teams, want) {
			t.Fatalf("unexpected team listing, status %d, body %s", response.Code, response.Body.String())
		}
	}

	target := addCompany("Teams company", actor)
	additional := addCompany("Second company", actor)
	inaccessible := addCompany("Other company", other)
	archived := addCompany("Archived company", actor)
	if _, err := tx.Exec(ctx, `INSERT INTO dealforge.company_memberships (company_id, user_id, role) VALUES ($1, $2, 'member')`,
		target, other); err != nil {
		t.Fatal(err)
	}
	restricted := addTeam(target, other, "Restricted", "manager")
	archivedTeam := addTeam(target, actor, "Archived team", "manager")
	isolated := addTeam(additional, actor, "Second company team", "manager")
	addTeam(inaccessible, other, "Other company team", "manager")
	addTeam(archived, actor, "Team in archived company", "manager")
	if _, err := tx.Exec(ctx, `UPDATE dealforge.teams SET archived_at = now() WHERE id = $1`, archivedTeam.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `UPDATE dealforge.companies SET archived_at = now() WHERE id = $1`, archived); err != nil {
		t.Fatal(err)
	}

	assertUnauthorizedSession(t, getTeams(handler, target.String(), nil, actor.String()))
	if response := getTeams(handler, "invalid", cookie, ""); response.Code != http.StatusBadRequest {
		t.Fatalf("invalid company ID returned %d, want 400", response.Code)
	}
	assertCompanyPreconditionFailed(t, getTeams(handler, target.String(), cookie, other.String()))
	for _, companyID := range []string{inaccessible.String(), archived.String(), "00000000-0000-0000-0000-000000000000"} {
		response := getTeams(handler, companyID, cookie, "")
		if response.Code != http.StatusNotFound || response.Body.String() != "{\"error\":\"company not found\"}\n" {
			t.Fatalf("expected indistinguishable company 404, got %d", response.Code)
		}
	}
	assertTeams(target, cookie)
	zulu := addTeam(target, actor, "Zulu", "manager")
	assertTeams(target, cookie, zulu)
	alpha := addTeam(target, actor, "Alpha", "salesperson")
	assertTeams(target, cookie, alpha, zulu)
	assertTeams(target, otherCookie, restricted)
	assertTeams(additional, cookie, isolated)
}
