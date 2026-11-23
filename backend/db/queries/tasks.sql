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
