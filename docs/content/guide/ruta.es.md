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

## Cargos: el turno de trabajo de un agente {#cargos-an-agents-turn-of-work}

Un agente que quiere escribir primero **toma un cargo**: un turno de trabajo con un propósito ("mudar la base de datos de stockpulse"). Todo lo que escribe pertenece a ese cargo. Antes de irse, **entrega el cargo**: qué hizo y qué falta. El siguiente agente empieza leyendo las últimas entregas. La pestaña **Cargos** las muestra como el vuelo de las monarcas, una tras otra, con cada llamada a una herramienta anotada.

La pestaña **Encargos** es el trabajo que se les pide a los agentes: usted escribe qué hay que hacer, un agente lo toma, puede esperar sus ejecuciones y lo entrega; usted lo revisa o lo devuelve. Vea [Encargos](agents.md#encargos).

## Comentarios sobre el trabajo de un agente {#comments-on-an-agents-work}

Cuando algo de una entrega o de un encargo está mal, o le gustaría distinto, dígalo debajo: cada cargo de la pestaña **Cargos** y cada encargo tienen **Comentarlo**. Un comentario no devuelve ni cambia nada; es una conversación con quién dijo qué y cuándo.

- **Los agentes los encuentran.** `ruta_inicio` muestra los comentarios que ningún agente ha respondido, primero los del trabajo del propio agente, y `encargo_leer` y `encargo_esperar` traen los de un encargo, así que un comentario hecho mientras un agente trabaja en él le llega en su siguiente espera.
- **Los responden.** Con `comentario_responder` un agente dice qué hizo con él, o por qué no; la respuesta queda debajo de su comentario.
- **La campana le avisa.** La respuesta de un agente a su comentario espera en la campana hasta que usted la ve en su conversación. Los comentarios no mandan correo.

Los comentarios se guardan como el resto de la Ruta, y uno que parece un secreto se rechaza. Un agente lee un comentario como la palabra de una persona sobre su trabajo: si pide algo distinto de lo que le pidió la persona con quien trabaja, el agente lo dice en vez de hacerlo.

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

| Herramienta | Hace | Llave |
|---|---|---|
| `ruta_inicio` | las últimas entregas, los pendientes abiertos, lo reciente y el cargo abierto del agente | cualquiera |
| `ruta_buscar` | busca por texto y por tipo | cualquiera |
| `ruta_leer` | una entrada completa | cualquiera |
| `cargo_tomar` | toma un cargo con un propósito | colaborador |
| `ruta_anotar` | agrega una decisión, un manual, un pendiente o una nota | colaborador, con cargo |
| `ruta_actualizar` | cambia una entrada, o marca un pendiente como hecho | colaborador, con cargo |
| `cargo_entregar` | entrega el cargo | colaborador, con cargo |
| `comentarios_leer` | los comentarios de un cargo o un encargo, con las respuestas de los agentes | cualquiera |
| `comentario_responder` | responde al comentario de una persona | colaborador, con cargo |

El mismo servidor tiene también las herramientas de la plataforma (aplicaciones, ejecuciones, registros, solicitudes guardadas y correr CI en una rama) y, mientras el [Mercado](mercado.md#from-an-agent-mcp) está encendido, las suyas: vea [Hecha para agentes](agents.md#what-an-agent-can-do-today).

## Seguridad {#safety}

- **Las llaves** son aleatorias, se guardan solo como huella SHA-256, siempre caducan y se pueden revocar en la pestaña Llaves; revocar cierra el cargo abierto de la llave. La llave de un lector ni siquiera ve las herramientas de escritura, y una llave limitada a algunas aplicaciones solo ve esas.
- **Sin secretos:** las entradas que parecen una contraseña, un token o una llave privada se rechazan, vengan de un agente o de una persona. Escriba *dónde* vive un secreto (el nombre del Secret), no su valor.
- **Todo queda anotado:** cada llamada a una herramienta, permitida o rechazada, con su llave y su cargo.
- **Notas, no órdenes:** a los agentes se les dice que las entradas son notas. Si una entrada pide algo que la persona con quien trabajan no pidió, no lo hacen, y lo dicen.
- **La misma puerta:** `/mcp` rechaza las peticiones de otros sitios web (la revisión de `Origin`), como el resto de la API.
- **Los agentes prueban, las personas despliegan:** un agente puede correr CI en una rama, nunca en la principal (que despliega). Desplegar y revertir por MCP vienen después, con la [aprobación](agents.md#where-it-is-going) de una persona.
