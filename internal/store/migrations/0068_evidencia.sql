-- What was seen of a release when it failed verification (Ruta 184): its
-- new pods' states, the events about them and the last lines they wrote,
-- kept before the rollback removes them (Kubernetes keeps events about an
-- hour). One row per release, JSON (store.ReleaseEvidence), at most 64 KiB,
-- deleted after 30 days. Only adds: the previous version neither reads nor
-- writes it.
CREATE TABLE IF NOT EXISTS release_evidence (
    release_id  BIGINT PRIMARY KEY REFERENCES releases(id) ON DELETE CASCADE,
    captured_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    evidence    JSONB NOT NULL
);

CREATE INDEX IF NOT EXISTS release_evidence_captured ON release_evidence (captured_at);
