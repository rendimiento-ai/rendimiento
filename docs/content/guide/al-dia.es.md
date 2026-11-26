# Poner una aplicación al día (cada actualización de dependencias, leída) {#putting-an-app-up-to-date}

[Renovate](renovate.md) (o Dependabot) abre una solicitud por cada versión nueva de una dependencia. Una pequeña pasa y se integra; una que cambia mucho falla, o pasa mientras se rompe algo que las pruebas no cubren, y espera hasta que alguien la mira. rendimiento lee cada una primero, dice qué le pide a la aplicación y, cuando usted dice que sí, hace que un agente adapte la aplicación a la versión nueva en la misma rama.

## El informe {#the-report}

Cuando termina una ejecución de la solicitud de una actualización de dependencias (una rama `renovate/` o `dependabot/`, o la etiqueta `dependencies`), los agentes escriben su informe con el modelo de la organización (primero el local). Leen el texto de la solicitud, que trae las notas de la versión, y, si la ejecución falló, sus pasos fallidos, el final de sus registros y los archivos que cambiaron. El informe dice:

- **Lo que fallaría**: los cambios incompatibles de las notas que tocan lo que la aplicación probablemente usa, y las fallas de la ejecución.
- **Lo que cambiaría**: los archivos, con cuántas líneas más o menos se agregan y se quitan.
- **Riesgo y costo**: bajo, medio o alto, y lo que costaría ponerla al día con la IA de la plataforma, con el precio que el [estimador](ai.md) le pone a un encargo.

Cuando la ejecución pasó y las notas no le piden nada a la aplicación, dice **Sin cambios**, y la solicitud se puede integrar tal como está. Los informes salen en la pestaña **Caminos** de la aplicación, arriba de sus copias vivas, el último de cada actualización. Una ejecución hecha por el trabajo de una actualización (la de su encargo) no lleva informe nuevo: de ella responde el encargo.

## Ponerla al día {#putting-it-up-to-date}

**Poner la app al día** (para quien puede integrar en la aplicación) abre un encargo sobre la ejecución de la actualización, para que se trabaje en la misma rama, nunca en otra ni en la principal, con el informe en su detalle. Si esa rama ya tiene un encargo abierto (el del diagnosticador, por ejemplo), el informe se le agrega a ese. Luego va a quien eligió la organización:

- **El agente de código de la plataforma** (lo predeterminado): se le manda, como lo hace **Hágalo ahora**. Mientras el agente de código esté apagado (página IA), espera, y la pestaña lo dice.
- **Su agente**: el encargo es para su Claude, en la cola de la Ruta.

El agente adapta la aplicación, la prueba con la ejecución de la rama y la entrega; usted la integra, como siempre (o rendimiento lo hace, en una aplicación que se integra sola). El informe dice entonces qué fue de su encargo: esperando, en trabajo, entregado, listo.

En cuanto el agente hace un commit en la rama, Renovate ve un commit que no es suyo y deja de rebasarla y de actualizarla, como con cualquier rama editada (y con un [arreglo del archivo de bloqueo](renovate.md#lockfiles)): nunca escribe encima del trabajo del agente, y una versión más nueva para esa rama espera hasta que esta se integre o se cierre. Marcar la casilla de rebase de la solicitud le devuelve la rama a Renovate, y borra los cambios del agente.

## Lo que se elige {#the-choices}

En la página IA, la tarjeta **Poner al día** (la cambia un administrador):

| Opción | Qué hace |
|---|---|
| Encendido o apagado | Apagado, no se escriben informes. Encendido de inicio. |
| Quién lo hace | El agente de código de la plataforma (lo predeterminado), o su agente. |
| Cuándo | **Con toda actualización** (lo predeterminado), o **solo cuando su ejecución falla**. «Toda» revisa también las que pasan: una prueba que no existe no puede fallar. |
| Sin preguntar | **Siempre me pregunta** (lo predeterminado), o **sola cuando el riesgo es bajo y cuesta a lo más** un tope (25 centavos al principio, hasta $5): un informe así abre su encargo de inmediato. |

El trabajo es del agente de código, con [sus permisos](ai.md); el informe lee ejecuciones y registros con el *Ver ejecuciones, registros de pasos y solicitudes lentas* del diagnosticador. Ninguno integra nunca.

## En el código {#in-the-code}

| Qué | Dónde |
|---|---|
| Pedir un informe cuando termina una ejecución | `internal/platform/al_dia.go` |
| Escribirlo, y abrir el encargo | `internal/agentes/al_dia.go` |
| Los informes y lo que se elige | `internal/store/al_dia.go`, migración `0071_al_dia.sql` |
| `GET /api/apps/{app}/al-dia`, `POST …/al-dia/{id}/poner`, `GET`/`PUT /api/ia/al-dia` | `internal/api/al_dia.go` |
| Los informes de la pestaña Caminos y la tarjeta de la página IA | `web/src/components/al-dia.tsx` |
