package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Alok-CS-2022/DealForge/backend/internal/db"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

type taskCreator interface {
	CreateDealTask(context.Context, pgtype.UUID, db.CreateTaskParams) (db.DealforgeTask, error)
}

type taskResponse struct {
	ID             pgtype.UUID        `json:"id"`
	DealID         pgtype.UUID        `json:"deal_id"`
	TeamID         pgtype.UUID        `json:"team_id"`
	Title          string             `json:"title"`
	DueDate        pgtype.Date        `json:"due_date"`
	Status         string             `json:"status"`
	CompletedAt    pgtype.Timestamptz `json:"completed_at"`
	AssigneeUserID pgtype.UUID        `json:"assignee_user_id"`
	CreatedAt      pgtype.Timestamptz `json:"created_at"`
	UpdatedAt      pgtype.Timestamptz `json:"updated_at"`
}

func createTask(tasks taskCreator) func(http.ResponseWriter, *http.Request, publicUser) {
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
			Title   string `json:"title"`
			DueDate string `json:"due_date"`
		}
		if !decodeAuthRequest(w, r, &input) {
			writeAuthError(w, http.StatusBadRequest, "invalid task JSON")
			return
		}
		title := strings.TrimSpace(input.Title)
		if title == "" || utf8.RuneCountInString(title) > 100 || strings.ContainsRune(title, 0) {
			writeAuthError(w, http.StatusBadRequest, "title must be 1-100 characters")
			return
		}
		dueDate, err := time.Parse(time.DateOnly, input.DueDate)
		if err != nil || dueDate.Year() < 1 || dueDate.Format(time.DateOnly) != input.DueDate {
			writeAuthError(w, http.StatusBadRequest, "due_date must be a date in YYYY-MM-DD format")
			return
		}

		var userID pgtype.UUID
		if err := userID.Scan(user.ID); err != nil || !userID.Valid || tasks == nil {
			writeAuthError(w, http.StatusInternalServerError, "could not create task")
			return
		}
		task, err := tasks.CreateDealTask(r.Context(), userID, db.CreateTaskParams{
			CompanyID: companyID, TeamID: teamID, DealID: dealID, Title: title,
			DueDate: pgtype.Date{Time: dueDate, Valid: true},
		})
		if errors.Is(err, pgx.ErrNoRows) {
			writeAuthError(w, http.StatusNotFound, "deal not found")
			return
		}
		if err != nil {
			log.Print("task creation failed")
			writeAuthError(w, http.StatusInternalServerError, "could not create task")
			return
		}
		writeAuthJSON(w, http.StatusCreated, taskResponse(task))
	}
}

func (c postgresCompanyStore) CreateDealTask(ctx context.Context, userID pgtype.UUID, params db.CreateTaskParams) (db.DealforgeTask, error) {
	var task db.DealforgeTask
	err := pgx.BeginFunc(ctx, c.database, func(tx pgx.Tx) error {
		queries := db.New(tx)
		if err := lockDealWriteScope(ctx, queries, params.CompanyID, params.TeamID, userID); err != nil {
			return err
		}
		var err error
		task, err = queries.CreateTask(ctx, params)
		if err != nil {
			return err
		}
		return queries.LogTaskCreation(ctx, db.LogTaskCreationParams{
			CompanyID: params.CompanyID, ActorUserID: userID, TaskID: task.ID,
			TeamID: task.TeamID, DealID: task.DealID, Title: task.Title, DueDate: task.DueDate, Status: task.Status,
		})
	})
	if err != nil {
		return db.DealforgeTask{}, err
	}
	return task, nil
}
