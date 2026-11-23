package main

import (
	"context"
	"log"
	"net/http"

	"github.com/Alok-CS-2022/DealForge/backend/internal/db"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

type dealLister interface {
	ListTeamDeals(context.Context, db.ListTeamDealsParams) ([]db.ListTeamDealsRow, error)
}

func listDeals(deals dealLister) func(http.ResponseWriter, *http.Request, publicUser) {
	return func(w http.ResponseWriter, r *http.Request, user publicUser) {
		if !checkExpectedUser(w, r, user.ID, false) {
			return
		}
		companyID, validCompany := companyPathID(r)
		teamID, validTeam := parseResourceID(chi.URLParam(r, "teamID"))
		if !validCompany || !validTeam {
			writeAuthError(w, http.StatusBadRequest, "invalid company or team ID")
			return
		}
		var userID pgtype.UUID
		if err := userID.Scan(user.ID); err != nil || !userID.Valid || deals == nil {
			writeAuthError(w, http.StatusInternalServerError, "could not list deals")
			return
		}
		rows, err := deals.ListTeamDeals(r.Context(), db.ListTeamDealsParams{
			CompanyID: companyID, TeamID: teamID, UserID: userID,
		})
		if err != nil {
			log.Print("deal listing failed")
			writeAuthError(w, http.StatusInternalServerError, "could not list deals")
			return
		}
		if len(rows) == 0 {
			writeAuthError(w, http.StatusNotFound, "team not found")
			return
		}
		result := make([]dealResponse, 0, len(rows))
		for _, row := range rows {
			// An authorized team without deals has a left-join row with a null deal ID.
			if row.ID.Valid {
				result = append(result, dealResponse(row))
			}
		}
		writeAuthJSON(w, http.StatusOK, result)
	}
}

func (c postgresCompanyStore) ListTeamDeals(ctx context.Context, params db.ListTeamDealsParams) ([]db.ListTeamDealsRow, error) {
	return db.New(c.database).ListTeamDeals(ctx, params)
}
