package main

import (
	"context"
	"log"
	"net/http"

	"github.com/Alok-CS-2022/DealForge/backend/internal/db"
	"github.com/jackc/pgx/v5/pgtype"
)

type companyLister interface {
	ListCompaniesForUser(context.Context, pgtype.UUID) ([]db.ListCompaniesForUserRow, error)
}

type companyStore interface {
	companyCreator
	companyLister
	contactCreator
	contactLister
}

type companyMembership struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Role string `json:"role"`
}

func listCompanies(companies companyLister) func(http.ResponseWriter, *http.Request, publicUser) {
	return func(w http.ResponseWriter, r *http.Request, user publicUser) {
		if !checkExpectedUser(w, r, user.ID, false) {
			return
		}
		var userID pgtype.UUID
		if err := userID.Scan(user.ID); err != nil || !userID.Valid || companies == nil {
			writeAuthError(w, http.StatusInternalServerError, "could not list companies")
			return
		}
		rows, err := companies.ListCompaniesForUser(r.Context(), userID)
		if err != nil {
			log.Print("company list query failed")
			writeAuthError(w, http.StatusInternalServerError, "could not list companies")
			return
		}
		result := make([]companyMembership, 0, len(rows))
		for _, row := range rows {
			result = append(result, companyMembership{ID: row.ID.String(), Name: row.Name, Role: row.Role})
		}
		writeAuthJSON(w, http.StatusOK, result)
	}
}

func (c postgresCompanyStore) ListCompaniesForUser(ctx context.Context, userID pgtype.UUID) ([]db.ListCompaniesForUserRow, error) {
	return db.New(c.database).ListCompaniesForUser(ctx, userID)
}
