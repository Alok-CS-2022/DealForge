package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/Alok-CS-2022/DealForge/backend/internal/db"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
)

var errContactCompanyUnavailable = errors.New("contact company unavailable")

type contactCreator interface {
	CreateCompanyContact(context.Context, pgtype.UUID, db.CreateContactParams) (db.DealforgeContact, error)
}

type contactInput struct {
	Name        string `json:"name"`
	Email       string `json:"email"`
	Phone       string `json:"phone"`
	CompanyName string `json:"company_name"`
}

type contactResponse struct {
	ID             pgtype.UUID        `json:"id"`
	CompanyID      pgtype.UUID        `json:"company_id"`
	Name           string             `json:"name"`
	Email          string             `json:"email"`
	Phone          pgtype.Text        `json:"phone"`
	CompanyName    pgtype.Text        `json:"company_name"`
	AssigneeUserID pgtype.UUID        `json:"assignee_user_id"`
	CreatedAt      pgtype.Timestamptz `json:"created_at"`
	UpdatedAt      pgtype.Timestamptz `json:"updated_at"`
}

func (input *contactInput) normalizeAndValidate() string {
	input.Name = strings.TrimSpace(input.Name)
	input.Email = strings.ToLower(strings.TrimSpace(input.Email))
	input.Phone = strings.TrimSpace(input.Phone)
	input.CompanyName = strings.TrimSpace(input.CompanyName)

	switch {
	case input.Name == "" || utf8.RuneCountInString(input.Name) > 100 || strings.ContainsRune(input.Name, 0):
		return "name must be 1-100 characters"
	case !validEmail(input.Email):
		return "email must be a valid address of at most 254 bytes"
	case utf8.RuneCountInString(input.Phone) > 50 || strings.ContainsRune(input.Phone, 0):
		return "phone must be at most 50 characters"
	case utf8.RuneCountInString(input.CompanyName) > 100 || strings.ContainsRune(input.CompanyName, 0):
		return "company_name must be at most 100 characters"
	default:
		return ""
	}
}

func createContact(contacts contactCreator) func(http.ResponseWriter, *http.Request, publicUser) {
	return func(w http.ResponseWriter, r *http.Request, user publicUser) {
		if !checkExpectedUser(w, r, user.ID, false) {
			return
		}
		companyID, valid := contactCompanyPathID(r)
		if !valid {
			writeAuthError(w, http.StatusBadRequest, "invalid company ID")
			return
		}
		var input contactInput
		if !decodeAuthRequest(w, r, &input) {
			writeAuthError(w, http.StatusBadRequest, "invalid contact JSON")
			return
		}
		if message := input.normalizeAndValidate(); message != "" {
			writeAuthError(w, http.StatusBadRequest, message)
			return
		}
		var userID pgtype.UUID
		if err := userID.Scan(user.ID); err != nil || !userID.Valid || contacts == nil {
			writeAuthError(w, http.StatusInternalServerError, "could not create contact")
			return
		}
		contact, err := contacts.CreateCompanyContact(r.Context(), userID, db.CreateContactParams{
			CompanyID: companyID, Name: input.Name, Email: input.Email,
			Phone:       pgtype.Text{String: input.Phone, Valid: input.Phone != ""},
			CompanyName: pgtype.Text{String: input.CompanyName, Valid: input.CompanyName != ""},
		})
		if err != nil {
			var pgErr *pgconn.PgError
			switch {
			case errors.Is(err, errContactCompanyUnavailable):
				writeAuthError(w, http.StatusNotFound, "company not found")
			case errors.As(err, &pgErr) && pgErr.Code == "23505" && pgErr.ConstraintName == "contacts_company_email_key":
				writeAuthError(w, http.StatusConflict, "contact email already exists in this company")
			default:
				log.Print("contact creation failed")
				writeAuthError(w, http.StatusInternalServerError, "could not create contact")
			}
			return
		}
		writeAuthJSON(w, http.StatusCreated, contactResponse{
			ID: contact.ID, CompanyID: contact.CompanyID, Name: contact.Name, Email: contact.Email,
			Phone: contact.Phone, CompanyName: contact.CompanyName, AssigneeUserID: contact.AssigneeUserID,
			CreatedAt: contact.CreatedAt, UpdatedAt: contact.UpdatedAt,
		})
	}
}

func contactCompanyPathID(r *http.Request) (pgtype.UUID, bool) {
	var id pgtype.UUID
	value := chi.URLParam(r, "companyID")
	valid := len(value) == 36 && id.Scan(value) == nil && strings.EqualFold(id.String(), value)
	return id, valid
}

func (c postgresCompanyStore) CreateCompanyContact(ctx context.Context, userID pgtype.UUID, params db.CreateContactParams) (db.DealforgeContact, error) {
	var contact db.DealforgeContact
	err := pgx.BeginFunc(ctx, c.database, func(tx pgx.Tx) error {
		queries := db.New(tx)
		if _, err := queries.LockContactCompany(ctx, db.LockContactCompanyParams{CompanyID: params.CompanyID, UserID: userID}); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return errContactCompanyUnavailable
			}
			return err
		}
		// Recheck in a separate statement after taking the company lock. The
		// membership lock prevents deletion until this transaction finishes.
		if _, err := queries.LockContactMembership(ctx, db.LockContactMembershipParams{CompanyID: params.CompanyID, UserID: userID}); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return errContactCompanyUnavailable
			}
			return err
		}
		var err error
		contact, err = queries.CreateContact(ctx, params)
		return err
	})
	if err != nil {
		return db.DealforgeContact{}, err
	}
	return contact, nil
}
