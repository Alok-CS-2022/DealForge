package main

import (
	"crypto/rand"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/Alok-CS-2022/DealForge/backend/internal/db"
	"github.com/jackc/pgx/v5/pgtype"
)

func TestListDealsInPostgres(t *testing.T) {
	ctx, tx := companyFixtureTransaction(t)
	queries := db.New(tx)
	tag := strings.ToLower(rand.Text())[:18]
	seller := createCompanyTestUser(t, ctx, queries, "it_seller_"+tag)
	manager := createCompanyTestUser(t, ctx, queries, "it_manager_"+tag)
	sellerCookie := createCompanyTestSession(t, ctx, queries, seller)
	managerCookie := createCompanyTestSession(t, ctx, queries, manager)
	store := postgresCompanyStore{database: tx}
	handler := routes(queries, queries, store, true)

	company := func(name string, admin pgtype.UUID) (pgtype.UUID, pgtype.UUID) {
		t.Helper()
		row, err := queries.CreateCompany(ctx, name+" "+tag)
		if err != nil {
			t.Fatal(err)
		}
		if err := queries.AddCompanyAdmin(ctx, db.AddCompanyAdminParams{CompanyID: row.ID, UserID: admin}); err != nil {
			t.Fatal(err)
		}
		contact, err := queries.CreateContact(ctx, db.CreateContactParams{CompanyID: row.ID, Name: "Fixture", Email: "fixture@example.test"})
		if err != nil {
			t.Fatal(err)
		}
		return row.ID, contact.ID
	}
	team := func(companyID pgtype.UUID, name string, userID pgtype.UUID, role string) pgtype.UUID {
		t.Helper()
		row, err := queries.CreateTeam(ctx, db.CreateTeamParams{CompanyID: companyID, Name: name})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := tx.Exec(ctx, `INSERT INTO dealforge.team_memberships (company_id, team_id, user_id, role) VALUES ($1, $2, $3, $4)`,
			companyID, row.ID, userID, role); err != nil {
			t.Fatal(err)
		}
		return row.ID
	}
	const amount = "999999999999999.9999"
	addDeal := func(companyID, teamID, contactID pgtype.UUID, title string) db.CreateDealRow {
		t.Helper()
		params := db.CreateDealParams{
			CompanyID: companyID, TeamID: teamID, ContactID: contactID, Title: title,
			Stage: "proposal", Currency: "USD",
		}
		if err := params.Amount.Scan(amount); err != nil {
			t.Fatal(err)
		}
		row, err := queries.CreateDeal(ctx, params)
		if err != nil {
			t.Fatal(err)
		}
		return row
	}

	target, targetContact := company("Deals company", seller)
	foreign, foreignContact := company("Foreign company", manager)
	archived, archivedContact := company("Archived company", seller)
	if _, err := tx.Exec(ctx, `INSERT INTO dealforge.company_memberships (company_id, user_id, role) VALUES ($1, $2, 'member')`,
		target, manager); err != nil {
		t.Fatal(err)
	}
	active := team(target, "Sales", seller, "salesperson")
	empty := team(target, "Empty", seller, "manager")
	restricted := team(target, "Restricted", manager, "manager")
	archivedTeam := team(target, "Archived team", seller, "manager")
	foreignTeam := team(foreign, "Foreign team", manager, "manager")
	teamInArchivedCompany := team(archived, "In archived company", seller, "manager")

	old := addDeal(target, active, targetContact, "Old deal")
	first := addDeal(target, active, targetContact, "Tied deal A")
	second := addDeal(target, active, targetContact, "Tied deal B")
	otherTeamDeal := addDeal(target, restricted, targetContact, "Restricted")
	foreignDeal := addDeal(foreign, foreignTeam, foreignContact, "Foreign")
	addDeal(target, archivedTeam, targetContact, "Archived team")
	addDeal(archived, teamInArchivedCompany, archivedContact, "Archived company")
	if _, err := tx.Exec(ctx, `UPDATE dealforge.deals SET created_at = now() - interval '1 day' WHERE id = $1`, old.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `UPDATE dealforge.teams SET archived_at = now() WHERE id = $1`, archivedTeam); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `UPDATE dealforge.companies SET archived_at = now() WHERE id = $1`, archived); err != nil {
		t.Fatal(err)
	}

	assertUnauthorizedSession(t, getDeals(handler, target.String(), active.String(), nil, seller.String()))
	assertCompanyPreconditionFailed(t, getDeals(handler, target.String(), active.String(), sellerCookie, manager.String()))
	if result := getDeals(handler, target.String(), "invalid", sellerCookie, ""); result.Code != http.StatusBadRequest {
		t.Fatalf("invalid ID returned %d, want 400", result.Code)
	}
	if result := getDeals(handler, target.String(), empty.String(), sellerCookie, ""); result.Code != http.StatusOK || result.Body.String() != "[]\n" {
		t.Fatalf("empty team returned %d, want 200 with []", result.Code)
	}
	missing := pgtype.UUID{Valid: true}.String()
	for _, tc := range []struct {
		name, companyID, teamID string
	}{
		{"company admin outside the team", target.String(), restricted.String()},
		{"foreign company", foreign.String(), foreignTeam.String()},
		{"foreign team with accessible company", target.String(), foreignTeam.String()},
		{"archived team", target.String(), archivedTeam.String()},
		{"archived company", archived.String(), teamInArchivedCompany.String()},
		{"missing team", target.String(), missing},
		{"missing company", missing, active.String()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			response := getDeals(handler, tc.companyID, tc.teamID, sellerCookie, "")
			if response.Code != http.StatusNotFound || response.Body.String() != "{\"error\":\"team not found\"}\n" {
				t.Fatalf("expected indistinguishable 404, got %d", response.Code)
			}
		})
	}

	assertDeals := func(companyID, teamID pgtype.UUID, cookie *http.Cookie, want ...db.CreateDealRow) {
		t.Helper()
		response := getDeals(handler, companyID.String(), teamID.String(), cookie, "")
		var rows []dealResponse
		if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &rows) != nil || len(rows) != len(want) {
			t.Fatalf("unexpected listing response, status %d", response.Code)
		}
		for i, row := range rows {
			if row.ID != want[i].ID || row.CompanyID != companyID || row.TeamID != teamID || row.ContactID != want[i].ContactID ||
				row.Title != want[i].Title || row.Amount != amount || row.Currency != "USD" || row.Stage != "proposal" ||
				row.ExpectedCloseDate.Valid || row.LostReason.Valid || row.AssigneeUserID.Valid || !row.CreatedAt.Valid || !row.UpdatedAt.Valid {
				t.Fatalf("unexpected deal details or ordering at index %d", i)
			}
		}
	}
	if second.ID.String() < first.ID.String() {
		first, second = second, first
	}
	assertDeals(target, active, sellerCookie, first, second, old)
	assertDeals(target, restricted, managerCookie, otherTeamDeal)
	assertDeals(foreign, foreignTeam, managerCookie, foreignDeal)
	if result := getDeals(handler, target.String(), active.String(), managerCookie, ""); result.Code != http.StatusNotFound {
		t.Fatalf("member of a different team got %d, want 404", result.Code)
	}
}
