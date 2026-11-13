# IA: la pasarela, los proveedores y los topes {#ai-the-gateway-providers-and-caps}

Toda llamada que la plataforma le hace a un modelo pasa por una sola **pasarela** (`internal/ia`): la de los [agentes dentro de la plataforma](agents.md#agents-inside-the-platform), como el diagnosticador que explica las ejecuciones que fallan. Decide qué modelo contesta para cada organización, mantiene a todos bajo sus topes mensuales y registra cada llamada.

## Quién contesta {#who-answers}

Cada organización elige en su página **IA** (los administradores):

| Proveedor | Quién paga | Notas |
|---|---|---|
| **El modelo local** | nadie: corre en el equipo de la propia plataforma (`AI_LOCAL_URL`, un Ollama) | el predeterminado, y al que cae toda llamada |
| **Su propia llave** | la organización, a su proveedor | Anthropic, o cualquier servicio compatible con OpenAI (su dirección https y un modelo). La llave se guarda en un Secret de Kubernetes, nunca en la base de datos, y la página solo muestra sus últimos cuatro caracteres |
| **La de la plataforma** | quienes operan la plataforma | solo hasta la **asignación** que los operadores le dan a esa organización cada mes, y nunca más allá del total de la plataforma (`AI_PLATFORM_CAP`) |

## Topes {#caps}

- La organización fija su **tope mensual** en dólares. Con su propia llave, la pasarela deja de llamar al proveedor en cuanto el gasto del mes lo alcanza.
- Con la llave de la plataforma, el límite es el menor entre el tope de la organización y la **asignación** de los operadores, y todas las organizaciones juntas se quedan bajo `AI_PLATFORM_CAP` (5 dólares si no se cambia).
- Cuando se alcanza un tope, falta una llave o el proveedor falla, **contesta el modelo local** y la llamada dice por qué. Nada deja de funcionar; solo se vuelve más sencillo.
- Una respuesta tiene a lo más 4,096 tokens, así que una llamada solo puede pasarse de un tope por una respuesta.

El gasto se mide con los tokens que reporta cada proveedor y el precio de lista del modelo (`ia.DefaultPrices`; un modelo que no está en la lista cuenta como gratis, así que póngale precio antes de confiar en su tope). Los topes vuelven a cero el primero de cada mes (UTC).

## Escalar las fallas nuevas {#escalating-new-failures}

Lo más barato es el modelo local más las fallas conocidas de la Ruta, que el diagnosticador lee primero. Para las fallas de las que la Ruta no sabe nada parecido, una organización puede escalar a un modelo más fuerte (su propia llave, o la de la plataforma dentro de su asignación), hasta cierto número de veces al mes (10 si no se cambia); está apagado mientras no se encienda. Lo que conteste el modelo más fuerte se vuelve una falla conocida sin confirmar, así que la próxima vez el modelo local la sabe. Vea [Agentes dentro de la plataforma](agents.md#agents-inside-the-platform).

## El resumen del día {#the-days-summary}

Arriba del inicio, unas pocas líneas cuentan lo que pasó cada día desde su última visita (ayer, y hasta tres días atrás): versiones nuevas y si quedaron sanas, reversiones, fallas y arreglos, lo que hicieron las personas y los agentes, y los encargos que esperan revisión. Cada línea es de una aplicación, con su enlace, o de toda la organización.

El día más reciente es un **marcador**: cuatro números que cuentan al abrir la página (versiones, ejecuciones que pasaron y fallaron, solicitudes que integró rendimiento, problemas que empezaron), insignias de las versiones que quedaron sanas y de los encargos que esperan revisión, las ejecuciones por hora, y cada aplicación con un anillo de la parte de sus ejecuciones que pasó y su línea. Los números se cuentan de lo que se registró, sin IA, y las horas y los totales salen de la misma cuenta, así que siempre coinciden. A un lado, la **carta del día** de lotería muestra lo más notable, en este orden: *La nube* (problemas o reversiones), *El tropiezo* (fallaron 15 % o más de las ejecuciones), *La flor* (versiones, todas sanas), *La luna* (versiones de noche), *El colibrí* (trabajo de agentes que espera revisión); un día tranquilo no tiene carta. Toque la carta para voltearla y leer qué fue. Con el movimiento apagado, nada cuenta ni se mueve.

- **Cómo se escribe.** Cada día, rendimiento junta lo que registró (versiones, ejecuciones, problemas, la bitácora, los encargos) y la IA de la plataforma lo cuenta, una vez por organización, día e idioma, por la misma pasarela y con los mismos [topes](#caps) que todo lo demás. Cuando se agota la parte de la plataforma, lo escribe el modelo local; sin ningún modelo, muestra solo los números. Un día sin nada nuevo es una línea, escrita sin modelo. Los mensajes de commit y los registros nunca van al modelo; lo que va son datos, nunca instrucciones, y el modelo no tiene herramientas.
- **Quién ve qué.** Una línea de una aplicación solo la ven quienes pueden ver esa aplicación ([quién entra](members.md#who-enters-an-app)).
- **Rehacer** vuelve a escribir el último día (colaboradores y administradores); **Ocultar por hoy** lo esconde en este dispositivo hasta mañana. Los días empiezan y terminan en `PUBLIC_STATS_TZ` (UTC si no está puesta).

## La página IA {#the-ai-page}

- **Arriba, cómo está todo:** quién contesta ahora (y, si el elegido no puede, por qué contesta el modelo local), si se explican las fallas y cuántas fallas nuevas fueron este mes a un modelo más fuerte, lo gastado de cada tope y quién hizo el último cambio.
- **La configuración, en cinco partes:** 1. quién contesta, 2. explicar las fallas (y quién contesta las nuevas), 3. el tope mensual, 4. encargos: hasta cuánto puede costar, según la estimación, un encargo para que lo tome la IA de la plataforma (arriba de eso, es para su propio agente; vea [Encargos](agents.md#encargos)), 5. [el agente de código](agents.md#the-coding-agent): apagado, solo los encargos que usted le mande, o también los fáciles. Cada opción es una tarjeta que dice cuánto cuesta y si se puede usar ahora (falta la llave, asignación de $0). Los administradores la cambian; antes de guardar, una barra enumera cada cambio respecto a lo guardado, para guardarlo o descartarlo.
- **Pruébela:** haga una pregunta y vea quién contestó, los tokens, el costo y, si cayó al modelo local, por qué (los colaboradores).
- **Últimas llamadas:** cada llamada, quién preguntó (una función o un agente), el modelo, los tokens, el costo y cualquier error.
- **Los operadores** ven además la asignación de cada organización de la IA de la plataforma, lo que gastó cada una y la cuenta de la plataforma; ahí cambian las asignaciones, cada una con su botón Guardar.

## Cómo configurarla {#setting-it-up}

1. Un modelo local: un Ollama al que llegue la plataforma (por ejemplo el servicio `ollama` de la aplicación `ai`), en `AI_LOCAL_URL`, con `AI_LOCAL_MODEL`.
2. Si quiere, la llave de la plataforma: una llave de Anthropic en el Secret `rendimiento-ia`, como `AI_PLATFORM_KEY`. Ponga el mismo límite mensual en la consola de Anthropic, como segundo seguro.
3. Dé asignaciones a las organizaciones que puedan usarla, en la página IA.

Vea [Ajustes](../reference/config.md#ai).
