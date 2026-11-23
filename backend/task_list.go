package main

import (
	"context"
	"log"
	"net/http"

	"github.com/Alok-CS-2022/DealForge/backend/internal/db"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

type taskLister interface {
	ListDealTasks(context.Context, db.ListDealTasksParams) ([]db.ListDealTasksRow, error)
}

func listTasks(tasks taskLister) func(http.ResponseWriter, *http.Request, publicUser) {
	return func(w http.ResponseWriter, r *http.Request, user publicUser) {
		if !checkExpectedUser(w, r, user.ID, false) {
			return
		}
		companyID, validCompany := companyPathID(r)
		teamID, validTeam := parseResourceID(chi.URLParam(r, "teamID"))
		dealID, validDeal := parseResourceID(chi.URLParam(r, "dealID"))
		if !validCompany || !validTeam || !validDeal {
			writeAuthError(w, http.StatusBadRequest, "invalid company, team or deal ID")
			return
		}
		var userID pgtype.UUID
		if err := userID.Scan(user.ID); err != nil || !userID.Valid || tasks == nil {
			writeAuthError(w, http.StatusInternalServerError, "could not list tasks")
			return
		}
		rows, err := tasks.ListDealTasks(r.Context(), db.ListDealTasksParams{
			CompanyID: companyID, TeamID: teamID, DealID: dealID, UserID: userID,
		})
		if err != nil {
			log.Print("task listing failed")
			writeAuthError(w, http.StatusInternalServerError, "could not list tasks")
			return
		}
		if len(rows) == 0 {
			writeAuthError(w, http.StatusNotFound, "deal not found")
			return
		}
		result := make([]taskResponse, 0, len(rows))
		for _, row := range rows {
			// An authorized deal without tasks has a left-join row with a null task ID.
			if row.ID.Valid {
				result = append(result, taskResponse(row))
			}
		}
		writeAuthJSON(w, http.StatusOK, result)
	}
}

func (c postgresCompanyStore) ListDealTasks(ctx context.Context, params db.ListDealTasksParams) ([]db.ListDealTasksRow, error) {
	return db.New(c.database).ListDealTasks(ctx, params)
}
