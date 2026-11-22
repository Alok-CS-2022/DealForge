package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Alok-CS-2022/DealForge/backend/internal/db"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
)

func TestCreateCompanyInPostgres(t *testing.T) {
	ctx, tx := companyFixtureTransaction(t)
	queries := db.New(tx)
	tag := strings.ToLower(rand.Text())[:18]
	creatorID := createCompanyTestUser(t, ctx, queries, "it_company_"+tag)
	outsiderID := createCompanyTestUser(t, ctx, queries, "it_other_"+tag)
	cookie := createCompanyTestSession(t, ctx, queries, creatorID)
	handler := routes(queries, queries, postgresCompanyStore{database: tx}, true)

	assertUnauthorizedSession(t, postCompany(handler, `{"name":"Unauthenticated `+tag+`"}`, nil))
	if response := postCompany(handler, `{"name":"  "}`, cookie, creatorID.String()); response.Code != http.StatusBadRequest {
		t.Fatalf("invalid company name returned %d, want 400", response.Code)
	}
	t.Run("rejects the other user's ID without persisting a company", func(t *testing.T) {
		name := "Mismatched " + tag
		response := postCompany(handler, `{"name":"`+name+`"}`, cookie, outsiderID.String())
		assertCompanyPreconditionFailed(t, response)
		var count int
		if err := tx.QueryRow(ctx, "SELECT count(*) FROM dealforge.companies WHERE name = $1", name).Scan(&count); err != nil || count != 0 {
			t.Fatal("mismatched request left a company behind")
		}
	})
	name := "Company " + tag
	response := postCompany(handler, `{"name":"  `+name+`  "}`, cookie, creatorID.String())
	var created createdCompany
	if response.Code != http.StatusCreated || json.Unmarshal(response.Body.Bytes(), &created) != nil ||
		created.Company.Name != name || created.Team.CompanyID != created.Company.ID || created.Team.Name != initialTeamName {
		t.Fatalf("unexpected create response, status %d", response.Code)
	}

	var companyRole, teamRole string
	err := tx.QueryRow(ctx, `
		SELECT cm.role, tm.role
		FROM dealforge.companies c
		JOIN dealforge.company_memberships cm ON cm.company_id = c.id
		JOIN dealforge.teams t ON t.company_id = c.id
		JOIN dealforge.team_memberships tm ON tm.team_id = t.id
			AND tm.company_id = c.id AND tm.user_id = cm.user_id
		WHERE c.id::text = $1 AND c.name = $2 AND t.id::text = $3 AND t.name = $4 AND cm.user_id = $5
	`, created.Company.ID, name, created.Team.ID, initialTeamName, creatorID).Scan(&companyRole, &teamRole)
	if err != nil || companyRole != "admin" || teamRole != "manager" {
		t.Fatal("company, team, and both creator memberships were not persisted")
	}

	t.Run("rolls back after a real final foreign key violation", func(t *testing.T) {
		fault := &companyManagerFailure{Tx: tx, outsiderID: outsiderID}
		failingHandler := routes(queries, queries, postgresCompanyStore{database: fault}, true)
		failedName := "Rollback " + tag
		failed := postCompany(failingHandler, `{"name":"`+failedName+`"}`, cookie, creatorID.String())
		if failed.Code != http.StatusInternalServerError || failed.Body.String() != "{\"error\":\"could not create company\"}\n" ||
			fault.sqlState != "23503" {
			t.Fatalf("expected sanitized failure from the team membership constraint; status %d, SQLSTATE %q", failed.Code, fault.sqlState)
		}
		var count int
		if err := tx.QueryRow(ctx, "SELECT count(*) FROM dealforge.companies WHERE name = $1", failedName).Scan(&count); err != nil || count != 0 {
			t.Fatal("failed creation left a company behind or poisoned the fixture transaction")
		}
	})
}

func companyFixtureTransaction(t *testing.T) (context.Context, pgx.Tx) {
	t.Helper()
	if os.Getenv("DEALFORGE_COMPANY_INTEGRATION") != "1" {
		t.Skip("set DEALFORGE_COMPANY_INTEGRATION=1 and the local PostgreSQL environment")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	t.Cleanup(cancel)
	conn, err := pgx.Connect(ctx, "")
	if err != nil {
		t.Fatal("could not connect to PostgreSQL for company integration")
	}
	t.Cleanup(func() { _ = conn.Close(context.Background()) })
	tx, err := conn.Begin(ctx)
	if err != nil {
		t.Fatal("could not start company fixture transaction")
	}
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := tx.Rollback(cleanup); err != nil {
			t.Error("could not roll back company fixtures")
		}
	})
	return ctx, tx
}

func createCompanyTestSession(t *testing.T, ctx context.Context, queries *db.Queries, userID pgtype.UUID) *http.Cookie {
	t.Helper()
	var tokenBytes [sessionTokenSize]byte
	if _, err := rand.Read(tokenBytes[:]); err != nil {
		t.Fatal("could not generate a session fixture")
	}
	token := base64.RawURLEncoding.EncodeToString(tokenBytes[:])
	digest := sha256.Sum256([]byte(token))
	if err := queries.CreateSession(ctx, db.CreateSessionParams{
		UserID: userID, TokenHash: digest[:],
		ExpiresAt: pgtype.Timestamptz{Time: time.Now().Add(time.Hour), Valid: true},
	}); err != nil {
		t.Fatal("could not insert a session fixture")
	}
	return &http.Cookie{Name: "__Host-dealforge_session", Value: token}
}

func createCompanyTestUser(t *testing.T, ctx context.Context, queries *db.Queries, username string) pgtype.UUID {
	t.Helper()
	user, err := queries.CreateUser(ctx, db.CreateUserParams{
		Username: username, Email: username + "@example.test",
		DisplayName: "Company integration test", PasswordHash: "unused-test-fixture",
	})
	if err != nil {
		t.Fatal("could not create a company fixture user")
	}
	return user.ID
}

// Exercise PostgreSQL's final insert constraint inside the service savepoint.
type companyManagerFailure struct {
	pgx.Tx
	outsiderID pgtype.UUID
	sqlState   string
}

func (f *companyManagerFailure) Begin(ctx context.Context) (pgx.Tx, error) {
	tx, err := f.Tx.Begin(ctx)
	if err != nil {
		return nil, err
	}
	return &companyManagerFailureTx{Tx: tx, fault: f}, nil
}

type companyManagerFailureTx struct {
	pgx.Tx
	fault *companyManagerFailure
}

func (tx *companyManagerFailureTx) Exec(ctx context.Context, query string, args ...any) (pgconn.CommandTag, error) {
	if !strings.Contains(query, "INSERT INTO dealforge.team_memberships") {
		return tx.Tx.Exec(ctx, query, args...)
	}
	args[2] = tx.fault.outsiderID
	tag, err := tx.Tx.Exec(ctx, query, args...)
	var dbError *pgconn.PgError
	if errors.As(err, &dbError) {
		tx.fault.sqlState = dbError.Code
	}
	return tag, err
}
