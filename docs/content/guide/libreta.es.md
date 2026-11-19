# La libreta: lo que se pidió para cada aplicación

Cada aplicación tiene una pestaña **Libreta**, junto a Caminos: la lista de lo que se pidió para esa aplicación, y si ya se hizo. Es una vista, no una segunda memoria. Cada renglón sale de algo que ya existe, y la [Ruta](ruta.md) sigue siendo el único lugar donde se escribe y se decide.

## De dónde sale cada renglón {#where-each-line-comes-from}

| Fuente | Qué |
|---|---|
| **La Ruta** | los pendientes y las decisiones cuyo campo `app` nombra a esta aplicación ([entradas de una aplicación](ruta.md#entries-about-an-app)) |
| **Encargos** | cada encargo de esta aplicación, abierto o terminado |
| **Renovate** | sus solicitudes de integración en el repositorio de la aplicación (y las de Dependabot), salvo que una entrada o un encargo ya hable por esa rama. *Incluir los de Renovate* las esconde |
| **Las pruebas propias de la aplicación** | mientras algunas de sus [pruebas de navegador](reliability.md#the-apps-own-tests) solo avisan, un renglón dice cuántas versiones limpias les faltan para poder regresar una versión |

**Anotar un pedido** escribe un pendiente en la Ruta con la aplicación ya puesta, firmado por usted. Los agentes hacen lo mismo con `ruta_anotar` y su campo `app`.

## Cómo avanza un pedido {#how-a-request-moves}

Nadie marca un estado a mano: rendimiento lo calcula cada vez con lo que ya sabe.

| Estado | Cuándo |
|---|---|
| **Pedido** | está anotado, y todavía no se sabe nada más |
| **En curso** | un agente tomó su encargo, o tiene una rama ligada (su ejecución va, falló o choca) |
| **Listo para integrar** | la última ejecución de su rama pasó |
| **Integrado** | su rama ya está en la rama principal: se integró su solicitud, rendimiento la integró, o su confirmación ya está ahí |
| **Publicado** | una versión hecha después de la integración pasó su [verificación](reliability.md#verifying-each-release) |

Y dos aparte:

- **Esperando**: depende de algo que todavía no pasa. Un encargo entregado espera su revisión; una rama que pasó espera mientras GitHub no la deje integrar (una revisión o una comprobación obligatoria); las pruebas propias de la aplicación esperan versiones limpias seguidas.
- **Descartado**: se dejó de lado, con su porqué. Un encargo cancelado, con la nota con que se canceló; una entrada de la Ruta dejada de lado con su porqué (`descartado` en `ruta_actualizar`, o `discarded` desde la API); una solicitud de Renovate cerrada sin integrarse.

Un pendiente marcado en la Ruta, una decisión o un encargo aceptado, sin rama que diga más, queda **Hecho**.

### Qué ramas son de un pedido {#which-branches-belong-to-a-request}

- Una entrada de la Ruta: las ramas que tiene ligadas (`rama` en `ruta_anotar` o `ruta_actualizar`; cada nueva se agrega).
- Un encargo: la rama que el agente de código le pone (`feature/encargo-<n>-…`), la rama de su ejecución, y las ramas y solicitudes de sus enlaces. Una rama cuenta en cuanto rendimiento la ha visto (una ejecución o una solicitud).

Cuando un pedido tiene varias ramas, habla por él la que va más adelante.

## Qué se ve {#what-you-see}

- **Abiertos**, **Hechos** y **Todos**: los pedidos abiertos con cuánto ha avanzado cada uno (pedido, su rama, integrado, publicado) y sus enlaces (su entrada de la Ruta o su encargo, el cargo, la rama, la solicitud y la ejecución); lo que se hizo esta semana con su integración y la versión en que salió; y lo descartado, con su porqué.
- **Esta semana**: cuántos se hicieron, cuántos siguen abiertos y cuántos se dejaron de lado.
- En el teléfono va en una sola columna, y el camino de cada pedido es una barra de cuatro pasos.

## Para los agentes {#for-agents}

`app_estado` trae la libreta en su campo `libreta`: `abiertos`, `hechos_semana` (lo hecho en los últimos siete días) y `descartados_semana`, cada renglón con su `fuente` (ruta, encargo, renovate, pruebas), su `estado` (pedido, en_curso, listo, integrado, publicado, esperando, descartado, hecho), su `porque`, y su rama, solicitud, ejecución y versión. Un agente que trabaja en una aplicación ve de un vistazo qué falta ahí. Cambia un estado haciendo el trabajo (una rama, una ejecución, una integración), nunca marcándolo.

## De dónde salen los datos {#where-the-data-comes-from}

- Las entradas de la Ruta, sus ramas ligadas y los encargos salen de la base de datos de rendimiento. Se leen hasta 300 de cada uno.
- Las ejecuciones son las de los últimos 60 días, y las versiones las últimas 100 con su verificación; la versión en que salió una integración es la primera hecha por una ejecución de la rama principal creada después de la integración.
- Las solicitudes de integración, y si la confirmación de una rama ya está en la rama principal, salen de GitHub, como en la [mesa de trabajo](../start/tour.md#the-app-page), y se recuerdan dos minutos. Si GitHub no responde, las integraciones se saben solo por las ejecuciones (las que integró rendimiento) y faltan las solicitudes de Renovate; la pestaña lo dice.

## En el código {#in-the-code}

| Qué | Dónde |
|---|---|
| Los estados, calculados con los hechos | `internal/libreta` (`Build`) |
| La lectura de los hechos | `platform.Libreta` |
| Las entradas de una aplicación, sus ramas, los encargos de una aplicación | `store.RutaOfApp`, `store.RutaBranches`, `store.EncargosOfApp` (migración `0057_libreta.sql`) |
| La pestaña | `web/src/components/libreta.tsx` |
| La API | `GET` y `POST /api/apps/{app}/libreta` |
