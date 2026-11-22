-- name: CreateSession :exec
INSERT INTO dealforge.sessions (user_id, token_hash, expires_at)
VALUES ($1, $2, $3);

-- name: GetActiveSessionUser :one
SELECT u.id, u.username, u.email, u.display_name
FROM dealforge.sessions AS s
JOIN dealforge.users AS u ON u.id = s.user_id
WHERE s.token_hash = $1
  AND s.expires_at > now()
  AND s.revoked_at IS NULL;
