-- The GitHub installations an organization other than the platform's may
-- create apps from: an operator gives one to it, or its administrator
-- connects the one on their own GitHub account. The platform's
-- organization uses every installation the App allows, and has no rows
-- here. Only adds.
CREATE TABLE IF NOT EXISTS org_installations (
    org_id          BIGINT NOT NULL REFERENCES orgs(id),
    installation_id BIGINT NOT NULL,
    account         TEXT NOT NULL,          -- the GitHub account it is on, as GitHub writes it
    added_by        TEXT NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (org_id, installation_id)
);
