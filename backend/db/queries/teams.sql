-- name: ListCompanyTeams :many
SELECT t.id, c.id AS company_id, COALESCE(t.name, '') AS name,
       COALESCE(tm.role, '') AS role
FROM dealforge.companies AS c
JOIN dealforge.company_memberships AS cm
  ON cm.company_id = c.id AND cm.user_id = sqlc.arg(user_id)
LEFT JOIN (
    dealforge.team_memberships AS tm
    JOIN dealforge.teams AS t
      ON t.company_id = tm.company_id AND t.id = tm.team_id AND t.archived_at IS NULL
) ON tm.company_id = c.id AND tm.user_id = cm.user_id
WHERE c.id = sqlc.arg(company_id) AND c.archived_at IS NULL
ORDER BY t.name, t.id;
