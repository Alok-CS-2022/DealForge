# DealForge architecture

DealForge is a CRM for tracking customers and sales deals.

- **Frontend.** React and TypeScript power the browser interface. The frontend calls the REST API to load and update CRM records. Playwright tests cover workflows in a real browser.
- **Backend.** Go and Chi serve the REST API. The backend validates requests, applies business rules, checks permissions, and manages sign-in with server-side sessions. Go tests cover backend behavior.
- **Database.** PostgreSQL stores CRM records and enforces data integrity with constraints. The backend uses pgx to connect to PostgreSQL and sqlc to generate typed Go code from SQL queries.
