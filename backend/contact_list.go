package main

import (
	"context"
	"log"
	"net/http"

	"github.com/Alok-CS-2022/DealForge/backend/internal/db"
	"github.com/jackc/pgx/v5/pgtype"
)

type contactLister interface {
	ListCompanyContacts(context.Context, db.ListCompanyContactsParams) ([]db.ListCompanyContactsRow, error)
}

func listContacts(contacts contactLister) func(http.ResponseWriter, *http.Request, publicUser) {
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
		if err := userID.Scan(user.ID); err != nil || !userID.Valid || contacts == nil {
			writeAuthError(w, http.StatusInternalServerError, "could not list contacts")
			return
		}
		rows, err := contacts.ListCompanyContacts(r.Context(), db.ListCompanyContactsParams{
			CompanyID: companyID, UserID: userID,
		})
		if err != nil {
			log.Print("contact listing failed")
			writeAuthError(w, http.StatusInternalServerError, "could not list contacts")
			return
		}
		if len(rows) == 0 {
			writeAuthError(w, http.StatusNotFound, "company not found")
			return
		}
		result := make([]contactResponse, 0, len(rows))
		for _, row := range rows {
			// The left join returns a row with no contact ID for an authorized empty company.
			if !row.ID.Valid {
				continue
			}
			result = append(result, contactResponse{
				ID: row.ID, CompanyID: row.CompanyID, Name: row.Name, Email: row.Email,
				Phone: row.Phone, CompanyName: row.CompanyName, AssigneeUserID: row.AssigneeUserID,
				CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
			})
		}
		writeAuthJSON(w, http.StatusOK, result)
	}
}

func (c postgresCompanyStore) ListCompanyContacts(ctx context.Context, params db.ListCompanyContactsParams) ([]db.ListCompanyContactsRow, error) {
	return db.New(c.database).ListCompanyContacts(ctx, params)
}
