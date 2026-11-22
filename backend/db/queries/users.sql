-- name: CreateUser :one
INSERT INTO dealforge.users (username, email, display_name, password_hash)
VALUES ($1, $2, $3, $4)
RETURNING id, username, email, display_name;

-- name: GetLoginUser :one
SELECT id, password_hash
FROM dealforge.users
WHERE lower(username) = lower(sqlc.arg(username)::text);
