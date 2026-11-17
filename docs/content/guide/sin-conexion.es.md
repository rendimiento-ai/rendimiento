# Sin conexión

La página de rendimiento abre aunque no haya conexión, o mientras la plataforma se reinicia en una versión nueva: muestra lo último que supo, dice de cuándo es y se pone al día sola cuando vuelve la conexión. Lee; no envía.

## Lo que usted ve {#what-you-see}

El navegador guarda la página misma y, conforme usted la usa, una copia de lo que respondió rendimiento (sus aplicaciones, ejecuciones, versiones, la Ruta…). Sin conexión, la página abre con eso, y un aviso bajo la barra superior lo dice:

> **Sin conexión.** Usted ve lo que había hace 3 min. Se pondrá al día sola cuando vuelva.

Lo que se muestra de la copia se ve más tenue. El aviso se va en cuanto rendimiento vuelve a responder, y las páginas cargan lo nuevo de inmediato. Una página que usted nunca abrió en este dispositivo no tiene nada guardado: dice *rendimiento no responde*, y regresa sola cuando responda.

Nunca se guardan: los registros en vivo y los flujos de eventos, el inicio de sesión, `/mcp`, `/healthz`, nada que no sea una simple lectura. Las copias son a lo más 200 respuestas y de hasta 7 días.

## Lo que se apaga, y por qué {#what-is-switched-off-and-why}

Sin conexión, toda acción que cambia algo se apaga y dice por qué: Construir ahora, probar una rama, Reintentar, Cancelar, Integrar esta rama, Revertir, Actualizar, Descartar, guardar los ajustes, agregar a la Ruta, enviar un comentario. Cualquier otra cosa que enviaría algo falla de inmediato con *Sin conexión: no se envió nada*.

**Nunca se guardan para enviarse después.** Una orden de desplegar que sale sola veinte minutos más tarde, cuando ya cambió todo, es peligrosa: usted la vuelve a dar cuando regrese la conexión, sabiendo lo que hay entonces.

## Borradores {#drafts}

Lo que usted está escribiendo es lo único que se guarda para después: un comentario al equipo, la respuesta del equipo a un comentario, una nota de la Ruta. Se guarda en este dispositivo mientras escribe (*Borrador guardado en este dispositivo*), por caja y por persona que inició sesión, regresa a su caja cuando la vuelve a abrir, y se borra en cuanto se envía. Nunca se envía solo.

## Una versión nueva {#a-new-version}

Cuando se actualizó rendimiento, la página dice **Hay una versión nueva de rendimiento** con un botón **Volver a cargar**, y la carga cuando usted decida, nunca a mitad de lo que está haciendo. Después de **Entorno → Actualizar**, la página se vuelve a cargar sola en la versión nueva, como antes.

## Instalarla en el teléfono {#installing-it-on-a-phone}

rendimiento se puede instalar como aplicación, con su icono: en Android, el menú de Chrome → **Instalar aplicación** (o **Agregar a la pantalla principal**); en un iPhone, **Compartir** de Safari → **Agregar a inicio**. Abre en una ventana propia, sin la barra del navegador.

## Computadoras compartidas {#shared-computers}

Lo que guardó el navegador se borra cuando usted cierra sesión, cuando entra otra persona o cuando cambia de organización: los datos de una persona nunca se le muestran a la siguiente.

## Apagarlo {#turning-it-off}

Quien opera la plataforma puede apagarlo todo sin una construcción nueva: ponga `SERVICE_WORKER=off` ([ajustes](../reference/config.md#core)) y reinicie. Cada navegador borra entonces lo que guardó y deja de guardar en su siguiente visita; la página abre solo con conexión, como antes.

Funciona solo en direcciones `https://` y en esta computadora (`localhost`), lo que incluye rendimiento en una sola computadora y su ventana de escritorio. El código de la página está en `web/src/trabajador/` (el trabajador) y `web/src/conexion.ts` (el aviso).
