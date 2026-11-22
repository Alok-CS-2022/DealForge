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

Chi routes HTTP requests, and pgx provides the PostgreSQL driver for the Go code
that sqlc generates. Vite serves and builds the React UI, TypeScript checks its
types, and Playwright checks that the page renders in Chromium.
