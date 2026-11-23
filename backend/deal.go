package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Alok-CS-2022/DealForge/backend/internal/db"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

type dealCreator interface {
	CreateTeamDeal(context.Context, pgtype.UUID, db.CreateDealParams) (db.CreateDealRow, error)
}

type dealInput struct {
	Title             string  `json:"title"`
	ContactID         string  `json:"contact_id"`
	Amount            string  `json:"amount"`
	Currency          string  `json:"currency"`
	Stage             *string `json:"stage"`
	ExpectedCloseDate *string `json:"expected_close_date"`
	LostReason        *string `json:"lost_reason"`
}

type dealResponse struct {
	ID                pgtype.UUID        `json:"id"`
	CompanyID         pgtype.UUID        `json:"company_id"`
	TeamID            pgtype.UUID        `json:"team_id"`
	ContactID         pgtype.UUID        `json:"contact_id"`
	Title             string             `json:"title"`
	Amount            string             `json:"amount"`
	Currency          string             `json:"currency"`
	Stage             string             `json:"stage"`
	ExpectedCloseDate pgtype.Date        `json:"expected_close_date"`
	LostReason        pgtype.Text        `json:"lost_reason"`
	AssigneeUserID    pgtype.UUID        `json:"assignee_user_id"`
	CreatedAt         pgtype.Timestamptz `json:"created_at"`
	UpdatedAt         pgtype.Timestamptz `json:"updated_at"`
}

var dealAmountPattern = regexp.MustCompile(`^[0-9]{1,15}(\.[0-9]{1,4})?$`)
var dealCurrencyPattern = regexp.MustCompile(`^[a-zA-Z]{3}$`)

func (input dealInput) validate() (db.CreateDealParams, string) {
	params := db.CreateDealParams{
		Title: strings.TrimSpace(input.Title), Currency: strings.TrimSpace(input.Currency), Stage: "new",
	}
	if params.Title == "" || utf8.RuneCountInString(params.Title) > 100 || strings.ContainsRune(params.Title, 0) {
		return params, "title must be 1-100 characters"
	}
	contactID, valid := parseResourceID(input.ContactID)
	if !valid {
		return params, "invalid contact ID"
	}
	params.ContactID = contactID

	amount := strings.TrimSpace(input.Amount)
	if !dealAmountPattern.MatchString(amount) || params.Amount.Scan(amount) != nil {
		return params, "amount must be a nonnegative decimal string with up to 15 whole and 4 fractional digits"
	}
	if !dealCurrencyPattern.MatchString(params.Currency) {
		return params, "currency must be a three-letter code"
	}
	params.Currency = strings.ToUpper(params.Currency)
	if input.Stage != nil {
		params.Stage = strings.ToLower(strings.TrimSpace(*input.Stage))
	}
	switch params.Stage {
	case "new", "qualified", "proposal", "won", "lost":
	default:
		return params, "invalid deal stage"
	}
	if input.ExpectedCloseDate != nil {
		date, err := time.Parse(time.DateOnly, *input.ExpectedCloseDate)
		if err != nil || date.Year() < 1 || date.Format(time.DateOnly) != *input.ExpectedCloseDate {
			return params, "expected_close_date must be a date in YYYY-MM-DD format"
		}
		params.ExpectedCloseDate = pgtype.Date{Time: date, Valid: true}
	}
	if input.LostReason != nil {
		if params.Stage != "lost" {
			return params, "lost_reason requires the lost stage"
		}
		reason := strings.TrimSpace(*input.LostReason)
		if reason == "" || utf8.RuneCountInString(reason) > 500 || strings.ContainsRune(reason, 0) {
			return params, "lost_reason must be 1-500 characters"
		}
		params.LostReason = pgtype.Text{String: reason, Valid: true}
	}
	return params, ""
}

func createDeal(deals dealCreator) func(http.ResponseWriter, *http.Request, publicUser) {
	return func(w http.ResponseWriter, r *http.Request, user publicUser) {
		if !checkExpectedUser(w, r, user.ID, true) {
			return
		}
		companyID, validCompany := companyPathID(r)
		teamID, validTeam := parseResourceID(chi.URLParam(r, "teamID"))
		if !validCompany || !validTeam {
			writeAuthError(w, http.StatusBadRequest, "invalid company or team ID")
			return
		}
		var input dealInput
		if !decodeAuthRequest(w, r, &input) {
			writeAuthError(w, http.StatusBadRequest, "invalid deal JSON")
			return
		}
		params, message := input.validate()
		if message != "" {
			writeAuthError(w, http.StatusBadRequest, message)
			return
		}
		var userID pgtype.UUID
		if err := userID.Scan(user.ID); err != nil || !userID.Valid || deals == nil {
			writeAuthError(w, http.StatusInternalServerError, "could not create deal")
			return
		}
		params.CompanyID, params.TeamID = companyID, teamID
		deal, err := deals.CreateTeamDeal(r.Context(), userID, params)
		if errors.Is(err, pgx.ErrNoRows) {
			writeAuthError(w, http.StatusNotFound, "team or contact not found")
			return
		}
		if err != nil {
			log.Print("deal creation failed")
			writeAuthError(w, http.StatusInternalServerError, "could not create deal")
			return
		}
		writeAuthJSON(w, http.StatusCreated, dealResponse(deal))
	}
}

func (c postgresCompanyStore) CreateTeamDeal(ctx context.Context, userID pgtype.UUID, params db.CreateDealParams) (db.CreateDealRow, error) {
	var deal db.CreateDealRow
	err := pgx.BeginFunc(ctx, c.database, func(tx pgx.Tx) error {
		queries := db.New(tx)
		// Lock company, then team. Recheck membership in separate statements after the locks.
		if _, err := queries.LockActiveCompanyForMember(ctx, db.LockActiveCompanyForMemberParams{CompanyID: params.CompanyID, UserID: userID}); err != nil {
			return err
		}
		if _, err := queries.LockActiveDealTeam(ctx, db.LockActiveDealTeamParams{CompanyID: params.CompanyID, ID: params.TeamID}); err != nil {
			return err
		}
		if _, err := queries.LockCompanyMembership(ctx, db.LockCompanyMembershipParams{CompanyID: params.CompanyID, UserID: userID}); err != nil {
			return err
		}
		if _, err := queries.LockDealTeamMembership(ctx, db.LockDealTeamMembershipParams{
			CompanyID: params.CompanyID, TeamID: params.TeamID, UserID: userID,
		}); err != nil {
			return err
		}
		var err error
		deal, err = queries.CreateDeal(ctx, params)
		return err
	})
	if err != nil {
		return db.CreateDealRow{}, err
	}
	return deal, nil
}
