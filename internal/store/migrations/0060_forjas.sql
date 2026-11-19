-- Forjas (Ruta 102): an app's code can live on GitLab too. apps.forja is
-- where ("github", the default, for every app that existed). A GitLab
-- connection is a row of gitlab_connections: its address (gitlab.com or
-- one's own) and its OAuth application's id; the application's secret,
-- the webhooks' token and the tokens rendimiento acts with are kept where
-- the GitHub App's credentials are (a Kubernetes Secret, or the Caja), not
-- here. forge_identities links the same person's accounts on several
-- forjas, so they are one person in rendimiento. Only adds: the previous
-- version neither reads nor writes them, and its apps stay "github".
ALTER TABLE apps ADD COLUMN IF NOT EXISTS forja TEXT NOT NULL DEFAULT 'github';

CREATE TABLE IF NOT EXISTS gitlab_connections (
    id             BIGSERIAL PRIMARY KEY,
    base_url       TEXT NOT NULL,            -- https://gitlab.com, https://git.example.com
    application_id TEXT NOT NULL,            -- the OAuth application's id (public)
    username       TEXT NOT NULL DEFAULT '', -- the GitLab account rendimiento acts as, once authorized
    added_by       TEXT NOT NULL,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX IF NOT EXISTS gitlab_connections_base ON gitlab_connections (lower(base_url));

CREATE TABLE IF NOT EXISTS forge_identities (
    login       TEXT NOT NULL,          -- the person in rendimiento (their members' login)
    forja       TEXT NOT NULL,          -- gitlab
    base_url    TEXT NOT NULL,          -- the forja's address
    external_id TEXT NOT NULL,          -- the account's id there, which never changes
    username    TEXT NOT NULL,          -- its name there, as last seen
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (forja, base_url, external_id)
);
CREATE INDEX IF NOT EXISTS forge_identities_login ON forge_identities (lower(login));
