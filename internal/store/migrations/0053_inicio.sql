-- How each person likes their home page (the Apps page): which sections it
-- shows, and in what order. By login, not by organization: it is how the
-- person reads a home page, the same on their phone and in every
-- organization they belong to (each organization's apps are already its
-- own). Who never chose sees the default an administrator of the platform
-- set (platform_settings, key 'inicio'), or the built-in one. Only adds:
-- the previous version neither reads nor writes it.
CREATE TABLE IF NOT EXISTS inicio_preferencias (
    login      TEXT PRIMARY KEY,
    secciones  JSONB NOT NULL,   -- [{"id": "corriendo", "on": true}, ...], in order
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
