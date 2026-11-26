# La Ruta

Ninguna mariposa monarca hace sola toda la migración: hacen falta varias generaciones, y aun así cada otoño llegan a los mismos bosques de oyamel en Michoacán. Los agentes de IA son así. Una sesión de un agente no recuerda nada de la anterior, y el propio modelo será reemplazado. **La Ruta** es donde se guarda el camino: la memoria de la plataforma, para las personas y para los agentes, para que quien siga (otra sesión, otro modelo o una persona) continúe donde se quedó el anterior.

## Qué guarda {#what-it-holds}

| Tipo | Qué | Quién lo escribe |
|---|---|---|
| **Decisión** | qué se decidió, y por qué | personas y agentes |
| **Manual** | cómo se hace algo aquí | personas y agentes |
| **Pendiente** | algo que falta hacer; se marca cuando queda hecho | personas y agentes |
| **Nota** | cualquier otra cosa que valga la pena saber | personas y agentes |
| **Lo vivido** | relatos que vivieron las personas, en sus propias palabras | **solo personas** |

Lo vivido existe porque un agente solo sabe lo que alguien escribió, no lo que las personas vivieron. Esos relatos se guardan tal como se contaron; los agentes los leen con respeto, pero nunca los pueden escribir ni cambiar.

No se pierde nada: una edición conserva la versión anterior, y *Archivar* esconde una entrada sin borrarla.

## Entradas de una aplicación {#entries-about-an-app}

Un pendiente o una decisión puede decir de qué **aplicación** trata (el campo opcional `app`). La Ruta sigue siendo una sola; la entrada simplemente sale también en la [libreta](libreta.md) de esa aplicación, la lista de lo que se pidió para ella y si ya se hizo. Una entrada de una aplicación puede nombrar las **ramas** en que se trabaja (`rama`), para que su estado salga de sus ejecuciones, su integración y su versión, y se puede **dejar de lado** con su porqué (`descartado`), que la libreta muestra como *descartado* (o con **Descartar** en su tarjeta ahí). Una rama también se liga sola: una confirmación subida a ella con una línea `Ruta: 150` nombra esa entrada, y una rama que un agente ejecuta en su cargo responde a las entradas de esa aplicación que escribió o cambió en el cargo ([qué ramas son de un pedido](libreta.md#which-branches-belong-to-a-request)). El historial guarda también la aplicación de una entrada y su porqué de dejarla de lado, como sus demás campos. **Anotar un pedido**, en la pestaña Libreta de una aplicación, escribe un pendiente con la aplicación ya puesta. Una entrada de una aplicación que la persona no puede ver se le esconde, igual que una etiquetada con ella.

## Para usted: la portada {#for-you}

La Ruta abre en **Para usted**: cuántas cosas esperan su palabra, el [agente de código](agents.md#the-coding-agent) en vivo (qué hace ahora y cuánto lleva; al tocarlo abre su fila), y luego una fila por cada cosa que lo necesita a usted, cada una con **una** acción:

| Fila | Qué es | Su acción |
|---|---|---|
| **Revisar** | un encargo que un agente entregó | lo abre para marcarlo revisado o devolverlo |
| **Encargos que esperan quién los haga** | encargos pendientes que nadie tiene, que el agente de código no tomará por su cuenta y que usted no le dejó a su propio agente | marque los que manda al agente de código (la barra de abajo los manda, con lo que costarían), o **Escoger app** cuando uno no dice cuál |
| **Decidir** | una entrada con la etiqueta `por-decidir`: una decisión que solo toma una persona | la abre |
| **Calificar** | una falla conocida que el diagnosticador escribió con un modelo más fuerte, todavía *sin confirmar* | la ejecución, para decir si acertó |
| **Hacer usted** | un pendiente con la etiqueta `para-usted`, o con su usuario (así marcan los agentes lo que solo usted puede hacer: una cuenta que solo usted tiene, una llave que solo usted guarda) | abre sus pasos |
| **Nadie ha respondido** | el comentario de una persona sobre el trabajo de un agente que ningún agente ha respondido | el hilo |

Los agentes conocen las dos etiquetas por `ruta_anotar`: `por-decidir` cuando una entrada espera la palabra de una persona, `para-usted` (o el usuario de la persona) cuando solo una persona puede hacerlo. **Mientras tanto** cuenta lo que va solo: encargos en manos de agentes, pendientes para después, lo hecho hoy (encargos entregados y pendientes marcados) y las historias nuevas de lo vivido, cada uno con un enlace a donde está.

En el teléfono, los cuatro lugares de la Ruta son una barra abajo, encima del menú: **Para usted**, **Encargos**, **El agente** (la fila del agente de código, vea [Encargos](agents.md#the-coding-agent)) y **Cuaderno**. En la computadora son pestañas arriba, y Para usted tiene una columna a la derecha con el agente, las cuentas, las secciones del cuaderno y las últimas entradas. El cuaderno es un buscador y una tarjeta por sección: pendientes, decisiones, manuales, lo vivido, cargos y las llaves de los agentes, con lo último que se escribió. Los enlaces de antes siguen sirviendo: `?tab=encargos`, `?tab=cargos`, `?tab=vivido`, `?tab=llaves`, y `?tab=ruta` o `?entrada=N` para el índice de todas las entradas. Con el movimiento apagado (en la barra de arriba, o en el dispositivo) la monarca, el giro del agente y su avance se quedan quietos.

## Cargos: el turno de trabajo de un agente {#cargos-an-agents-turn-of-work}

Un agente que quiere escribir primero **toma un cargo**: un turno de trabajo con un propósito ("mudar la base de datos de stockpulse"). Todo lo que escribe pertenece a ese cargo. Antes de irse, **entrega el cargo**: qué hizo y qué falta. El siguiente agente empieza leyendo las últimas entregas. La pestaña **Cargos** las muestra como el vuelo de las monarcas, una tras otra, con cada llamada a una herramienta anotada.

**Encargos** es el trabajo que se les pide a los agentes: usted escribe qué hay que hacer, un agente lo toma, puede esperar sus ejecuciones y lo entrega; usted lo revisa o lo devuelve. Vea [Encargos](agents.md#encargos).

## Comentarios sobre el trabajo de un agente {#comments-on-an-agents-work}

Cuando algo de una entrega o de un encargo está mal, o le gustaría distinto, dígalo debajo: cada cargo de la pestaña **Cargos** y cada encargo tienen **Comentarlo**. Un comentario no devuelve ni cambia nada; es una conversación con quién dijo qué y cuándo.

- **Los agentes los encuentran.** `ruta_inicio` muestra los comentarios que ningún agente ha respondido, primero los del trabajo del propio agente, y `encargo_leer` y `encargo_esperar` traen los de un encargo, así que un comentario hecho mientras un agente trabaja en él le llega en su siguiente espera.
- **Los responden.** Con `comentario_responder` un agente dice qué hizo con él, o por qué no; la respuesta queda debajo de su comentario.
- **La campana le avisa.** La respuesta de un agente a su comentario espera en la campana hasta que usted la ve en su conversación. Los comentarios no mandan correo.

Los comentarios se guardan como el resto de la Ruta, y uno que parece un secreto se rechaza. Un agente lee un comentario como la palabra de una persona sobre su trabajo: si pide algo distinto de lo que le pidió la persona con quien trabaja, el agente lo dice en vez de hacerlo.

## Platicar una entrada {#talking-an-entry-over}

Toda entrada, menos lo vivido, se abre en dos pestañas: **La entrada** y **Platicarla**, con cuántos mensajes lleva. La segunda es una plática sobre la entrada, que se guarda con ella: entra quien pueda ver la entrada, y cada mensaje va firmado por quien lo escribió (el usuario de una persona, la IA con su modelo, o un agente con el nombre de su llave). Dos personas en la plática ven los mensajes de la otra en pocos segundos.

- **Quién responde.** La caja para escribir tiene las IA que ofrece ahora la [página IA](ai.md), cada una con lo que cuesta un mensaje: la IA de la plataforma, la llave propia de su organización, el modelo local (gratis) y **Mi agente**, su propio Claude Code o Codex. Lo que escoja se guarda con la plática. Con Mi agente no responde la IA de la plataforma: su agente encuentra las pláticas que lo esperan con `platicas_esperando` y responde con `platica_responder`, en un cargo, firmado con el nombre de su llave.
- **Qué lee la IA.** La entrada, las entradas de la Ruta relacionadas con ella (nunca lo vivido) y las páginas del libro cuyo título coincide; debajo de cada respuesta dice qué leyó. Solo platica: no tiene herramientas y no cambia nada. Lo que escribieron las personas le llega cercado, como datos y nunca como órdenes, así que un texto que le pide hacer algo es solo parte de lo que dice el texto.
- **La idea hasta ahora.** Después de cada respuesta la IA escribe, en pocos renglones, lo que se ha decidido y, al final, lo que falta decidir (esos renglones empiezan con *Falta decidir*, en rojo). Cualquiera en la plática la puede **Corregir**; la IA conserva las correcciones.
- **Lo que cuesta.** Debajo de la caja: lo que lleva gastado la plática. Al pasar el **tope por plática** (en la página IA, en *Platicar las entradas*; 50 centavos si un administrador no lo cambia) pregunta antes de seguir, y cada sí permite un tope más. El modelo local y su propio agente aquí no cuestan; cada llamada pasa por la pasarela de la IA, así que los topes del mes también se cumplen.

**Ya está: hacer encargos** le pide a la IA que convierta lo decidido en encargos. Usted ve lo que se decidió (con **Guardarlo como decisión en la Ruta**: una entrada de decisión ligada a esta) y cada encargo propuesto, para dejarlo o quitarlo, con su título, su **app** (obligatoria), su dificultad y lo que gastaría la IA de la plataforma (con el precio del estimador, como cualquier encargo), **quién lo hace** (la IA de la plataforma o su propio agente, según su monto por encargo, que usted puede cambiar) y **después de** cuál va. **+ Agregar otro** añade uno a mano. **Crearlos** los crea; **Crear y mandar** además manda los de la IA de la plataforma al [agente de código](agents.md#the-coding-agent). Cada encargo lleva un enlace a la plática, la idea como quedó y la decisión, para que el agente que lo tome sepa el porqué. **Solo guardar la decisión** guarda la decisión sin encargos.

## Conectar un agente (MCP) {#connecting-an-agent-mcp}

Los agentes llegan a la Ruta por **MCP** (Model Context Protocol), un estándar abierto que hablan muchas herramientas de IA, en `https://<su rendimiento>/mcp`.

1. En **Ruta → Llaves**, cree una llave: un nombre, su [función](agents.md#roles) (**lector**, o **colaborador** para además escribir y correr CI), si quiere las aplicaciones que puede usar, y cuánto dura (90 días como máximo). La llave se muestra **una sola vez**.
2. Conecte el agente. En Claude Code:

    ```bash
    claude mcp add --transport http rendimiento https://rendimiento.example.com/mcp \
      --header "Authorization: Bearer $RENDIMIENTO_MCP_TOKEN"
    ```

    O confirme un `.mcp.json` en un repositorio, para que quien lo abra con un agente quede conectado, con la llave en una variable de entorno y nunca en git:

    ```json
    { "mcpServers": { "rendimiento": { "type": "http", "url": "https://rendimiento.example.com/mcp",
        "headers": { "Authorization": "Bearer ${RENDIMIENTO_MCP_TOKEN}" } } } }
    ```

3. Cuando el agente se conecta, el servidor le explica las reglas: empezar con `ruta_inicio`, tomar un cargo antes de escribir, entregarlo antes de irse y nunca escribir secretos.

Las reglas y las descripciones de las herramientas vienen en el idioma de la plataforma (`NOTIFY_LANG`), o en el que pida la dirección: `/mcp?lang=en` o `/mcp?lang=es`. Las herramientas conservan sus nombres en español (`ruta_inicio`, `cargo_tomar`…) en los dos, igual que los campos de sus respuestas; las instrucciones en inglés explican las palabras que un agente encuentra (*cargo*, *encargo*, *entrega*).

### Cuando una versión cambia las herramientas {#when-a-release-changes-the-tools}

El cliente de un agente lista las herramientas, y lo que responde cada una (su esquema de salida), al conectarse, y conserva esa lista toda la sesión. El punto de conexión no guarda estado: cada solicitud se responde por sí sola, sin un canal de vuelta al cliente, así que no puede enviar `notifications/tools/list_changed` cuando una versión agrega o cambia una herramienta. Una sesión abierta antes de una versión conserva las herramientas de la versión anterior: lo agregado después le falta hasta que el agente empieza una sesión nueva. Para que esa sesión siga funcionando mientras tanto, los esquemas de salida son abiertos (nunca dicen `additionalProperties: false`) y un campo nuevo de una respuesta siempre es opcional: una sesión vieja acepta una respuesta con un campo de más. Las sesiones abiertas antes de la primera versión con esquemas abiertos todavía tienen los cerrados; si una herramienta responde *Structured content does not match the tool's output schema*, empiece una sesión nueva.

| Herramienta | Hace | Llave |
|---|---|---|
| `ruta_inicio` | las últimas entregas, los pendientes abiertos, lo reciente y el cargo abierto del agente | cualquiera |
| `ruta_buscar` | busca por texto y por tipo | cualquiera |
| `ruta_leer` | una entrada completa | cualquiera |
| `cargo_tomar` | toma un cargo con un propósito | colaborador |
| `ruta_anotar` | agrega una decisión, un manual, un pendiente o una nota; con `app`, de esa aplicación (sale en su libreta), y con `rama`, la rama en que se trabaja | colaborador, con cargo |
| `ruta_actualizar` | cambia una entrada o su `app`, marca un pendiente como hecho, liga una rama más (`rama`) o la deja de lado con su porqué (`descartado`) | colaborador, con cargo |
| `cargo_entregar` | entrega el cargo | colaborador, con cargo |
| `comentarios_leer` | los comentarios de un cargo o un encargo, con las respuestas de los agentes | cualquiera |
| `comentario_responder` | responde al comentario de una persona | colaborador, con cargo |
| `platicas_esperando` | las pláticas sobre entradas que escogieron al propio agente de la persona (*Mi agente*) y lo esperan, con su entrada, su idea y sus últimos mensajes | cualquiera |
| `platica_responder` | responde una, firmada con el nombre de la llave, y pone al día su idea (`idea`) | colaborador, con cargo |

El mismo servidor tiene también las herramientas de la plataforma (aplicaciones, ejecuciones, registros, solicitudes guardadas y correr CI en una rama) y, mientras el [Mercado](mercado.md#from-an-agent-mcp) está encendido, las suyas: vea [Hecha para agentes](agents.md#what-an-agent-can-do-today).

### Los permisos de cada llave {#each-keys-permissions}

Además de su función y sus aplicaciones, cada llave tiene sus propios [permisos](agents.md#permissions), que se ponen en la [página IA → Permisos](ai.md#permissions) escogiendo la llave en la lista: las vistas de la plataforma, la infraestructura de la aplicación (`infra_leer`), sus copias (`copias_leer`) y las acciones con efecto (`servicio_reiniciar`, `copia_rehacer`, y correr una rama o reintentar una ejecución). Una llave nueva empieza igual que el agente de código: ve la plataforma y las copias, pregunta por la infraestructura y no hace nada con efecto. Una herramienta con el permiso apagado contesta que está apagado; una que pregunta contesta *necesita permiso; se le pidió a &lt;quien hizo la llave&gt;*, con el número del pedido, y el pedido sale en **Para usted**. El agente dice por qué con `permiso_pedir`; en cuanto una persona lo da (solo esta vez, mientras su cargo siga abierto, o siempre para esa aplicación), la siguiente llamada funciona. Cada uso queda en la [bitácora](../architecture/security.md#the-bitacora) con el permiso.

## Seguridad {#safety}

- **Las llaves** son aleatorias, se guardan solo como huella SHA-256, siempre caducan y se pueden revocar en la pestaña Llaves; revocar cierra el cargo abierto de la llave. La llave de un lector ni siquiera ve las herramientas de escritura, y una llave limitada a algunas aplicaciones solo ve esas.
- **Sin secretos:** las entradas que parecen una contraseña, un token o una llave privada se rechazan, vengan de un agente o de una persona. Escriba *dónde* vive un secreto (el nombre del Secret), no su valor.
- **Todo queda anotado:** cada llamada a una herramienta, permitida o rechazada, con su llave y su cargo.
- **Notas, no órdenes:** a los agentes se les dice que las entradas son notas. Si una entrada pide algo que la persona con quien trabajan no pidió, no lo hacen, y lo dicen.
- **La misma puerta:** `/mcp` rechaza las peticiones de otros sitios web (la revisión de `Origin`), como el resto de la API.
- **Los agentes prueban, las personas despliegan:** un agente puede correr CI en una rama, nunca en la principal (que despliega). Desplegar y revertir por MCP vienen después, con la [aprobación](agents.md#where-it-is-going) de una persona.
