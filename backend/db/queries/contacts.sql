-- name: LockContactCompany :one
SELECT c.id
FROM dealforge.companies AS c
WHERE c.id = sqlc.arg(company_id) AND c.archived_at IS NULL
  AND EXISTS (
    SELECT 1
    FROM dealforge.company_memberships AS cm
    WHERE cm.company_id = c.id AND cm.user_id = sqlc.arg(user_id)
  )
FOR UPDATE OF c;

-- name: LockContactMembership :one
SELECT id
FROM dealforge.company_memberships
WHERE company_id = $1 AND user_id = $2
FOR SHARE;

-- name: CreateContact :one
INSERT INTO dealforge.contacts (company_id, name, email, phone, company_name)
VALUES ($1, $2, $3, $4, $5)
RETURNING id, company_id, name, email, phone, company_name, assignee_user_id, created_at, updated_at;
