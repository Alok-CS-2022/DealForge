-- name: CreateTask :one
INSERT INTO dealforge.tasks (team_id, deal_id, title, due_date)
SELECT d.team_id, d.id, sqlc.arg(title)::text, sqlc.arg(due_date)::date
FROM dealforge.deals AS d
WHERE d.company_id = sqlc.arg(company_id) AND d.team_id = sqlc.arg(team_id)
  AND d.id = sqlc.arg(deal_id)
RETURNING id, deal_id, team_id, title, due_date, status, completed_at,
          assignee_user_id, created_at, updated_at;

-- name: LogTaskCreation :exec
INSERT INTO dealforge.activity_logs (
    company_id, actor_user_id, action, entity_type, entity_id, changes
)
VALUES (
    sqlc.arg(company_id), sqlc.arg(actor_user_id), 'created', 'task', sqlc.arg(task_id),
    jsonb_build_object(
        'team_id', sqlc.arg(team_id)::uuid,
        'deal_id', sqlc.arg(deal_id)::uuid,
        'title', sqlc.arg(title)::text,
        'due_date', sqlc.arg(due_date)::date,
        'status', sqlc.arg(status)::text
    )
);

-- name: ListDealTasks :many
SELECT task.id, d.id AS deal_id, d.team_id, COALESCE(task.title, '') AS title,
       task.due_date, COALESCE(task.status, '') AS status, task.completed_at,
       task.assignee_user_id, task.created_at, task.updated_at
FROM dealforge.deals AS d
JOIN dealforge.teams AS t ON t.company_id = d.company_id AND t.id = d.team_id
JOIN dealforge.companies AS c ON c.id = t.company_id
JOIN dealforge.company_memberships AS cm
  ON cm.company_id = c.id AND cm.user_id = sqlc.arg(user_id)
JOIN dealforge.team_memberships AS tm
  ON tm.company_id = t.company_id AND tm.team_id = t.id AND tm.user_id = cm.user_id
  AND tm.role IN ('manager', 'salesperson')
LEFT JOIN dealforge.tasks AS task ON task.team_id = d.team_id AND task.deal_id = d.id
WHERE d.company_id = sqlc.arg(company_id) AND d.team_id = sqlc.arg(team_id)
  AND d.id = sqlc.arg(deal_id) AND c.archived_at IS NULL AND t.archived_at IS NULL
ORDER BY task.due_date ASC, task.created_at ASC, task.id ASC;
