-- name: LockActiveDealTeam :one
SELECT id
FROM dealforge.teams
WHERE company_id = $1 AND id = $2 AND archived_at IS NULL
FOR UPDATE;

-- name: LockDealTeamMembership :one
SELECT id
FROM dealforge.team_memberships
WHERE company_id = $1 AND team_id = $2 AND user_id = $3
  AND role IN ('manager', 'salesperson')
FOR SHARE;

-- name: CreateDeal :one
INSERT INTO dealforge.deals (
    company_id, team_id, contact_id, title, amount, currency,
    stage, expected_close_date, lost_reason
)
SELECT c.company_id, sqlc.arg(team_id)::uuid, c.id, sqlc.arg(title)::text,
       sqlc.arg(amount)::numeric, sqlc.arg(currency)::text, sqlc.arg(stage)::text,
       sqlc.narg(expected_close_date)::date, sqlc.narg(lost_reason)::text
FROM dealforge.contacts AS c
WHERE c.company_id = sqlc.arg(company_id) AND c.id = sqlc.arg(contact_id)
FOR KEY SHARE OF c
RETURNING id, company_id, team_id, contact_id, title, amount::text AS amount,
          currency, stage, expected_close_date, lost_reason, assignee_user_id,
          created_at, updated_at;

-- name: ListTeamDeals :many
SELECT d.id, t.company_id, t.id AS team_id, d.contact_id,
       COALESCE(d.title, '') AS title, COALESCE(d.amount::text, '')::text AS amount,
       COALESCE(d.currency, '') AS currency, COALESCE(d.stage, '') AS stage,
       d.expected_close_date, d.lost_reason, d.assignee_user_id, d.created_at, d.updated_at
FROM dealforge.teams AS t
JOIN dealforge.companies AS c ON c.id = t.company_id
JOIN dealforge.company_memberships AS cm
  ON cm.company_id = c.id AND cm.user_id = sqlc.arg(user_id)
JOIN dealforge.team_memberships AS tm
  ON tm.company_id = t.company_id AND tm.team_id = t.id AND tm.user_id = cm.user_id
  AND tm.role IN ('manager', 'salesperson')
LEFT JOIN dealforge.deals AS d ON d.company_id = t.company_id AND d.team_id = t.id
WHERE t.company_id = sqlc.arg(company_id) AND t.id = sqlc.arg(team_id)
  AND c.archived_at IS NULL AND t.archived_at IS NULL
ORDER BY d.created_at DESC, d.id ASC;
