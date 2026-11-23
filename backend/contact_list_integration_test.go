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

func TestListContactsInPostgres(t *testing.T) {
	ctx, tx := companyFixtureTransaction(t)
	queries := db.New(tx)
	tag := strings.ToLower(rand.Text())[:18]
	member := createCompanyTestUser(t, ctx, queries, "it_contacts_"+tag)
	outsider := createCompanyTestUser(t, ctx, queries, "it_outsider_"+tag)
	cookie := createCompanyTestSession(t, ctx, queries, member)
	otherCookie := createCompanyTestSession(t, ctx, queries, outsider)
	handler := routes(queries, queries, postgresCompanyStore{database: tx}, true)

	company := func(label string, userID pgtype.UUID) pgtype.UUID {
		t.Helper()
		row, err := queries.CreateCompany(ctx, label+" "+tag)
		if err != nil {
			t.Fatal("could not create contact listing fixture company")
		}
		if _, err := tx.Exec(ctx, `INSERT INTO dealforge.company_memberships (company_id, user_id, role) VALUES ($1, $2, 'member')`,
			row.ID, userID); err != nil {
			t.Fatal("could not add contact listing fixture membership")
		}
		return row.ID
	}
	contact := func(companyID pgtype.UUID, name, email string) pgtype.UUID {
		t.Helper()
		row, err := queries.CreateContact(ctx, db.CreateContactParams{
			CompanyID: companyID, Name: name, Email: email,
			Phone: pgtype.Text{Valid: true, String: "+1 555 0100"},
		})
		if err != nil {
			t.Fatal("could not create contact listing fixture")
		}
		return row.ID
	}

	target := company("Contacts", member)
	empty := company("Empty", member)
	foreign := company("Foreign", outsider)
	archived := company("Archived", member)
	last := contact(target, "Zulu", "zulu@example.test")
	first := contact(target, "Alpha", "one@example.test")
	second := contact(target, "Alpha", "two@example.test")
	foreignContact := contact(foreign, "Private", "private@example.test")
	contact(archived, "Archived", "archived@example.test")
	if _, err := tx.Exec(ctx, `UPDATE dealforge.companies SET archived_at = now() WHERE id = $1`, archived); err != nil {
		t.Fatal("could not archive contact listing fixture company")
	}

	assertUnauthorizedSession(t, getContacts(handler, target.String(), nil))
	if response := getContacts(handler, "invalid", cookie); response.Code != http.StatusBadRequest {
		t.Fatalf("invalid ID returned %d, want 400", response.Code)
	}
	for _, companyID := range []string{foreign.String(), archived.String(), "00000000-0000-0000-0000-000000000000"} {
		response := getContacts(handler, companyID, cookie)
		if response.Code != http.StatusNotFound || response.Body.String() != "{\"error\":\"company not found\"}\n" {
			t.Fatalf("expected indistinguishable company 404, got %d", response.Code)
		}
	}
	if response := getContacts(handler, empty.String(), cookie); response.Code != http.StatusOK || response.Body.String() != "[]\n" {
		t.Fatalf("empty company returned %d, body %s", response.Code, response.Body.String())
	}

	if second.String() < first.String() {
		first, second = second, first
	}
	assertList := func(companyID pgtype.UUID, sessionCookie *http.Cookie, ids ...pgtype.UUID) {
		t.Helper()
		response := getContacts(handler, companyID.String(), sessionCookie)
		var result []contactResponse
		if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &result) != nil || len(result) != len(ids) {
			t.Fatalf("unexpected contact list, status %d", response.Code)
		}
		for index, id := range ids {
			row := result[index]
			if row.ID != id || row.CompanyID != companyID || row.Name == "" || row.Email == "" ||
				row.Phone != (pgtype.Text{Valid: true, String: "+1 555 0100"}) || row.CompanyName.Valid ||
				row.AssigneeUserID.Valid || !row.CreatedAt.Valid || !row.UpdatedAt.Valid {
				t.Fatalf("unexpected contact details or ordering at index %d", index)
			}
		}
	}
	assertList(target, cookie, first, second, last)
	assertList(foreign, otherCookie, foreignContact)
	if response := getContacts(handler, target.String(), otherCookie); response.Code != http.StatusNotFound {
		t.Fatalf("outsider received %d, want 404", response.Code)
	}
}
