-- Bitbucket Cloud (Ruta 102, step 3): a Bitbucket connection is a row of
-- bitbucket_connections, one per workspace: its slug and its OAuth
-- consumer's key. The consumer's secret, the webhooks' secret and the
-- tokens rendimiento acts with are kept where GitLab's are (a Kubernetes
-- Secret, or the Caja), not here. Its apps say apps.forja = 'bitbucket',
-- and the accounts people link are rows of forge_identities (0060). Only
-- adds: the previous version neither reads nor writes it.
CREATE TABLE IF NOT EXISTS bitbucket_connections (
    id           BIGSERIAL PRIMARY KEY,
    workspace    TEXT NOT NULL,            -- the workspace's slug: bitbucket.org/<workspace>
    consumer_key TEXT NOT NULL,            -- the OAuth consumer's key (public)
    username     TEXT NOT NULL DEFAULT '', -- the Bitbucket account rendimiento acts as, once authorized
    added_by     TEXT NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX IF NOT EXISTS bitbucket_connections_workspace ON bitbucket_connections (lower(workspace));
