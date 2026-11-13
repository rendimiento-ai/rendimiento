-- Who enters an app: without a list (access_list false) everyone of its
-- organization enters with their organization's role, as before. With a
-- list, each person listed has their role for this app, people not listed
-- do not see it, and the organization's administrators always enter.
-- Only adds: the previous version ignores both.
ALTER TABLE apps ADD COLUMN IF NOT EXISTS access_list BOOLEAN NOT NULL DEFAULT false;

CREATE TABLE IF NOT EXISTS app_members (
    app_id     BIGINT NOT NULL REFERENCES apps(id) ON DELETE CASCADE,
    login      TEXT NOT NULL,          -- GitHub login, as GitHub writes it
    role       TEXT NOT NULL CHECK (role IN ('guest', 'collaborator', 'admin')),
    added_by   TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX IF NOT EXISTS app_members_login ON app_members (app_id, lower(login));
