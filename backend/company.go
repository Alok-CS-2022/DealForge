package main

import (
	"context"
	"log"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/Alok-CS-2022/DealForge/backend/internal/db"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

const initialTeamName = "General"
const expectedUserHeader = "X-DealForge-Expected-User-ID"

type companyCreator interface {
	CreateCompanyWithTeam(context.Context, string, pgtype.UUID) (createdCompany, error)
}

type createdCompany struct {
	Company struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"company"`
	Team struct {
		ID        string `json:"id"`
		CompanyID string `json:"company_id"`
		Name      string `json:"name"`
	} `json:"team"`
}

func createCompany(companies companyCreator) func(http.ResponseWriter, *http.Request, publicUser) {
	return func(w http.ResponseWriter, r *http.Request, user publicUser) {
		if !checkExpectedUser(w, r, user.ID, true) {
			return
		}
		var input struct {
			Name string `json:"name"`
		}
		if !decodeAuthRequest(w, r, &input) {
			writeAuthError(w, http.StatusBadRequest, "invalid company JSON")
			return
		}
		name := strings.TrimSpace(input.Name)
		if name == "" || strings.ContainsRune(name, 0) || utf8.RuneCountInString(name) > 100 {
			writeAuthError(w, http.StatusBadRequest, "name must be 1-100 characters")
			return
		}
		var creatorID pgtype.UUID
		if err := creatorID.Scan(user.ID); err != nil || !creatorID.Valid || companies == nil {
			writeAuthError(w, http.StatusInternalServerError, "could not create company")
			return
		}
		created, err := companies.CreateCompanyWithTeam(r.Context(), name, creatorID)
		if err != nil {
			log.Print("company creation failed")
			writeAuthError(w, http.StatusInternalServerError, "could not create company")
			return
		}
		writeAuthJSON(w, http.StatusCreated, created)
	}
}

func checkExpectedUser(w http.ResponseWriter, r *http.Request, userID string, required bool) bool {
	values := r.Header.Values(expectedUserHeader)
	if len(values) == 0 && !required {
		return true
	}
	if len(values) == 1 && values[0] != "" && values[0] == userID {
		return true
	}
	writeAuthError(w, http.StatusPreconditionFailed, "session identity precondition failed")
	return false
}

type postgresCompanyStore struct {
	database interface {
		db.DBTX
		Begin(context.Context) (pgx.Tx, error)
	}
}

func (c postgresCompanyStore) CreateCompanyWithTeam(ctx context.Context, name string, creatorID pgtype.UUID) (createdCompany, error) {
	var result createdCompany
	err := pgx.BeginFunc(ctx, c.database, func(tx pgx.Tx) error {
		queries := db.New(tx)
		company, err := queries.CreateCompany(ctx, name)
		if err != nil {
			return err
		}
		if err := queries.AddCompanyAdmin(ctx, db.AddCompanyAdminParams{CompanyID: company.ID, UserID: creatorID}); err != nil {
			return err
		}
		team, err := queries.CreateTeam(ctx, db.CreateTeamParams{CompanyID: company.ID, Name: initialTeamName})
		if err != nil {
			return err
		}
		if err := queries.AddTeamManager(ctx, db.AddTeamManagerParams{CompanyID: company.ID, TeamID: team.ID, UserID: creatorID}); err != nil {
			return err
		}

		result.Company.ID = company.ID.String()
		result.Company.Name = company.Name
		result.Team.ID = team.ID.String()
		result.Team.CompanyID = team.CompanyID.String()
		result.Team.Name = team.Name
		return nil
	})
	if err != nil {
		return createdCompany{}, err
	}
	return result, nil
}
