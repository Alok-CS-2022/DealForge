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

func TestCreateContactInPostgres(t *testing.T) {
	ctx, tx := companyFixtureTransaction(t)
	queries := db.New(tx)
	tag := strings.ToLower(rand.Text())[:18]
	actor := createCompanyTestUser(t, ctx, queries, "it_contact_"+tag)
	outsider := createCompanyTestUser(t, ctx, queries, "it_outsider_"+tag)
	cookie := createCompanyTestSession(t, ctx, queries, actor)
	otherCookie := createCompanyTestSession(t, ctx, queries, outsider)
	handler := routes(queries, queries, postgresCompanyStore{database: tx}, true)

	addCompany := func(label string, member pgtype.UUID, role string) pgtype.UUID {
		t.Helper()
		company, err := queries.CreateCompany(ctx, label+" "+tag)
		if err != nil {
			t.Fatal("could not create contact fixture company")
		}
		if _, err := tx.Exec(ctx, `INSERT INTO dealforge.company_memberships (company_id, user_id, role) VALUES ($1, $2, $3)`,
			company.ID, member, role); err != nil {
			t.Fatal("could not add contact fixture membership")
		}
		return company.ID
	}

	companyID := addCompany("Contact member", actor, "member")
	secondCompany := addCompany("Contact admin", actor, "admin")
	otherCompany := addCompany("Other tenant", outsider, "admin")
	archivedCompany := addCompany("Archived", actor, "admin")
	if _, err := tx.Exec(ctx, `UPDATE dealforge.companies SET archived_at = now() WHERE id = $1`, archivedCompany); err != nil {
		t.Fatal("could not archive contact fixture company")
	}

	assertUnauthorizedSession(t, postContact(handler, companyID.String(), validContactJSON, nil))
	if response := postContact(handler, companyID.String(), `{"name":"Ada","email":"invalid"}`, cookie); response.Code != http.StatusBadRequest {
		t.Fatalf("expected invalid contact to return 400, got %d", response.Code)
	}
	for _, id := range []string{otherCompany.String(), archivedCompany.String(), "00000000-0000-0000-0000-000000000000"} {
		response := postContact(handler, id, validContactJSON, cookie)
		if response.Code != http.StatusNotFound || response.Body.String() != "{\"error\":\"company not found\"}\n" {
			t.Fatalf("expected indistinguishable 404 for unavailable company, got %d", response.Code)
		}
	}
	checkCount := func(id pgtype.UUID, want int) {
		t.Helper()
		var count int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM dealforge.contacts WHERE company_id = $1`, id).Scan(&count); err != nil || count != want {
			t.Fatalf("expected %d persisted contacts, got %d", want, count)
		}
	}
	checkCount(otherCompany, 0)
	checkCount(archivedCompany, 0)
	checkCount(companyID, 0)

	// Seed mixed case directly to exercise the database index independently
	// of the handler's lowercase normalization.
	if _, err := queries.CreateContact(ctx, db.CreateContactParams{
		CompanyID: companyID, Name: "Existing Ada", Email: "Ada@EXAMPLE.test",
	}); err != nil {
		t.Fatal("could not seed existing contact")
	}
	duplicate := postContact(handler, companyID.String(), validContactJSON, cookie)
	if duplicate.Code != http.StatusConflict || duplicate.Body.String() != "{\"error\":\"contact email already exists in this company\"}\n" {
		t.Fatalf("expected case-insensitive duplicate to return 409, got %d", duplicate.Code)
	}
	checkCount(companyID, 1)
	if response := postContact(handler, companyID.String(), validContactJSON, otherCookie); response.Code != http.StatusNotFound {
		t.Fatalf("duplicate must remain hidden from non-members, got %d", response.Code)
	}
	if response := postContact(handler, secondCompany.String(), validContactJSON, cookie); response.Code != http.StatusCreated {
		t.Fatalf("same email in another authorized company returned %d, want 201", response.Code)
	}
	checkCount(secondCompany, 1)
	checkCount(otherCompany, 0)

	body := `{"name":" Grace Hopper ","email":" GRACE@EXAMPLE.TEST ","phone":" +1 555 0100 ","company_name":" Navy "}`
	response := postContact(handler, companyID.String(), body, cookie)
	var result contactResponse
	if response.Code != http.StatusCreated || json.Unmarshal(response.Body.Bytes(), &result) != nil ||
		!result.ID.Valid || result.CompanyID != companyID || result.Name != "Grace Hopper" || result.Email != "grace@example.test" ||
		result.Phone != (pgtype.Text{Valid: true, String: "+1 555 0100"}) ||
		result.CompanyName != (pgtype.Text{Valid: true, String: "Navy"}) ||
		result.AssigneeUserID.Valid || !result.CreatedAt.Valid || !result.UpdatedAt.Valid {
		t.Fatalf("unexpected contact response after duplicate rollback, status %d", response.Code)
	}
	checkCount(companyID, 2)
	var persistedEmail string
	if err := tx.QueryRow(ctx, `SELECT email FROM dealforge.contacts WHERE id = $1 AND company_id = $2`, result.ID, companyID).Scan(&persistedEmail); err != nil || persistedEmail != result.Email {
		t.Fatal("returned contact was not persisted in the authorized tenant")
	}
}
