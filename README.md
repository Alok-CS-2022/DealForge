# DealForge

DealForge is a CRM application for managing contacts, sales opportunities, and follow-up tasks.

## Tech Stack

- Backend: Go
- Frontend: React + TypeScript
- Database: PostgreSQL
- API: REST
- Authentication: Server-side sessions
- Testing: Go tests and Playwright

## Local development

Use Go 1.27.4 or later in the 1.27 series, Node.js 24 with npm, and PostgreSQL 16
with its command-line tools.
Start the backend and frontend in separate terminals from the repository root.

```powershell
cd backend
go run .
```

Open `http://127.0.0.1:8080/health` to see the API return `{"status":"ok"}`.
The health endpoint checks that the HTTP server is running and needs no database connection.

```powershell
cd frontend
npm ci
npm run dev
```

Open the loopback URL printed by Vite, usually `http://127.0.0.1:5173`.
Browser code should call the API with relative URLs such as
`/api/v1/auth/me`. The Vite development server forwards `/api` requests
to Go on `http://127.0.0.1:8080` and preserves the browser's origin and
host for cookie handling and origin checks. Vite preview and production
builds have no development proxy.

To use session cookies over local HTTP, set the PostgreSQL configuration
described below and `$env:DEALFORGE_INSECURE_DEV_COOKIE = 'true'` in the
**backend** terminal before running `go run .`. Keep database credentials
in the backend environment or a PostgreSQL password file. Use the same
frontend URL throughout the browser session.

## Tests and builds

From `backend/`, run:

```powershell
go test ./...
go vet ./...
go build ./...
```

From `frontend/`, run:

```powershell
npm ci
npx playwright install chromium
npm run test:e2e
npm run build
```

Playwright starts its own Vite server on port 4173 for the smoke test.

## Local database

`backend/.env.example` lists the connection settings. Set them in your shell
for the PostgreSQL tools and the API. Setting `PGDATABASE` enables the API's
database connection. Use a local PostgreSQL password file outside the
repository for the API. The PostgreSQL command-line tools can also prompt
for a password.

Run these commands from `backend/` using a PostgreSQL role with permission to
create a database. If an administrator has already created the database and
granted your role permission to create schemas there, skip `createdb`.

```powershell
$env:PGHOST = '127.0.0.1'
$env:PGPORT = '5432'
$env:PGDATABASE = 'dealforge'
$env:PGUSER = Read-Host 'PostgreSQL role'
createdb --maintenance-db=postgres $env:PGDATABASE
psql -X -v ON_ERROR_STOP=1 --single-transaction -f db/migrations/000001_create_dealforge_schema.sql
psql -X -v ON_ERROR_STOP=1 --single-transaction -f db/migrations/000002_create_crm_tables.up.sql
```

Migration 000001 creates the application schema. Migration 000002 creates the
CRM tables and should be applied once. Its down migration drops those tables and
their data. Use a disposable database to test rollback. Keep migrations
numbered and apply them in order. To validate the SQL and regenerate
the typed pgx query code without a database connection, run this from `backend/`:

```powershell
go run github.com/sqlc-dev/sqlc/cmd/sqlc@v1.31.1 generate
```

With sqlc 1.31.1 installed, the equivalent command is `sqlc generate`.

## User registration

After applying the database migrations, start the backend with its PostgreSQL
environment settings. Send `POST /api/v1/auth/register` with a JSON object
containing `username`, `email`, `display_name`, and `password`. Success returns
HTTP 201 with `id`, `username`, `email`, and `display_name`. Registration creates
only a user account. Usernames and email addresses are trimmed and lowercased
before insertion. Passwords are hashed with Argon2id from Go's `x/crypto`
package using a fresh random salt for each registration.

## User login

Send `POST /api/v1/auth/login` with `Content-Type: application/json` and a
JSON object containing `username` and `password`. Success returns HTTP 204
and a host-only, HttpOnly session cookie with SameSite=Lax and a 24-hour
expiry. The cookie is Secure by default. Only for local HTTP development,
set `DEALFORGE_INSECURE_DEV_COOKIE=true` before starting the backend.
Production must use HTTPS and leave that override unset.

Send `GET /api/v1/auth/me` with the session cookie to retrieve the signed-in
user's public profile. Missing, invalid, expired, and revoked sessions all
return HTTP 401.

Send `POST /api/v1/auth/logout` to revoke the session from the current cookie
and clear that cookie. Logout leaves other sessions active and returns HTTP
204 even if the cookie is missing, invalid, or already revoked. Browser
requests must come from the same origin.

With the local PostgreSQL settings exported and migrations applied, run
`go test -count=1 -run TestLoginPersistsSessionInPostgres ./...` from
`backend/` with `DEALFORGE_LOGIN_INTEGRATION=1` to verify session storage,
validation, and logout. The test rolls back its fixtures.

## Company creation

Send an authenticated `POST /api/v1/companies` with JSON
`{"name":"Example Company"}` and one `X-DealForge-Expected-User-ID`
header containing the user ID returned by `GET /api/v1/auth/me`.
The server uses the validated session to determine the creator.
A missing, empty, repeated, or mismatched header returns HTTP 412
with `{"error":"session identity precondition failed"}` before any
writes. A missing or invalid session returns HTTP 401. After a 412,
the browser reloads the session and company list before allowing a new
submission.

The name is trimmed and must have 1–100
characters. HTTP 201 returns `company` with `id` and `name`, plus `team`
with `id`, `company_id`, and `name`. The initial team is named `General`.
The company, team, and the creator's admin and manager memberships are
created in one transaction.

With the local PostgreSQL settings exported, set
`DEALFORGE_COMPANY_INTEGRATION=1` and run
`go test -count=1 -run TestCreateCompanyInPostgres ./...` from `backend/`.
The test rolls back its fixtures.

## Company list

An authenticated `GET /api/v1/companies` returns an array of
`{"id":"…","name":"Example Company","role":"admin"}` objects. It includes
only companies with a current membership for the signed-in user and
excludes archived companies. Results are ordered by name, then UUID.
If no active memberships match, the response is HTTP 200 with `[]`.
Clients may send the same identity header on this GET. If supplied,
it must match the session user or the server returns HTTP 412 without
listing companies. The browser uses this check to keep company preferences
scoped to the account it is displaying.
The `DEALFORGE_COMPANY_INTEGRATION=1` tests also cover this endpoint.

## Team listing

An authenticated `GET /api/v1/companies/{companyID}/teams` returns a JSON
array of objects with `id`, `company_id`, `name`, and `role`
(`manager` or `salesperson`). Results include only active teams that the
session user belongs to within the active company, ordered by name and then ID.
Company admins also need team membership to see a team. An accessible company
with no visible teams returns 200 with `[]`.

Missing, archived, and inaccessible companies all return 404. A malformed
company ID returns 400, an invalid session returns 401, and database failures
return a generic 500. If supplied, `X-DealForge-Expected-User-ID` must match
the session or the request returns 412.

Run `go test -count=1 -run TestListTeamsInPostgres ./...` from `backend/`
with the local PostgreSQL settings and `DEALFORGE_COMPANY_INTEGRATION=1`.

## Contact creation

Send an authenticated `POST /api/v1/companies/{companyID}/contacts` with
`{"name":"Ada Lovelace","email":"ada@example.test","phone":"+44 20 1234","company_name":"Example Company"}`.
Both admins and members can create contacts in an active company. Names
are trimmed and must contain 1–100 characters. Email is trimmed, lowercased,
and validated as a bare address of at most 254 bytes. Optional `phone`
and `company_name` are trimmed, accept up to 50 and 100 characters
respectively, and are stored as null when blank or omitted.

HTTP 201 returns `id`, `company_id`, `name`, `email`, `phone`,
`company_name`, `assignee_user_id`, `created_at`, and `updated_at`.
New contacts have a null assignee. Invalid input returns 400, invalid
sessions return 401, and blocked cross-origin requests return 403.
Missing, archived, and inaccessible companies all return 404.
A duplicate email within the company returns 409 regardless of case.
Send one `X-DealForge-Expected-User-ID` header with the ID returned by
`GET /api/v1/auth/me`. A missing or mismatched header returns 412 before
any write. The authenticated session still determines company access.

Run `go test -count=1 -run TestCreateContactInPostgres ./...` from
`backend/` with the local PostgreSQL settings and
`DEALFORGE_COMPANY_INTEGRATION=1` to test against PostgreSQL with
transaction-scoped fixtures.

## Contact listing

An authenticated `GET /api/v1/companies/{companyID}/contacts` returns a JSON
array with the same contact fields as creation, ordered by name and then ID.
Members of active companies receive HTTP 200 with `[]` when there are no
contacts. Missing, archived, and inaccessible companies all return 404.
Invalid company IDs return 400 and missing or invalid sessions return 401.
If supplied, `X-DealForge-Expected-User-ID` must match the session or the
request returns 412.

The `DEALFORGE_COMPANY_INTEGRATION=1` tests also cover this endpoint.

## Deal creation

Send an authenticated `POST /api/v1/companies/{companyID}/teams/{teamID}/deals`
with one `X-DealForge-Expected-User-ID` header matching `GET /api/v1/auth/me`.
The session user must belong to the active company and the active team as a
manager or salesperson.

The JSON body requires `title` (1–100 characters), `contact_id` (an existing
contact in the company), `amount` (a nonnegative decimal **string** with up to
15 whole digits and four fractional digits), and `currency` (three ASCII
letters, normalized to uppercase). Optional `stage` defaults to `new` and
accepts `new`, `qualified`, `proposal`, `won`, or `lost`. Optional
`expected_close_date` uses `YYYY-MM-DD`. Optional `lost_reason` accepts
1–500 characters and requires the `lost` stage.

HTTP 201 returns the saved deal with `id`, `company_id`, `team_id`,
`contact_id`, `title`, `amount` (a decimal string with four fractional digits),
`currency`, `stage`, `expected_close_date`, `lost_reason`,
`assignee_user_id` (null on creation), `created_at`, and `updated_at`.
Invalid input returns 400, invalid sessions return 401, cross-origin writes
return 403, and a missing or mismatched account header returns 412. Missing,
archived, or inaccessible companies and teams, and missing or out-of-company
contacts, all return 404. Unexpected database failures return a generic 500.

Run `go test -count=1 -run TestCreateDealInPostgres ./...` from `backend/`
with the local PostgreSQL settings and `DEALFORGE_COMPANY_INTEGRATION=1`.

## Deal listing

An authenticated `GET /api/v1/companies/{companyID}/teams/{teamID}/deals`
returns the same deal fields as creation, ordered by `created_at` descending
and then `id` ascending. Amounts are decimal strings. The session user must
belong to the active company and the active team as a manager or salesperson.
Authorized teams with no deals return 200 with `[]`. Missing, archived, or
inaccessible companies and teams return the same 404. Malformed IDs return
400 and missing or invalid sessions return 401. If supplied,
`X-DealForge-Expected-User-ID` must match the session or the request returns 412.

Run `go test -count=1 -run TestListDealsInPostgres ./...` from `backend/`
with the local PostgreSQL settings and `DEALFORGE_COMPANY_INTEGRATION=1`.

## Deal stage updates

Send an authenticated
`PATCH /api/v1/companies/{companyID}/teams/{teamID}/deals/{dealID}/stage`
with one `X-DealForge-Expected-User-ID` header matching `GET /api/v1/auth/me`.
The session user must belong to the active company and the active team as a
manager or salesperson. Company admins need team membership as well.

The JSON body requires `stage` and optionally accepts `lost_reason`.
Any move among `new`, `qualified`, `proposal`, `won`, and `lost` is allowed,
including reopening a deal or repeating its current stage. Stage names are
trimmed and matched case-insensitively. A supplied loss reason is trimmed,
must contain 1–500 characters, and is accepted only for `lost`. Omitting it
or sending `null` leaves an existing loss reason in place when the deal stays
`lost`. Moving out of `lost` clears the reason. Moving into `lost` without a
reason leaves it unset.

HTTP 200 returns the same deal fields as creation, including the exact
decimal amount string and a refreshed `updated_at`.

Concurrent updates to the same deal are applied one at a time. The last
successfully applied update determines the stage. The account header checks
session identity only. The endpoint has no deal version or `If-Match`
precondition, so a request based on an older view can overwrite a more
recent change. A successful response describes the deal at the time of that
update, and another update may already be waiting to change it again.

Invalid input or malformed IDs return 400, invalid sessions return 401,
cross-origin writes return 403, and a missing or mismatched account header
returns 412. Missing, archived, or inaccessible companies and teams, and
missing or out-of-scope deals, return the same 404. Unexpected database
failures return a generic 500.

Run `go test -count=1 -run TestUpdateDealStage.*InPostgres ./...` from
`backend/` with the local PostgreSQL settings and
`DEALFORGE_COMPANY_INTEGRATION=1`.

## Task creation

Send an authenticated
`POST /api/v1/companies/{companyID}/teams/{teamID}/deals/{dealID}/tasks`
with one `X-DealForge-Expected-User-ID` header matching `GET /api/v1/auth/me`.
The session user must be a manager or salesperson in the active team and
belong to its active company. Company admins need team membership as well.

The JSON body accepts only `title` and `due_date`, both required. The server
trims the title and requires 1–100 characters. The due date must be a valid
calendar date in `YYYY-MM-DD` format. Past dates and all deal stages are allowed.

HTTP 201 returns the stored `id`, `deal_id`, `team_id`, `title`, `due_date`,
`status`, `completed_at`, `assignee_user_id`, `created_at`, and `updated_at`.
New tasks are `pending` with null `completed_at` and `assignee_user_id`.
The task and its append-only activity entry commit together. The entry uses
action `created`, entity type `task`, the task ID, and the authenticated
actor's user ID. Its `changes` object records the saved team ID, deal ID,
title, due date, and status.

Invalid input or malformed IDs return 400, invalid sessions return 401,
cross-origin writes return 403, and a missing or mismatched account header
returns 412. Missing, archived, or inaccessible companies and teams, and
missing or out-of-scope deals, return the same 404. Unexpected failures
return a generic 500.

Run `go test -count=1 -run TestCreateTaskInPostgres ./...` from `backend/`
with the local PostgreSQL settings and `DEALFORGE_COMPANY_INTEGRATION=1`.

Chi routes HTTP requests, and pgx provides the PostgreSQL driver for the Go code
that sqlc generates. Vite serves and builds the React UI, TypeScript checks its
types, and Playwright checks that the page renders in Chromium.
