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

-- name: ListCompanyContacts :many
SELECT ct.id, c.id AS company_id, COALESCE(ct.name, '') AS name,
       COALESCE(ct.email, '') AS email, ct.phone, ct.company_name,
       ct.assignee_user_id, ct.created_at, ct.updated_at
FROM dealforge.companies AS c
JOIN dealforge.company_memberships AS cm
  ON cm.company_id = c.id AND cm.user_id = sqlc.arg(user_id)
LEFT JOIN dealforge.contacts AS ct ON ct.company_id = c.id
WHERE c.id = sqlc.arg(company_id) AND c.archived_at IS NULL
ORDER BY ct.name, ct.id;
