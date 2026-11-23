package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"strings"

	"github.com/Alok-CS-2022/DealForge/backend/internal/db"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

type dealStageUpdater interface {
	UpdateTeamDealStage(context.Context, pgtype.UUID, db.UpdateDealStageParams) (db.UpdateDealStageRow, error)
}

func updateDealStage(deals dealStageUpdater) func(http.ResponseWriter, *http.Request, publicUser) {
	return func(w http.ResponseWriter, r *http.Request, user publicUser) {
		if !checkExpectedUser(w, r, user.ID, true) {
			return
		}
		companyID, validCompany := companyPathID(r)
		teamID, validTeam := parseResourceID(chi.URLParam(r, "teamID"))
		dealID, validDeal := parseResourceID(chi.URLParam(r, "dealID"))
		if !validCompany || !validTeam || !validDeal {
			writeAuthError(w, http.StatusBadRequest, "invalid company, team or deal ID")
			return
		}
		var input struct {
			Stage      string  `json:"stage"`
			LostReason *string `json:"lost_reason"`
		}
		if !decodeAuthRequest(w, r, &input) {
			writeAuthError(w, http.StatusBadRequest, "invalid deal stage JSON")
			return
		}
		stage := strings.ToLower(strings.TrimSpace(input.Stage))
		if !validDealStage(stage) {
			writeAuthError(w, http.StatusBadRequest, "invalid deal stage")
			return
		}
		reason, message := validateDealLostReason(stage, input.LostReason)
		if message != "" {
			writeAuthError(w, http.StatusBadRequest, message)
			return
		}
		var userID pgtype.UUID
		if err := userID.Scan(user.ID); err != nil || !userID.Valid || deals == nil {
			writeAuthError(w, http.StatusInternalServerError, "could not update deal stage")
			return
		}
		deal, err := deals.UpdateTeamDealStage(r.Context(), userID, db.UpdateDealStageParams{
			CompanyID: companyID, TeamID: teamID, DealID: dealID, Stage: stage, LostReason: reason,
		})
		if errors.Is(err, pgx.ErrNoRows) {
			writeAuthError(w, http.StatusNotFound, "deal not found")
			return
		}
		if err != nil {
			log.Print("deal stage update failed")
			writeAuthError(w, http.StatusInternalServerError, "could not update deal stage")
			return
		}
		writeAuthJSON(w, http.StatusOK, dealResponse(deal))
	}
}

func (c postgresCompanyStore) UpdateTeamDealStage(ctx context.Context, userID pgtype.UUID, params db.UpdateDealStageParams) (db.UpdateDealStageRow, error) {
	var deal db.UpdateDealStageRow
	err := pgx.BeginFunc(ctx, c.database, func(tx pgx.Tx) error {
		queries := db.New(tx)
		if err := lockDealWriteScope(ctx, queries, params.CompanyID, params.TeamID, userID); err != nil {
			return err
		}
		var err error
		deal, err = queries.UpdateDealStage(ctx, params)
		return err
	})
	if err != nil {
		return db.UpdateDealStageRow{}, err
	}
	return deal, nil
}
