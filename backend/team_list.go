package main

import (
	"context"
	"log"
	"net/http"

	"github.com/Alok-CS-2022/DealForge/backend/internal/db"
	"github.com/jackc/pgx/v5/pgtype"
)

type teamLister interface {
	ListCompanyTeams(context.Context, db.ListCompanyTeamsParams) ([]db.ListCompanyTeamsRow, error)
}

type teamMembership struct {
	ID        string `json:"id"`
	CompanyID string `json:"company_id"`
	Name      string `json:"name"`
	Role      string `json:"role"`
}

func listTeams(teams teamLister) func(http.ResponseWriter, *http.Request, publicUser) {
	return func(w http.ResponseWriter, r *http.Request, user publicUser) {
		if !checkExpectedUser(w, r, user.ID, false) {
			return
		}
		companyID, valid := companyPathID(r)
		if !valid {
			writeAuthError(w, http.StatusBadRequest, "invalid company ID")
			return
		}
		var userID pgtype.UUID
		if err := userID.Scan(user.ID); err != nil || !userID.Valid || teams == nil {
			writeAuthError(w, http.StatusInternalServerError, "could not list teams")
			return
		}
		rows, err := teams.ListCompanyTeams(r.Context(), db.ListCompanyTeamsParams{
			CompanyID: companyID, UserID: userID,
		})
		if err != nil {
			log.Print("team listing failed")
			writeAuthError(w, http.StatusInternalServerError, "could not list teams")
			return
		}
		if len(rows) == 0 {
			writeAuthError(w, http.StatusNotFound, "company not found")
			return
		}
		result := make([]teamMembership, 0, len(rows))
		for _, row := range rows {
			// An accessible company without visible teams has a null team ID.
			if row.ID.Valid {
				result = append(result, teamMembership{
					ID: row.ID.String(), CompanyID: row.CompanyID.String(), Name: row.Name, Role: row.Role,
				})
			}
		}
		writeAuthJSON(w, http.StatusOK, result)
	}
}

func (c postgresCompanyStore) ListCompanyTeams(ctx context.Context, params db.ListCompanyTeamsParams) ([]db.ListCompanyTeamsRow, error) {
	return db.New(c.database).ListCompanyTeams(ctx, params)
}
