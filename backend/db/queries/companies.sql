-- name: CreateCompany :one
INSERT INTO dealforge.companies (name)
VALUES ($1)
RETURNING id, name;

-- name: AddCompanyAdmin :exec
INSERT INTO dealforge.company_memberships (company_id, user_id, role)
VALUES ($1, $2, 'admin');

-- name: CreateTeam :one
INSERT INTO dealforge.teams (company_id, name)
VALUES ($1, $2)
RETURNING id, company_id, name;

-- name: AddTeamManager :exec
INSERT INTO dealforge.team_memberships (company_id, team_id, user_id, role)
VALUES ($1, $2, $3, 'manager');
