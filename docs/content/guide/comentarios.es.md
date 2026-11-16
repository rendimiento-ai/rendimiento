# Comentarios (escribirle al equipo) {#comments}

Cualquiera puede escribirle al equipo de rendimiento: una idea, algo que no funcionó, una pregunta. Una persona del equipo lee cada comentario y lo responde con sus propias palabras, con su nombre. La IA de la plataforma solo ordena los comentarios y redacta un borrador para que esa persona lo reescriba; nada sale nunca solo. El diseño y sus razones están en [Comentarios (diseño)](../future/comentarios.md) (decisión 145 de la Ruta).

## Escribir {#writing}

- **Desde una instalación:** **Escribirle al equipo**, al pie de cada página (o en el menú del icono de Windows), abre `/escribir`. Antes de enviar, **Exactamente lo que se enviará** muestra el JSON tal como saldrá: su texto, su idioma, y la versión, el modo, el motor y el sistema de esta instalación. Nunca su nombre, sus aplicaciones ni la identidad de la instalación. Con él sale la huella de una ficha al azar que se queda en su computadora: solo con esa ficha su instalación puede recoger la respuesta.
- **Sin cuenta:** el formulario de `https://rendimiento.ai/comentarios` (enlazado desde la página de bienvenida) pide su correo, solo para responderle.
- Un comentario tiene hasta 2,000 caracteres. rendimiento.ai acepta tres por hora de un mismo remitente y 200 al día en total.

Un comentario desde una instalación sale por el camino de las [señales de vida](senales.md), así que `SENALES=off`, o apagar las señales en esa página, también lo apaga; la página entonces señala el formulario público.

## La respuesta {#the-answer}

- **En la aplicación:** cada tres horas una instalación pregunta por las respuestas que le faltan, con la ficha de cada comentario. Cuando llega una, la campana dice **El equipo respondió su comentario** hasta que abre `/escribir`, donde cada comentario suyo muestra su respuesta.
- **Por correo:** la respuesta a un comentario del formulario sale una sola vez, de parte de la persona que la escribió, con el comentario al que responde. A esa dirección no se le envía nada más.

## Lo que ve el equipo {#what-the-team-sees}

Donde se reciben los comentarios (`SENALES_RECIBIR=1`, rendimiento.ai), los operadores de la plataforma tienen **Comentarios** (`/buzon`), con la cuenta en la campana:

- **Por revisar, Respondidos, Descartados:** cada comentario con de dónde vino y, si es del formulario, el correo.
- **Lo que entendió la IA:** su clase (*Ya existe*, *Nuevo*, *Algo falla*, *Sin sentido*), un resumen de una línea, las páginas del libro, entradas de la Ruta y comentarios anteriores con los que coincide, y el comentario anterior que pide lo mismo. La cuenta de un grupo (*3 personas piden esto*) es la evidencia de demanda.
- **El borrador,** para reescribirlo. **Enviar la respuesta** pregunta una vez más y luego la envía con su nombre. También puede cambiar la clase, unirlo a otro comentario, **Ordenarlo de nuevo** o **Descartar** (su correo se borra en ese momento).
- **Pasarlo a la Ruta,** cuando vale la pena guardarlo: se vuelve una idea en la Ruta (un pendiente con las etiquetas `idea` y `comentario`), con su nombre, las palabras de quien escribió citadas y el resumen de la IA. Si aún no se responde, la IA redacta de nuevo la respuesta, ahora diciéndole que su idea está en la lista del equipo, sin prometer fecha. Una vez por comentario; uno ya respondido también se puede pasar, y conserva lo que se envió.

La IA solo ve el texto del comentario, como dato: lo que un comentario le pida hacer no cambia nada. Los correos del formulario se guardan aparte del texto, solo se ven en esta página y se borran 30 días después de la respuesta.
