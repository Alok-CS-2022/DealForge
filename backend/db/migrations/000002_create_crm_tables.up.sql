CREATE TABLE dealforge.users (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    username text NOT NULL CHECK (username <> '' AND username = btrim(username)),
    email text NOT NULL CHECK (email <> '' AND email = btrim(email)),
    display_name text NOT NULL CHECK (btrim(display_name) <> ''),
    password_hash text NOT NULL CHECK (password_hash <> ''),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX users_username_key ON dealforge.users (lower(username));
CREATE UNIQUE INDEX users_email_key ON dealforge.users (lower(email));

CREATE TABLE dealforge.companies (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    name text NOT NULL CHECK (btrim(name) <> ''),
    archived_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE dealforge.company_memberships (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    company_id uuid NOT NULL REFERENCES dealforge.companies (id),
    user_id uuid NOT NULL REFERENCES dealforge.users (id),
    role text NOT NULL CHECK (role IN ('admin', 'member')),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (company_id, user_id)
);

CREATE INDEX company_memberships_user_idx ON dealforge.company_memberships (user_id);

CREATE TABLE dealforge.teams (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    company_id uuid NOT NULL REFERENCES dealforge.companies (id),
    name text NOT NULL CHECK (name <> '' AND name = btrim(name)),
    archived_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (company_id, id)
);

CREATE UNIQUE INDEX teams_active_name_key
    ON dealforge.teams (company_id, lower(name)) WHERE archived_at IS NULL;

CREATE TABLE dealforge.team_memberships (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    company_id uuid NOT NULL,
    team_id uuid NOT NULL,
    user_id uuid NOT NULL,
    role text NOT NULL CHECK (role IN ('manager', 'salesperson')),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (team_id, user_id),
    FOREIGN KEY (company_id, team_id) REFERENCES dealforge.teams (company_id, id),
    FOREIGN KEY (company_id, user_id) REFERENCES dealforge.company_memberships (company_id, user_id)
);

CREATE INDEX team_memberships_company_user_idx ON dealforge.team_memberships (company_id, user_id);

CREATE TABLE dealforge.contacts (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    company_id uuid NOT NULL REFERENCES dealforge.companies (id),
    name text NOT NULL CHECK (btrim(name) <> ''),
    email text NOT NULL CHECK (email <> '' AND email = btrim(email)),
    phone text,
    company_name text,
    assignee_user_id uuid,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (company_id, id),
    FOREIGN KEY (company_id, assignee_user_id) REFERENCES dealforge.company_memberships (company_id, user_id)
);

CREATE UNIQUE INDEX contacts_company_email_key ON dealforge.contacts (company_id, lower(email));
CREATE INDEX contacts_assignee_idx ON dealforge.contacts (company_id, assignee_user_id);

CREATE TABLE dealforge.deals (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    company_id uuid NOT NULL,
    team_id uuid NOT NULL,
    contact_id uuid NOT NULL,
    title text NOT NULL CHECK (btrim(title) <> ''),
    stage text NOT NULL DEFAULT 'new' CHECK (stage IN ('new', 'qualified', 'proposal', 'won', 'lost')),
    amount numeric(19, 4) NOT NULL CHECK (amount >= 0 AND amount <> 'NaN'::numeric),
    currency text NOT NULL CHECK (currency ~ '^[A-Z]{3}$'),
    expected_close_date date,
    lost_reason text CHECK (lost_reason IS NULL OR stage = 'lost'),
    assignee_user_id uuid,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (team_id, id),
    FOREIGN KEY (company_id, team_id) REFERENCES dealforge.teams (company_id, id),
    FOREIGN KEY (company_id, contact_id) REFERENCES dealforge.contacts (company_id, id),
    FOREIGN KEY (team_id, assignee_user_id) REFERENCES dealforge.team_memberships (team_id, user_id)
);

CREATE INDEX deals_company_team_idx ON dealforge.deals (company_id, team_id);
CREATE INDEX deals_company_contact_idx ON dealforge.deals (company_id, contact_id);
CREATE INDEX deals_team_assignee_idx ON dealforge.deals (team_id, assignee_user_id);

CREATE TABLE dealforge.tasks (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    deal_id uuid NOT NULL,
    team_id uuid NOT NULL,
    title text NOT NULL CHECK (btrim(title) <> ''),
    due_date date NOT NULL,
    status text NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'completed')),
    completed_at timestamptz,
    assignee_user_id uuid,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CHECK ((status = 'completed') = (completed_at IS NOT NULL)),
    FOREIGN KEY (team_id, deal_id) REFERENCES dealforge.deals (team_id, id),
    FOREIGN KEY (team_id, assignee_user_id) REFERENCES dealforge.team_memberships (team_id, user_id)
);

CREATE INDEX tasks_team_deal_idx ON dealforge.tasks (team_id, deal_id);
CREATE INDEX tasks_team_assignee_idx ON dealforge.tasks (team_id, assignee_user_id);
CREATE INDEX tasks_pending_due_idx ON dealforge.tasks (team_id, due_date) WHERE status = 'pending';

CREATE TABLE dealforge.activity_logs (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    company_id uuid NOT NULL REFERENCES dealforge.companies (id),
    actor_user_id uuid NOT NULL REFERENCES dealforge.users (id),
    action text NOT NULL CHECK (btrim(action) <> ''),
    entity_type text NOT NULL CHECK (btrim(entity_type) <> ''),
    entity_id uuid NOT NULL,
    changes jsonb NOT NULL CHECK (jsonb_typeof(changes) = 'object'),
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX activity_logs_company_time_idx ON dealforge.activity_logs (company_id, created_at DESC);
CREATE INDEX activity_logs_actor_idx ON dealforge.activity_logs (actor_user_id);

CREATE FUNCTION dealforge.reject_activity_log_mutation()
RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'activity logs are append-only' USING ERRCODE = '42501';
END;
$$;

CREATE TRIGGER activity_logs_append_only
    BEFORE UPDATE OR DELETE OR TRUNCATE ON dealforge.activity_logs
    FOR EACH STATEMENT EXECUTE FUNCTION dealforge.reject_activity_log_mutation();

CREATE TABLE dealforge.invitations (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    team_id uuid NOT NULL REFERENCES dealforge.teams (id),
    invitee_user_id uuid NOT NULL REFERENCES dealforge.users (id),
    addressed_username text NOT NULL CHECK (btrim(addressed_username) <> ''),
    invited_by_user_id uuid NOT NULL REFERENCES dealforge.users (id),
    status text NOT NULL DEFAULT 'pending'
        CHECK (status IN ('pending', 'accepted', 'declined', 'revoked')),
    resolved_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CHECK ((status = 'pending') = (resolved_at IS NULL))
);

CREATE UNIQUE INDEX invitations_pending_team_user_key
    ON dealforge.invitations (team_id, invitee_user_id) WHERE status = 'pending';
CREATE INDEX invitations_team_idx ON dealforge.invitations (team_id);
CREATE INDEX invitations_invitee_idx ON dealforge.invitations (invitee_user_id);
CREATE INDEX invitations_inviter_idx ON dealforge.invitations (invited_by_user_id);

CREATE TABLE dealforge.sessions (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id uuid NOT NULL REFERENCES dealforge.users (id),
    token_hash bytea NOT NULL UNIQUE CHECK (octet_length(token_hash) = 32),
    expires_at timestamptz NOT NULL,
    revoked_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CHECK (expires_at > created_at)
);

CREATE INDEX sessions_user_idx ON dealforge.sessions (user_id);
CREATE INDEX sessions_expires_idx ON dealforge.sessions (expires_at);
