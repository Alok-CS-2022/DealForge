package main

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/mail"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/Alok-CS-2022/DealForge/backend/internal/db"
	"github.com/jackc/pgx/v5/pgconn"
	"golang.org/x/crypto/argon2"
)

type userCreator interface {
	CreateUser(context.Context, db.CreateUserParams) (db.CreateUserRow, error)
}

type registrationInput struct {
	Username    string `json:"username"`
	Email       string `json:"email"`
	DisplayName string `json:"display_name"`
	Password    string `json:"password"`
}

type registeredUser struct {
	ID          string `json:"id"`
	Username    string `json:"username"`
	Email       string `json:"email"`
	DisplayName string `json:"display_name"`
}

var usernamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_.-]{2,31}$`)

func (input *registrationInput) normalizeAndValidate() string {
	input.Username = strings.ToLower(strings.TrimSpace(input.Username))
	input.Email = strings.ToLower(strings.TrimSpace(input.Email))
	input.DisplayName = strings.TrimSpace(input.DisplayName)

	if !usernamePattern.MatchString(input.Username) {
		return "username must be 3-32 characters, start with a letter or digit, and contain only letters, digits, dots, underscores, or hyphens"
	}
	address, err := mail.ParseAddress(input.Email)
	if err != nil || len(input.Email) > 254 || address.Address != input.Email {
		return "email must be a valid address of at most 254 bytes"
	}
	if input.DisplayName == "" || utf8.RuneCountInString(input.DisplayName) > 100 || strings.ContainsRune(input.DisplayName, 0) {
		return "display_name must be 1-100 characters"
	}
	if utf8.RuneCountInString(input.Password) < 15 || len(input.Password) > 256 || strings.TrimSpace(input.Password) == "" {
		return "password must contain at least 15 characters and at most 256 bytes"
	}
	return ""
}

func registerUser(users userCreator) http.HandlerFunc {
	// Bound memory use from simultaneous Argon2id calls.
	hashSlots := make(chan struct{}, 2)
	return func(w http.ResponseWriter, r *http.Request) {
		var input registrationInput
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&input); err != nil || decoder.Decode(new(json.RawMessage)) != io.EOF {
			writeRegistrationError(w, http.StatusBadRequest, "invalid registration JSON")
			return
		}
		if message := input.normalizeAndValidate(); message != "" {
			writeRegistrationError(w, http.StatusBadRequest, message)
			return
		}
		if users == nil {
			writeRegistrationError(w, http.StatusInternalServerError, "could not register user")
			return
		}
		select {
		case hashSlots <- struct{}{}:
		default:
			writeRegistrationError(w, http.StatusServiceUnavailable, "registration is busy, try again")
			return
		}
		hash, err := hashPassword(input.Password)
		<-hashSlots
		if err != nil {
			log.Print("registration password hashing failed")
			writeRegistrationError(w, http.StatusInternalServerError, "could not register user")
			return
		}

		user, err := users.CreateUser(r.Context(), db.CreateUserParams{
			Username:     input.Username,
			Email:        input.Email,
			DisplayName:  input.DisplayName,
			PasswordHash: hash,
		})
		if err != nil {
			var pgErr *pgconn.PgError
			if errors.As(err, &pgErr) && pgErr.Code == "23505" &&
				(pgErr.ConstraintName == "users_username_key" || pgErr.ConstraintName == "users_email_key") {
				writeRegistrationError(w, http.StatusConflict, "username or email already registered")
				return
			}
			log.Print("registration user insert failed")
			writeRegistrationError(w, http.StatusInternalServerError, "could not register user")
			return
		}

		writeRegistrationJSON(w, http.StatusCreated, registeredUser{
			ID: user.ID.String(), Username: user.Username, Email: user.Email, DisplayName: user.DisplayName,
		})
	}
}

func hashPassword(password string) (string, error) {
	const (
		memory      = 64 * 1024
		iterations  = 3
		parallelism = 4
	)
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	hash := argon2.IDKey([]byte(password), salt, iterations, memory, parallelism, 32)
	encoding := base64.RawStdEncoding
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s", argon2.Version, memory, iterations, parallelism,
		encoding.EncodeToString(salt), encoding.EncodeToString(hash)), nil
}

func writeRegistrationError(w http.ResponseWriter, status int, message string) {
	writeRegistrationJSON(w, status, map[string]string{"error": message})
}

func writeRegistrationJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(body); err != nil {
		log.Print("write registration response failed")
	}
}
