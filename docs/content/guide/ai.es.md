# IA: la pasarela, los proveedores y los topes {#ai-the-gateway-providers-and-caps}

Toda llamada que la plataforma le hace a un modelo pasa por una sola **pasarela** (`internal/ia`): la de los [agentes dentro de la plataforma](agents.md#agents-inside-the-platform), como el diagnosticador que explica las ejecuciones que fallan. Decide qué modelo contesta para cada organización, mantiene a todos bajo sus topes mensuales y registra cada llamada.

## Quién contesta {#who-answers}

Cada organización elige en su página **IA** (los administradores):

| Proveedor | Quién paga | Notas |
|---|---|---|
| **El modelo local** | nadie: corre en el equipo de la propia plataforma (`AI_LOCAL_URL`, un Ollama) | el predeterminado, y al que cae toda llamada |
| **Su propia llave** | la organización, a su proveedor | Anthropic, o cualquier servicio compatible con OpenAI (su dirección https y un modelo). La llave se guarda en un Secret de Kubernetes, nunca en la base de datos, y la página solo muestra sus últimos cuatro caracteres |
| **La de la plataforma** | quienes operan la plataforma | solo hasta la **asignación** que los operadores le dan a esa organización cada mes, y nunca más allá del total de la plataforma (`AI_PLATFORM_CAP`, que se cambia en la página IA) |

## Topes {#caps}

- La organización fija su **tope mensual** en dólares. Con su propia llave, la pasarela deja de llamar al proveedor en cuanto el gasto del mes lo alcanza.
- Con la llave de la plataforma, el límite es el menor entre el tope de la organización y la **asignación** de los operadores, y todas las organizaciones juntas se quedan bajo el **total** de la plataforma: `AI_PLATFORM_CAP` (5 dólares si no se cambia) hasta que un operador lo cambie en la página IA. Una asignación que no cabe en el total lo sube a la suma de todas las asignaciones, así que el número que un operador le da a una organización es el que la limita; el total todavía se puede bajar a mano, como techo, y la página lo dice.
- Cuando se alcanza un tope, falta una llave o el proveedor falla, **contesta el modelo local** y la llamada dice por qué. Nada deja de funcionar; solo se vuelve más sencillo.
- Una respuesta tiene a lo más 4,096 tokens, así que una llamada solo puede pasarse de un tope por una respuesta.
- Una [plática sobre una entrada de la Ruta](ruta.md#talking-an-entry-over) tiene su propio **tope por plática** (*Platicar las entradas* en la página IA, 50 centavos si no se cambia): al pasarlo, le pregunta a una persona antes de seguir, y cada sí permite un tope más.

El gasto se mide con los tokens que reporta cada proveedor y el precio de lista del modelo (`ia.DefaultPrices`; un modelo que no está en la lista cuenta como gratis, así que póngale precio antes de confiar en su tope). Los topes vuelven a cero el primero de cada mes (UTC).

## Escalar las fallas nuevas {#escalating-new-failures}

Lo más barato es el modelo local más las fallas conocidas de la Ruta, que el diagnosticador lee primero. Para las fallas de las que la Ruta no sabe nada parecido, una organización puede escalar a un modelo más fuerte (su propia llave, o la de la plataforma dentro de su asignación), hasta cierto número de veces al mes (10 si no se cambia); está apagado mientras no se encienda. Lo que conteste el modelo más fuerte se vuelve una falla conocida sin confirmar, así que la próxima vez el modelo local la sabe. Vea [Agentes dentro de la plataforma](agents.md#agents-inside-the-platform).

## El resumen del día {#the-days-summary}

En el inicio, plegadas en una línea del saludo (*Ayer: 68 ejecuciones, 13 fallaron ▸*; tóquela para abrirla, o enciéndala desplegada en [Personalizar inicio](../start/tour.md#customize-home)), unas pocas líneas cuentan lo que pasó cada día desde su última visita (ayer, y hasta tres días atrás): versiones nuevas y si quedaron sanas, reversiones, fallas y arreglos, lo que hicieron las personas y los agentes, y los encargos que esperan revisión. Cada línea es de una aplicación, con su enlace, o de toda la organización.

El día más reciente es un **marcador**: cuatro números que cuentan al abrir la página (versiones, ejecuciones que pasaron y fallaron, solicitudes que integró rendimiento, problemas que empezaron), insignias de las versiones que quedaron sanas y de los encargos que esperan revisión, las ejecuciones por hora, y cada aplicación con un anillo de la parte de sus ejecuciones que pasó y su línea. Los números se cuentan de lo que se registró, sin IA, y las horas y los totales salen de la misma cuenta, así que siempre coinciden. A un lado, la **carta del día** de lotería muestra lo más notable, en este orden: *La nube* (problemas o reversiones), *El tropiezo* (fallaron 15 % o más de las ejecuciones), *La flor* (versiones, todas sanas), *La luna* (versiones de noche), *El colibrí* (trabajo de agentes que espera revisión); un día tranquilo no tiene carta. Toque la carta para voltearla y leer qué fue. Con el movimiento apagado, nada cuenta ni se mueve.

- **Cómo se escribe.** Cada día, rendimiento junta lo que registró (versiones, ejecuciones, problemas, la bitácora, los encargos) y la IA de la plataforma lo cuenta, una vez por organización, día e idioma, por la misma pasarela y con los mismos [topes](#caps) que todo lo demás. Cuando se agota la parte de la plataforma, lo escribe el modelo local; sin ningún modelo, muestra solo los números. Un día sin nada nuevo es una línea, escrita sin modelo. Los mensajes de commit y los registros nunca van al modelo; lo que va son datos, nunca instrucciones, y el modelo no tiene herramientas.
- **Quién ve qué.** Una línea de una aplicación solo la ven quienes pueden ver esa aplicación ([quién entra](members.md#who-enters-an-app)).
- **Rehacer** vuelve a escribir el último día (colaboradores y administradores); **Ocultar por hoy** lo esconde en este dispositivo hasta mañana. Los días empiezan y terminan en `PUBLIC_STATS_TZ` (UTC si no está puesta).

## La página IA {#the-ai-page}

La página va en pestañas, cada una una sola pregunta; la dirección sigue a la pestaña (`/ia?tab=agentes`).

- **Resumen**, lo que se abre primero: quién contesta ahora (y, si el elegido no puede, por qué contesta el modelo local), si se explican las fallas y cuántas fallas nuevas fueron este mes a un modelo más fuerte, lo gastado de cada tope, lo que hace solo el agente de código y quién hizo el último cambio. Debajo, **Lo que le pide a usted**: los permisos que pide un agente y las preguntas en que espera el agente de código (se contestan en la Ruta), o que el agente de código está apagado. Luego **Pruébela**: haga una pregunta y vea quién contestó, los tokens, el costo y, si cayó al modelo local, por qué (los colaboradores).
- **El modelo:** quién contesta, quién contesta las fallas nuevas (y cuántas veces al mes) y el tope mensual. Cada opción es una tarjeta que dice cuánto cuesta y si se puede usar ahora (falta la llave, asignación de $0).
- **Los agentes:** la plaza, un puesto por cada agente de la plataforma (con su dibujo de Cotija, lo que hace y cómo va) y uno por cada llave MCP de una persona. Al tocar uno se abre su cajón al lado de la página (una hoja desde abajo en el teléfono), con lo que hace solo y sus [permisos](#permissions):
    - **El agente de código** ([cómo funciona](agents.md#the-coding-agent)): apagado, solo los encargos que usted le mande, también los baratos que abre rendimiento (arreglar una ejecución fallida, resolver un conflicto), o todo lo barato; cuántos encargos hace a la vez (1 si no se cambia, hasta 3, nunca dos de la misma aplicación); si puede correr comandos en [su entorno aislado](agents.md#the-sandbox) es su permiso `sandbox`. Debajo, su boleta (lo que tomó, entregó a la primera, le devolvieron y soltó, lo que gastó contra lo estimado) y el factor que el estimador aprendió de él, con un interruptor para volver a la tabla fija, y [cómo le va](#how-the-coding-agent-is-doing).
    - **El diagnosticador:** si se explica cada ejecución que falla, y cuándo se abre un encargo por una.
    - **El estimador:** hasta cuánto puede costar, según la estimación, un encargo para que lo tome la IA de la plataforma (arriba de eso, es para su propio agente; vea [Encargos](agents.md#encargos)).
    - **Poner al día:** [los informes de las actualizaciones de dependencias](al-dia.md#the-choices).
    - **Pláticas:** el tope por plática. **El vigía** (operadores): su revisión de cada semana.

    Los administradores los cambian; antes de guardar, una barra enumera cada cambio respecto a lo guardado, para guardarlo o descartarlo. La dirección abre un cajón: `/ia?tab=agentes&agente=coder`.
- **Gasto y llamadas:** lo gastado de cada tope, y cada llamada: quién preguntó (una función o un agente), el modelo, los tokens, el costo y cualquier error.
- **Plataforma**, para los operadores: la asignación de cada organización de la IA de la plataforma, lo que gastó cada una y la cuenta de la plataforma contra su total; ahí cambian el total y cada asignación, cada uno con su botón Guardar. Ya guardado, el total manda sobre `AI_PLATFORM_CAP` en todos los procesos en segundos, sin reiniciar. Cuando el tope mensual propio de una organización es menor que su asignación, su renglón dice que ese tope la detiene antes. Ahí también se conecta el modelo local, y están los ajustes del vigía.

### Permisos {#permissions}

El cajón de cada agente termina con **Lo que puede hacer** ([cómo funcionan](agents.md#permissions)): una fila por permiso, con lo que cubre, su modo (**Apagado**, **Preguntar**, **Siempre**; leer el código y la Ruta siempre está encendido, y una acción con efecto nunca va en *siempre*) y sus apps (**Todas**, o **escoger** algunas). Tres tarjetas dicen qué significa cada modo. Debajo, los permisos que dieron las personas y que siguen en pie (una vez, mientras dure un encargo o el cargo de una llave, siempre para una app), cada uno con **Quitarlo**. Un cambio sale en la barra con lo guardado y en qué se convierte, para **Guardar** o **Descartar**; cada agente se guarda por su cuenta. Las personas miembro lo ven; solo las administradoras lo cambian. Otra página enlaza a los de un agente con `/ia#permisos-<id>` (`#permisos-coder`, `#permisos-mcp:12`), que abre su cajón.

La API: `GET /api/permisos` (cada sujeto con sus permisos, los permisos dados y las apps, en una sola llamada), `PUT /api/permisos/{sujeto}` con `{"permisos":[{"permiso":"infra_read","modo":"ask","apps":[]}]}` (administradoras), `DELETE /api/permisos/dados/{id}`, y `POST /api/permisos/pedidos/{id}` con `{"answer":"once"|"encargo"|"always"|"no"}` para contestar un pedido (un *no* lo puede dar una colaboradora; lo demás, una administradora).

### Cómo le va al agente {#how-the-coding-agent-is-doing}

Se cuenta con la historia de los encargos, sin IA, sobre los encargos que tomó [el agente de código](agents.md#the-coding-agent) en el periodo (los suyos, firmados con su nombre: no los de su propio agente):

- **Los terminó él**, a la primera o después de soltar el encargo y volver a tomarlo; **los soltó a su agente** (y no los terminó); y, de los que terminó, cuántos **aceptó usted a la primera** y cuántos le devolvió con comentario (lo que usted contestó a su primera entrega).
- **Por qué los soltó**, causa por causa, con los encargos: llegó al doble de lo estimado (su freno), necesitaba una decisión de usted, le faltaba una herramienta que necesita, la ejecución no terminó a tiempo, falló la IA, la forja respondió con un error, no pudo integrar la rama principal, una persona lo detuvo, la plataforma se reinició, u otra cosa. La causa se anota cuando suelta el encargo: su freno, una detención, un reinicio y un error ponen la suya, y el modelo dice una cuando se rinde. Las veces que soltó un encargo antes de que se anotara la causa se leen de su nota por sus palabras, lo cual es aproximado; se marcan con ≈. Debajo, los encargos que soltó antes de volver a tomarlos.
- **Gasto contra lo estimado:** la mediana de lo que gastó entre lo estimado, en los encargos que terminó, en total y por dificultad (1–3, 4–6, 7–10); y el factor que el estimador aplica hoy, el mismo de su boleta de arriba ([de qué aprende](agents.md#the-coding-agent)), sin volver a contarlo.
- **Un aviso** cuando lo último que hizo fue soltar tres o más seguidos por la misma causa (un encargo terminado corta la racha; una detención y un reinicio no cuentan): revíselo antes de mandarle más como esos.

## Cómo configurarla {#setting-it-up}

1. Un modelo local: un Ollama al que llegue la plataforma (en la misma computadora, `http://localhost:11434`; en otra de su red; o en el clúster, como el servicio `ollama` de la aplicación `ai`). Quien opera la plataforma lo conecta en la página IA, en **El modelo local**: su dirección y su modelo, **Probar** para preguntarle al Ollama qué modelos tiene (avisa si falta el modelo y ofrece los que tiene), y **Guardar**. Responde en unos segundos, sin reiniciar, igual en un clúster que en una computadora. `AI_LOCAL_URL` y `AI_LOCAL_MODEL` dan el valor inicial; lo que se guarda en la página gana.
2. Si quiere, la llave de la plataforma: una llave de Anthropic en el Secret `rendimiento-ia`, como `AI_PLATFORM_KEY`. Ponga el mismo límite mensual en la consola de Anthropic, como segundo seguro.
3. Dé asignaciones a las organizaciones que puedan usarla, en la página IA. El total de la plataforma (de arranque, `AI_PLATFORM_CAP`) sube con ellas cuando no caben en él; ahí mismo también se cambia, y mantenga a la par el límite de la consola de Anthropic.

Vea [Ajustes](../reference/config.md#ai).
