# DealForge architecture

DealForge is a CRM for tracking customers and sales deals.

- **Frontend.** React and TypeScript power the browser interface. The frontend calls the REST API to load and update CRM records. Playwright tests cover workflows in a real browser.
- **Backend.** Go and Chi serve the REST API. The backend validates requests, applies business rules, checks permissions, and manages sign-in with server-side sessions. Go tests cover backend behavior.
- **Database.** PostgreSQL stores CRM records and enforces data integrity with constraints. The backend uses pgx to connect to PostgreSQL and sqlc to generate typed Go code from SQL queries.

## Database invariants

- Contacts belong to a company. Deals and tasks belong to a team. Composite
  foreign keys enforce company and team scope for references and assignments.
  The backend must also scope every read and write to the authorized company
  or team. Archiving sets a timestamp and preserves dependent records.
- Company admins and team managers are distinct roles. The Go service must
  prevent the last company admin and the last team manager from leaving,
  being removed, or being demoted until a replacement is promoted. In a
  read-committed transaction, lock the company row first and any affected team
  rows in ID order, then check remaining role holders in a separate statement.
  All membership and role changes must follow the same locking order. A company
  removal checks every affected team. These checks belong in the Go service.
- The same removal transaction clears the member's deal and task assignments
  before deleting their team membership. Company removal also clears their
  company contact assignments and removes their team memberships before
  deleting the company membership. Foreign keys reject leftover references.
- Resolve invitation usernames case-insensitively to stable user IDs. Check
  the inviter's authority over the target team when creating the invitation.
  Acceptance resolves the invitation and creates any missing memberships in
  one transaction.
- In the same transaction that writes a change and its activity log entry,
  the Go backend must verify that the actor currently belongs to the target
  company and has permission for the operation at the applicable company or
  team scope. Lock the company row before checking membership and permissions,
  following the membership change lock order above, so concurrent removal or
  demotion cannot invalidate the authorization before commit. The log's
  `actor_user_id` references `users.id`, so it survives membership removal.
  The log trigger rejects updates, deletes, and truncation. Provision the
  runtime database role separately from the table owner and grant it only
  SELECT and INSERT on the activity log.
- Generate session tokens from cryptographically random bytes in Go and store
  only their 32-byte SHA-256 digests. The backend checks expiration and
  revocation on every session lookup and maintains `updated_at` on writes.
