-- Comentarios (encargo 32, decision 145): what people write to the team,
-- from an installation (the app's button) or from the public form, kept
-- where señales are received (rendimiento.ai). The AI's class and draft and
-- the person's answer come in later steps; their columns are here so the
-- table is made once. Only adds.
CREATE TABLE IF NOT EXISTS opiniones (
    id           BIGSERIAL PRIMARY KEY,
    origen       TEXT NOT NULL,                 -- app or formulario
    texto        TEXT NOT NULL,
    idioma       TEXT NOT NULL DEFAULT '',
    nombre       TEXT NOT NULL DEFAULT '',      -- optional, from the form
    datos        JSONB NOT NULL DEFAULT '{}',   -- from an installation: version, modo, motor, so
    ficha        TEXT NOT NULL DEFAULT '',      -- sha256 of the installation's token for the answer
    remitente    TEXT NOT NULL DEFAULT '',      -- the sender counted for the limits: a salted, daily hash
    estado       TEXT NOT NULL DEFAULT 'por_revisar', -- por_revisar, respondido or descartado
    clase        TEXT NOT NULL DEFAULT '',      -- ya_existe, nuevo, falla or sin_sentido ('' not yet)
    resumen      TEXT NOT NULL DEFAULT '',
    borrador     TEXT NOT NULL DEFAULT '',
    respuesta    TEXT NOT NULL DEFAULT '',
    respondido_por TEXT NOT NULL DEFAULT '',
    respondido_at  TIMESTAMPTZ,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS opiniones_remitente ON opiniones (remitente, created_at);
CREATE INDEX IF NOT EXISTS opiniones_creadas ON opiniones (created_at);

-- The form's email, apart from the text: only to answer, deleted 30 days
-- after the answer (or when the comment is set aside).
CREATE TABLE IF NOT EXISTS opinion_correos (
    opinion_id BIGINT PRIMARY KEY REFERENCES opiniones(id) ON DELETE CASCADE,
    correo        TEXT NOT NULL
);
