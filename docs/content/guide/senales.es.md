# Señales de vida

Una vez por semana, cada instalación de rendimiento le dice a rendimiento.ai que sigue viva, con unos cuantos conteos anónimos. Así el proyecto sabe cuántas instalaciones hay, sobre qué corren y si se usan: es el primer número que la gente le pide a un proyecto de código abierto. Esta página dice exactamente qué se manda, qué nunca, y cómo apagarlo.

## Qué se manda {#what-goes}

Un `POST https://rendimiento.ai/api/senales` por semana (unas horas antes o después, para que no lleguen todas juntas), y uno unos diez minutos después de que una instalación arranca por primera vez. Exactamente esto, y nada más:

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

- `instalacion` son 16 bytes al azar, creados la primera vez y guardados en la base de datos de la propia instalación. No salen de nada de la computadora, la cuenta ni el dominio.
- `modo` es `local` (una computadora) o `kubernetes`; `motor` es `containerd`, `docker` o `kubernetes`.
- Los conteos de `semana` son de los últimos siete días; `apps` y `agentes_con_llave` son cuántos hay ahora.
- `personas` es un rango (`1-5`, `6-20`, `21-100`, `100+`), así que nunca se sabe el tamaño exacto de un equipo.

Los operadores ven la próxima señal **tal como saldría** en **Miembros → Lo que rendimiento manda**.

## Qué nunca se manda {#what-never-goes}

Nombres de aplicaciones, repositorios, ramas o dominios; usuarios de GitHub, correos o nombres; secretos, llaves, registros ni nada escrito en la Ruta. El código de rendimiento.ai nunca lee ni guarda la dirección de quien manda una señal (los servidores web que tiene enfrente, Cloudflare y el ingress, registran las peticiones como con cualquier visita). Guarda un renglón por instalación con sus últimos conteos, cuándo se supo de ella por primera y última vez, y cuántas señales llegaron.

## Apagarlas {#turning-them-off}

- **Miembros → Lo que rendimiento manda → Apagar** (los operadores de la plataforma), o **Apagarla** en el aviso que la página de Aplicaciones muestra una vez, la primera.
- `SENALES=off` en el entorno las apaga para siempre, diga lo que diga la página.

## Qué hace rendimiento.ai con ellas {#what-rendimiento-ai-does-with-them}

Cuenta las instalaciones de las que supo en los últimos 7 y 28 días, por modo, motor y versión. Las instalaciones del propio equipo mandan `SENALES_EQUIPO=1` y se cuentan aparte, nunca en los números que se dan a conocer. Cuando más de 25 instalaciones fuera del equipo están vivas, su número aparece en las estadísticas públicas (`/api/public/stats`). Solo una instalación con `SENALES_RECIBIR=1` (rendimiento.ai) recibe señales; cualquier otra responde `404`.
