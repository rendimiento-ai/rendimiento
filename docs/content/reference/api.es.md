# API HTTP

Todas las rutas se registran en `Server.Handler` (`internal/api/server.go`). La interfaz usa esta API y nada más, así que todo lo que hace la interfaz se puede automatizar con guiones.

- **Autenticación:** una galleta de sesión que pone el inicio de sesión con GitHub (`/api/auth/login`). Las rutas marcadas con 🔒 devuelven `401` sin ella o para quien no es [miembro](../guide/members.md), y `403` cuando la función de la persona es menor que la que pide la ruta.
- **Formato:** JSON de entrada y de salida. Los errores son `{"error": "mensaje"}` con el código de estado correspondiente.
- **Actualizaciones en vivo:** las rutas `…/events` son flujos de **eventos enviados por el servidor** (`text/event-stream`). La entrada desactiva el búfer para ellas.
- **Idioma:** con `Accept-Language: es`, los mensajes de la plataforma llegan en español de México.

## Públicas {#public}

| Método | Ruta | Hace |
|---|---|---|
| GET | `/healthz` | `ok`; las sondas de disponibilidad y de vida |
| GET | `/llms.txt` | para agentes de IA: qué es la plataforma, cómo conectarse por MCP, las funciones y las reglas ([Hecha para agentes](../guide/agents.md)) |
| GET | `/api/public/stats` | números agregados para una página pública; solo con `PUBLIC_STATS=true`, y solo en el puerto interno `STATS_LISTEN` cuando está definido ([qué contiene](../guide/reliability.md#public-stats)) |
| GET | `/api/public/activity` | el *Ahora mismo en el pueblo* de la página de bienvenida: las ejecuciones en cola o en curso y las 8 versiones más recientes, solo con el nombre de la aplicación y la hora (sin repositorios, confirmaciones, ramas ni mensajes); solo con `PUBLIC_ACTIVITY=true` |
| POST | `/api/webhooks/github` | los avisos web de la aplicación de GitHub (envío, solicitud de incorporación, instalación), verificados con HMAC |
| POST | `/api/webhooks/gitlab` | los webhooks de los proyectos de GitLab (push, merge request), comprobados por su `X-Gitlab-Token` ([Conectar GitLab](../guide/gitlab.md)) |
| GET | `/api/auth/gitlab/{id}` | iniciar sesión con un GitLab conectado |
| GET | `/api/forjas/gitlab/callback` | GitLab regresa aquí a las personas: al conectar, al iniciar sesión o al ligar una cuenta |
| POST | `/api/webhooks/bitbucket` | los webhooks de los repositorios de Bitbucket (push, pull request), verificados con HMAC-SHA256 en `X-Hub-Signature` ([Conectar Bitbucket](../guide/bitbucket.md)) |
| GET | `/api/auth/bitbucket/{id}` | iniciar sesión con un espacio de trabajo de Bitbucket conectado |
| GET | `/api/forjas/bitbucket/callback` | Bitbucket regresa aquí a las personas (la URL de retorno del consumidor OAuth): al conectar, al iniciar sesión o al ligar una cuenta |
| GET | `/api/opiniones` | los comentarios recibidos, del más nuevo al más viejo, sin correos (operadores, donde se reciben comentarios) |
| GET | `/api/opiniones/cuenta` | cuántos comentarios esperan respuesta: `{porRevisar}` |
| GET | `/api/opiniones/{id}` | un comentario, con su correo hasta que se borra, y su respuesta |
| PUT | `/api/opiniones/{id}` | cambia su clase, lo descarta o lo regresa a revisar, su grupo (`0`: ninguno) o su borrador |
| POST | `/api/opiniones/{id}/reclasificar` | pide a la IA ordenarlo y redactarlo de nuevo |
| POST | `/api/opiniones/{id}/respuesta` | envía la respuesta `{respuesta}`, una sola vez, con su nombre: por correo a quien escribió en el formulario; guardada para una instalación |
| GET | `/api/senales/comentarios/{id}/respuesta` | la respuesta al comentario `id`, solo para la instalación cuya ficha (encabezado `X-Ficha`) da la huella que envió: 204 mientras no hay |
| GET | `/api/al-equipo` | escribirle al equipo desde esta instalación: si pueden salir comentarios (señales encendidas), exactamente lo que saldría, y sus comentarios con sus respuestas (las marca como vistas); antes le pregunta a rendimiento.ai por las respuestas que faltan, a lo más una vez por minuto y por no más de 3 segundos |
| POST | `/api/al-equipo` | envía un comentario `{texto}` al equipo, por el camino de las señales |
| GET | `/api/al-equipo/sin-ver` | cuántas respuestas no ha visto: `{sinVer}` |
| POST | `/api/senales/comentarios` | un comentario para el equipo, donde se reciben señales: `{texto, idioma, correo, nombre}` desde el [formulario público](../future/comentarios.md) o `{texto, idioma, datos, ficha}` desde una instalación; hasta 2,000 caracteres, 3 por hora por remitente y 200 al día en total (429 al pasarse) |
| GET | `/api/auth/login` | redirige al OAuth de GitHub |
| GET | `/api/auth/callback` | la respuesta de OAuth; crea la sesión |
| POST | `/api/auth/logout` | termina la sesión |
| GET | `/api/setup/status` | si la plataforma está instalada (`configured`; si no, `install`, el estado de la [página de instalación](../environment/deploy.md#installing)), si *Iniciar sesión con GitHub* lleva a algún lado (`github`), y los GitLab y espacios de trabajo de Bitbucket con los que se puede iniciar sesión (`signIn`) |
| POST | `/api/setup/token` | `{token}`: instalar, paso 1; la ficha de instalación, comparada en tiempo constante, abre una sesión de instalación (una cookie firmada). `404` una vez instalada |
| POST | `/api/setup/gitlab` | instalar, paso 2 con un GitLab `{baseUrl, applicationId, applicationSecret}`; necesita la sesión de instalación; responde dónde autorizarlo |
| POST | `/api/setup/bitbucket` | instalar, paso 2 con un espacio de trabajo de Bitbucket `{workspace, consumerKey, consumerSecret}`; necesita la sesión de instalación |
| GET | `/api/setup/github` | inicia el flujo de manifiesto de la aplicación (la sesión de instalación, `?token=SETUP_TOKEN`, o la sesión de un administrador de la plataforma) |
| POST | `/mcp` | la puerta MCP para agentes ([la Ruta](../guide/ruta.md)); necesita una llave de agente como token portador, no una sesión |
| GET | `/api/setup/github/callback` | GitHub devuelve las credenciales de la aplicación nueva; se guardan en un Secret |

## Cuenta y entorno 🔒 {#account-and-environment}

| Método | Ruta | Hace |
|---|---|---|
| GET | `/api/inicio` | la página de inicio de la persona con la sesión: sus secciones en orden, cada una `{id, on}` (`secciones`), si la eligió (`propias`), la predeterminada (`predeterminado`, y `predeterminadoPor` cuando la fijó un administrador), y si puede fijarla (`puedeFijar`) |
| PUT | `/api/inicio` | guarda la página de inicio propia, `{"secciones": [{"id": "apps", "on": true}, …]}` (cualquier función; no en una demostración) |
| DELETE | `/api/inicio` | *Volver a lo de siempre*: olvida la página de inicio propia |
| PUT | `/api/inicio/predeterminado` | la página de inicio de quienes nunca eligieron la suya (los administradores de la plataforma) |
| DELETE | `/api/inicio/predeterminado` | vuelve a la predeterminada de fábrica (los administradores de la plataforma) |
| GET | `/api/me` | la cuenta con la sesión iniciada, y qué rendimiento contesta: `version`, `modo` (`local` o `kubernetes`), `os`, `arch` |
| GET | `/api/installations` | las instalaciones de la aplicación de GitHub |
| GET | `/api/installations/{id}/repos` | los repositorios de una instalación |
| POST | `/api/installations/{id}/connect` | conectar a su organización la aplicación de GitHub de su propia cuenta de GitHub ([Cuentas de GitHub](../guide/members.md#github-accounts)) |
| DELETE | `/api/installations/{id}` | que su organización deje de crear aplicaciones desde una instalación |
| GET | `/api/forjas` | dónde vive el código de las aplicaciones: GitHub, los GitLab y los espacios de trabajo de Bitbucket conectados (operadores) |
| POST | `/api/forjas/gitlab` | conectar un GitLab `{baseUrl, applicationId, applicationSecret}`; contesta dónde autorizarlo |
| POST | `/api/forjas/gitlab/{id}/authorize` | autorizar de nuevo una conexión con GitLab |
| DELETE | `/api/forjas/gitlab/{id}` | olvidar una conexión con GitLab que ninguna aplicación usa; `409` cuando es la última forja con la que entra un administrador |
| GET | `/api/forjas/gitlab/{id}/acceso` | quién perdería el acceso sin ella (`members`, y entre ellos `admins`) |
| GET | `/api/forjas/gitlab/{id}/projects` | los proyectos de una conexión con GitLab, para Nueva aplicación |
| POST | `/api/forjas/bitbucket` | conectar un espacio de trabajo de Bitbucket `{workspace, consumerKey, consumerSecret}`; contesta dónde autorizarlo |
| POST | `/api/forjas/bitbucket/{id}/authorize` | autorizar de nuevo una conexión con Bitbucket |
| DELETE | `/api/forjas/bitbucket/{id}` | olvidar una conexión con Bitbucket que ninguna aplicación usa; `409` cuando es la última forja con la que entra un administrador |
| GET | `/api/forjas/bitbucket/{id}/acceso` | quién perdería el acceso sin ella (`members`, y entre ellos `admins`) |
| GET | `/api/forjas/bitbucket/{id}/repos` | los repositorios de un espacio de trabajo de Bitbucket, para Nueva aplicación |
| GET | `/api/me/identities` | sus cuentas en otras forjas, ligadas a usted |
| POST | `/api/me/identities/gitlab/{id}` | ligar su cuenta de un GitLab; contesta dónde autorizarla |
| POST | `/api/me/identities/bitbucket/{id}` | ligar su cuenta de Bitbucket; contesta dónde autorizarla |
| DELETE | `/api/me/identities` | desligar una de sus cuentas (`?forja&baseUrl&externalId`) |
| GET | `/api/org-installations` | operadores: todas las instalaciones y las organizaciones a las que se dieron |
| PUT | `/api/org-installations/{id}` | operadores: darle una instalación a una organización: `{org}` |
| DELETE | `/api/org-installations/{id}?org=` | operadores: quitársela |
| GET | `/api/zones` | las zonas DNS que el token puede editar |
| GET | `/api/environment` | las comprobaciones de la página Entorno |
| GET | `/api/platform/version` | qué versión de rendimiento se ejecuta y si hay una más reciente lista (`SELF_APP`) |
| POST | `/api/platform/update` | actualiza rendimiento a la imagen de una versión: `{release}`; se reinicia unos segundos después |
| GET | `/api/services` | el catálogo de Servicios (aplicaciones, complementos, servicios del clúster, datos de conexión) |
| GET | `/api/universe` | todas las aplicaciones desde arriba: barrios, sistemas, nodos (con sus últimas revisiones de disponibilidad) y las conexiones entre ellos, con llamadas por minuto, errores y latencia (con sus tendencias) de las trazas en `?window=` (`15m`, `1h`, `24h`); `?refresh=1` reconstruye antes el catálogo |
| GET | `/api/universe/traces` | las solicitudes que guardó el servicio de trazas, de la más nueva a la más vieja; `?service=aplicación/servicio`, `?errors=1` para las que fallaron, `?before=` (RFC 3339) para ir hacia atrás |
| GET | `/api/universe/traces/{id}` | una solicitud guardada con sus tramos, para la cascada |
| GET | `/api/universe/spans` | lo que hace un servicio observado, según sus trazas: rutas que atiende, sus propios momentos, llamadas que hace, con llamadas por minuto, errores, latencia y tendencias; `?service=aplicación/servicio`, `?window=` |
| GET | `/api/stats/visits` | el panel de visitantes del inicio: los últimos 7 días de cada aplicación que Umami cuenta, y los 7 anteriores ([Visitas](../guide/visits.md)) |
| GET | `/api/stats/delivery` | las estadísticas de entregas del panel: los últimos 30 días, los 30 anteriores y doce puntos semanales |
| GET | `/api/problems` | las advertencias y errores de la propia plataforma, agrupados ([Problemas](../guide/problems.md)) |
| GET | `/api/problems/count` | cuántos problemas están abiertos (la insignia de la barra superior) |
| POST | `/api/problems/{id}/dismiss` | descartar un problema |
| GET | `/api/registro` | el registro de la plataforma, lo más reciente primero, para sus administradores ([El registro](../guide/registro.md)); `?proceso=`, `?app=`, `?nivel=WARN` o `ERROR` (ese nivel y los peores), `?desde=` y `?hasta=` (RFC 3339), `?q=` (en el mensaje), `?antes=` (el cursor `next` de la página anterior), `?limite=` (hasta 200) |
| POST | `/api/fallas` | la interfaz informa una página de error que mostró (no existe, sin acceso, falló, sin respuesta) a [Problemas](../guide/problems.md#pages-that-could-not-be-shown); no se guarda quién la vio |
| GET | `/api/ruta?kind=&q=` | las entradas de [la Ruta](../guide/ruta.md) |
| POST | `/api/ruta` | agrega una entrada (la escribe la persona con la sesión) |
| PUT | `/api/ruta/{id}` | cambia una entrada (se conserva la versión anterior) |
| POST | `/api/ruta/{id}/archive` | esconde una entrada de las listas |
| GET | `/api/ruta/activity` | los cargos de los agentes y sus últimas llamadas a herramientas |
| GET | `/api/ruta/portada` | la portada de la Ruta para usted: encargos entregados por revisar, entradas con la etiqueta `por-decidir`, pendientes con `para-usted` o con su usuario, fallas conocidas sin confirmar, comentarios que nadie respondió, encargos que nadie tiene, las cuentas del día (`?hoy=`: cuándo empezó su día) y las últimas entradas |
| GET | `/api/agent-keys` | las llaves de agente (nunca los tokens) |
| POST | `/api/agent-keys` | crea una llave de agente (`name`, `role`: `reader` o `collaborator`, `apps`: vacío para todas, `days`); el token solo viene en esta respuesta |
| DELETE | `/api/agent-keys/{id}` | revoca una llave de agente |
| GET | `/api/orgs` | las organizaciones a las que pertenece la persona, con su función |
| POST | `/api/orgs` | crea una organización (`slug`, `name`); solo los operadores de la plataforma; quien la crea es su primer administrador |
| POST | `/api/orgs/{id}/switch` | trabajar en otra de las organizaciones de la persona |
| GET | `/api/ia` | la IA de la organización: proveedor, modelo, tope, el final de su propia llave, el gasto del mes y las últimas llamadas |
| PUT | `/api/ia` | cambiarla (`provider`: `local`, `own`, `platform`; `kind`, `baseUrl`, `model`, `capDollars`, `key`, `removeKey`, `autoDiagnose`, `escalateTo`: `own`, `platform` o vacío, `escalateLimit`, `encargoAutoDollars`: hasta cuánto puede costar, según la estimación, un encargo para que lo tome la IA de la plataforma, `codeAgent`: `off`, `asked` o `auto`, `autoEncargo`: `off`, `branches` o `all`); administradores |
| POST | `/api/ia/test` | hacerle una pregunta al modelo de la organización (`prompt`), medida; colaboradores |
| GET | `/api/ia/orgs` | la asignación de cada organización de la IA de la plataforma, su gasto y la cuenta de la plataforma; operadores |
| PUT | `/api/ia/orgs/{id}/allowance` | la asignación mensual de una organización de la IA de la plataforma (`dollars`); operadores |
| GET | `/api/ia/platica` | lo que puede gastar una plática sobre una entrada en modelos de pago antes de preguntar (`capMicros`) |
| PUT | `/api/ia/platica` | cambiarlo (`dollars`, de $0.05 a $20); administradores |
| GET | `/api/ruta/{id}/platica` | la plática sobre una entrada ([platicar una entrada](../guide/ruta.md#talking-an-entry-over)): sus mensajes firmados, la idea hasta ahora, las IA que puede escoger (`ias`, con lo que cuesta un mensaje) y la que responde (`ia`), lo que lleva gastado y su tope (`alTope`: alcanzado); nunca lo vivido |
| POST | `/api/ruta/{id}/platica` | escribir en ella (`texto`); la IA escogida responde en poco tiempo (su propio agente, por MCP). Pasado el tope responde 402, y `continuar: true` permite un tope más; colaboradores |
| PUT | `/api/ruta/{id}/platica/ia` | quién la responde de ahora en adelante (`ia`: `local`, `own`, `platform` o `agente`); colaboradores |
| PUT | `/api/ruta/{id}/platica/idea` | corregir la idea hasta ahora (`idea`); colaboradores |
| POST | `/api/ruta/{id}/platica/propuesta` | la propuesta de encargos de la IA a partir de ella, con el precio del estimador (`encargos`, `decidido`, `precios` para cada dificultad); colaboradores |
| POST | `/api/ruta/{id}/platica/encargos` | crear los encargos que se quedaron (`encargos`: `titulo`, `app`, `dificultad`, `detalle`, `ruta`: `platform` o `local`, `despuesDe`), guardar la decisión (`decision`: `guardar`, `texto`) y mandar los de la IA de la plataforma al agente de código (`mandar`); colaboradores |
| GET | `/api/members` | las personas de la organización y sus funciones |
| POST | `/api/runs/{id}/diagnosis/feedback` | calificar la explicación de una ejecución (`useful`, o `cause` cuando no sirvió): se vuelve una falla conocida en la Ruta que lee el diagnosticador; colaboradores |
| PUT | `/api/members/{login}` | agrega a una persona por su cuenta de GitHub, o cambia su función (`role`: `guest`, `collaborator`, `admin`); solo administradores |
| DELETE | `/api/members/{login}` | quita a una persona; el último administrador se queda |
| GET | `/api/encargos` | los encargos abiertos (`?all=1`: también los revisados y descartados) |
| POST | `/api/encargos` | pedirles algo a los agentes (`title`, `body`, `app`, `runId`) |
| GET | `/api/encargos/{id}` | un encargo con su historia |
| POST | `/api/encargos/{id}/review` | aceptar un encargo entregado (`accept: true`) o devolverlo con un `comment` |
| POST | `/api/encargos/{id}/cancel` | descartar un encargo (`reason` opcional) |
| POST | `/api/encargos/{id}/app` | de qué aplicación es un encargo pendiente que nadie tiene (`app`); colaboradores de esa aplicación |
| POST | `/api/encargos/{id}/stop` | pedirle al agente de código que se detenga en el encargo que hace: lo devuelve con lo que hizo |
| GET | `/api/encargos/cola` | la fila del agente de código: qué hace ahora, lo que sigue en orden, lo terminado por revisar, lo devuelto; su modo, cuántos a la vez y lo que gastó hoy |
| POST | `/api/encargos/cola/orden` | el orden de la fila (`ids`, el primero se hace primero); los demás van después |
| GET | `/api/comentarios` | los comentarios sobre el trabajo de los agentes, del más viejo al más nuevo: los de un cargo (`?cargo=`), los de un encargo (`?encargo=`) o los últimos de la organización; `new` marca la respuesta de un agente a uno suyo que usted no ha visto |
| POST | `/api/comentarios` | comentar un cargo (`cargoId`) o un encargo (`encargoId`) sin devolverlo (`body`); colaboradores |
| GET | `/api/comentarios/respuestas` | las respuestas de los agentes a sus comentarios que usted no ha visto (la campana) |
| POST | `/api/comentarios/vistos` | marcar como vistas respuestas a sus comentarios (`ids`) |
| PUT | `/api/apps/{app}/auto-encargo` | cuándo se abre solo un encargo por las fallas explicadas de la aplicación (`mode`: `off`, `branches`, `all`, o vacío para seguir a la organización); administradores |
| POST | `/api/encargos/do` | mandar encargos pendientes al agente de código (`ids`); debe estar encendido |
| POST | `/api/encargos/{id}/estimate` | volver a calificar un encargo (el estimador lo hace en unos 30 segundos) |
| POST | `/api/encargos/{id}/route` | quién hace un encargo pendiente: `route` `platform` (la IA de la plataforma) o `local` (su propio agente) |
| POST | `/api/notifications/test` | mandar ahora un correo de aviso de muestra (el botón de la página Entorno) |
| POST | `/api/dns/sync` | volver a crear ahora los registros DNS de cada aplicación |
| POST | `/api/propose` | detectar un repositorio y devolver un `rendimiento.yaml` + Dockerfile propuestos |
| GET | `/api/namespaces/{name}/migration` | lo que ya existe en un espacio de nombres (para adoptar cargas de trabajo existentes) |

## Aplicaciones 🔒 {#apps}

| Método | Ruta | Hace |
|---|---|---|
| GET | `/api/apps` | enumera las aplicaciones con su estado |
| POST | `/api/apps` | incorpora un repositorio (abre la solicitud de configuración, o adopta directamente) |
| GET | `/api/mercado` | el [Mercado](../guide/mercado.md): si está encendido, y cada página con el estado de su aplicación |
| POST | `/api/mercado` | abre una página: `{pagina, dominio}`; crea su repositorio y su aplicación (las fotos como URL `data:`, hasta 24 MB en total) |
| POST | `/api/mercado/preview` | el HTML de la página para la vista previa del formulario, como `{html}` |
| GET | `/api/mercado/{app}` | las respuestas del formulario de una página (`pagina.json`), su dirección y su repositorio |
| PUT | `/api/mercado/{app}` | guarda una página: una confirmación, que la publica |
| GET | `/api/apps/{app}` | una aplicación: especificación, estado, última ejecución y última versión |
| DELETE | `/api/apps/{app}` | borra la aplicación y todo lo que creó |
| GET | `/api/apps/{app}/impact` | lo que quitaría un borrado (se muestra antes de confirmar) |
| POST | `/api/apps/{app}/disconnect` | deja de administrar la aplicación pero deja corriendo sus cargas de trabajo |
| GET | `/api/apps/{app}/runs` | las ejecuciones de integración continua |
| GET | `/api/apps/{app}/branch-kinds` | el tipo de cada rama de las últimas 50 ejecuciones (main, fix, feature, release, deps, other) y su solicitud de incorporación ([Tipos de rama](../guide/builds.md#kinds-of-branch)) |
| GET | `/api/apps/{app}/caminos` | las ejecuciones de los últimos 14 días por rama, con dónde salió y dónde se integró cada una ([Caminos](../guide/caminos.md)) |
| POST | `/api/apps/{app}/runs` | inicia una ejecución de la rama principal (el botón **Ejecutar**) |
| GET | `/api/apps/{app}/releases` | el historial de versiones (huella por servicio) |
| GET | `/api/apps/{app}/releases/{number}/tasks/{task}/log` | el registro de una tarea posterior al despliegue de una versión |
| GET | `/api/apps/{app}/reliability?range=24h\|7d\|30d` | las comprobaciones de disponibilidad: por comprobación, la disponibilidad, p50/p95, los intervalos de la gráfica y la última comprobación; las caídas; las versiones del intervalo |
| GET | `/api/apps/{app}/visits?range=today\|yesterday\|24h\|7d\|30d` o `?day=2026-10-20` | los visitantes según Umami: por cada sitio que coincide, los totales y los del periodo anterior, una serie para la gráfica, las páginas principales, los orígenes y los países, y las visitas más recientes del periodo (sesiones anónimas) |
| GET | `/api/apps/{app}/visits/{site}/sessions/{session}?from=&to=` | las páginas y los eventos de una visita, del más viejo al más nuevo (`from` y `to` en milisegundos, a lo más 31 días de distancia; el sitio debe ser de la aplicación) |
| POST | `/api/apps/{app}/rollback` | revierte a una versión anterior (`{"release": N}`) |
| GET | `/api/apps/{app}/resources` | los objetos vivos de Kubernetes y su salud (el árbol de recursos) |
| GET | `/api/apps/{app}/secrets/{secret}` | las llaves que contiene un secreto (`{"keys": [...]}`), nunca sus valores |
| PUT | `/api/apps/{app}/secrets/{secret}` | agrega o reemplaza llaves de un secreto (`{"LLAVE": "valor"}`) y conserva las demás; `null` quita una llave. Solo escritura: los valores nunca se devuelven |
| GET | `/api/apps/{app}/events` | SSE: los cambios de estado de la aplicación |
| GET | `/api/apps/{app}/addons` | los complementos por aplicación (p. ej. Renovate) y su estado |
| PUT | `/api/apps/{app}/addons/{addon}` | enciende, apaga o configura un complemento por aplicación |

## Ejecuciones 🔒 {#runs}

| Método | Ruta | Hace |
|---|---|---|
| GET | `/api/runs/{id}` | una ejecución con sus pasos (el grafo) |
| GET | `/api/corriendo` | lo que pasa en las aplicaciones de la organización a las que la persona puede entrar: las ejecuciones en cola o en curso con sus pasos (`ahora`, la tarjeta *En ejecución* del inicio), y las últimas 30 que terminaron (`recientes`, también para el buscador de la barra superior) |
| POST | `/api/runs/{id}/cancel` | la cancela; se borran sus pods de construcción |
| GET | `/api/runs/{id}/steps/{step}/log` | el registro de un paso (guardado, o en vivo mientras corre) |
| GET | `/api/runs/{id}/dry/{task}/log` | el registro de una prueba que la ejecución corrió en seco, contra la versión publicada (`?on=copy`: contra una copia de la rama) |
| GET | `/api/runs/{id}/events` | SSE: el estado de los pasos y las líneas del registro |

## Complementos por aplicación (Renovate) 🔒 {#per-app-add-ons-renovate}

| Método | Ruta | Hace |
|---|---|---|
| GET | `/api/addons` | los tipos de complementos por aplicación |
| GET | `/api/addons/{addon}` | uno, con sus ajustes |
| PUT | `/api/addons/{addon}` | cambia sus ajustes |
| GET | `/api/addons/{addon}/runs` | sus ejecuciones |
| POST | `/api/addons/{addon}/runs` | lo ejecuta ahora |
| GET | `/api/addons/{addon}/runs/{id}` | una ejecución con su registro |

## Complementos instalados (del clúster) 🔒 {#installed-add-ons-cluster-add-ons}

| Método | Ruta | Hace |
|---|---|---|
| GET | `/api/addon-catalog` | el catálogo de un clic |
| GET | `/api/installed` | los complementos instalados (objetos `Addon`) con su estado |
| GET | `/api/installed/{name}` | uno, con su vista previa, ganchos e inventario |
| PUT | `/api/installed/{name}` | instala o cambia uno (escribe `addons/<name>.yaml` en el repositorio de gitops) |
| PATCH | `/api/installed/{name}` | interruptores: suspender, sincronización manual, podar, adoptar |
| POST | `/api/installed/{name}/sync` | aprueba y aplica los cambios pendientes (sincronización manual) |
| DELETE | `/api/installed/{name}` | desinstala (primero los ganchos pre-delete; los tipos de `neverDelete` se quedan) |

## Ejemplo {#example}

```bash
# con una galleta de sesión copiada del navegador
curl -s -b "session=…" https://rendimiento.example.com/api/apps | jq '.[].name'
curl -s -b "session=…" -X POST https://rendimiento.example.com/api/apps/jobsentry/runs
```
