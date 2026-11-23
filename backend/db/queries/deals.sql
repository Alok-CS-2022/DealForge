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
