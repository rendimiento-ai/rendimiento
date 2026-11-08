# Señales de vida

Once a week, each installation of rendimiento tells rendimiento.ai it is alive, with a few anonymous counts. That is how the project knows how many installations there are, on what, and whether they are used: it is the first number people ask an open-source project for. This page says exactly what goes, what never does, and how to turn it off.

## What goes {#what-goes}

One `POST https://rendimiento.ai/api/senales` a week (a few hours either way, so they don't all come at once), and one about ten minutes after an installation first starts. Exactly this, and nothing else:

```json
{
  "instalacion": "8f3c2a9e1d474b6a9e0c5a7d2f1b3c84",
  "version": "0.1.4",
  "modo": "local",
  "motor": "docker",
  "so": "linux",
  "arquitectura": "amd64",
  "semana": { "apps": 3, "ejecuciones": 41, "versiones": 9, "agentes_con_llave": 2, "llamadas_de_agentes": 318 },
  "organizaciones": 1,
  "personas": "1-5"
}
```

- `instalacion` is 16 random bytes, made the first time and kept in the installation's own database. It comes from nothing about the computer, the account or the domain.
- `modo` is `local` (one computer) or `kubernetes`; `motor` is `containerd`, `docker` or `kubernetes`.
- The counts in `semana` are of the last seven days; `apps` and `agentes_con_llave` are how many there are now.
- `personas` is a range (`1-5`, `6-20`, `21-100`, `100+`), so a team's exact size is never known.

The operators see the next señal **exactly as it would go** under **Members → What rendimiento sends**.

## What never goes {#what-never-goes}

App, repository, branch or domain names; GitHub logins, emails or names; secrets, keys, logs or anything written in the Ruta. rendimiento.ai's code never reads nor keeps the sender's address (the web servers in front of it, Cloudflare and the ingress, log requests as they do for any visit). It keeps one row per installation with its last counts, when it was first and last heard of, and how many señales came.

## Turning them off {#turning-them-off}

- **Members → What rendimiento sends → Turn off** (the platform's operators), or **Turn it off** on the notice the Apps page shows once, the first time.
- `SENALES=off` in the environment turns them off for good, whatever the page says.

## What rendimiento.ai does with them {#what-rendimiento-ai-does-with-them}

It counts installations heard of in the last 7 and 28 days, by mode, engine and version. The team's own installations send `SENALES_EQUIPO=1` and are counted apart, never in the numbers given out. Once more than 25 installations outside the team are alive, their number appears in the public stats (`/api/public/stats`). Only an installation with `SENALES_RECIBIR=1` (rendimiento.ai) receives señales; any other answers `404`.
