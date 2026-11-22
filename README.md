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

Open the local URL printed by Vite.

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
for the PostgreSQL tools. The API does not load an environment file or connect
to PostgreSQL yet. Let PostgreSQL prompt for your password or use a local
password file outside the repository.

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

Chi routes HTTP requests, and pgx provides the PostgreSQL driver for the Go code
that sqlc generates. Vite serves and builds the React UI, TypeScript checks its
types, and Playwright checks that the page renders in Chromium.
