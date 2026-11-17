# El registro

Cada proceso de rendimiento (`api`, `control`, `worker`, `agents`, `traces`, o el único proceso en [una computadora](../start/quickstart.md)) va escribiendo lo que hace. Esas líneas antes vivían solo en la salida de cada proceso y se perdían cuando se reiniciaba. Ahora cada línea de nivel **INFO o superior** también se guarda en la base de datos, y la página **Registro** las muestra todas juntas. Es para los administradores de la plataforma: ábrala desde la página de [Problemas](problems.md) (**El registro completo de la plataforma**) o en `/registro`.

## Lo que se guarda {#what-is-kept}

- Cada línea de nivel INFO, WARN o ERROR: cuándo, su nivel, el proceso que la escribió, su mensaje, la aplicación y la ejecución de las que trata (cuando lo dice) y sus demás valores.
- Dos líneas que nadie escribía, de la API: una solicitud que tardó **más de un segundo** (INFO, *slow request*) y una que **falló con un 5xx** (WARN, *request failed*). Dicen el patrón de la ruta (`GET /api/apps/{app}`), nunca la dirección misma, su consulta, su cuerpo ni sus encabezados; las transmisiones en vivo (la página de una ejecución mientras corre) nunca son lentas. Un 500 también se vuelve un problema en la página de Problemas, uno por ruta; los demás 5xx (un servicio del que se depende que no respondió, una página que esta computadora no tiene) se lo dicen a la persona y quedan solo en el registro.
- Las líneas se guardan **30 días** y luego se borran.
- Escribir una línea nunca detiene el trabajo que la escribió. Si la base de datos va atrasada o no responde, las líneas se descartan y se cuentan; cuando vuelve a responder, una línea dice cuántas no se guardaron (*log: some lines were not kept*).

## Lo que se quita {#what-is-removed}

Los secretos nunca llegan al registro. Antes de guardar una línea:

- un valor o un mensaje que parece una credencial se reemplaza por *[removed: looked like a secret]*. La revisión es la misma con la que [la Ruta](ruta.md) rechaza secretos: fichas al estilo de GitHub, AWS, Slack y Anthropic, las llaves de agente de rendimiento, fichas firmadas, llaves privadas y `password: …` con un valor que parece aleatorio;
- se reemplaza el valor de todo atributo cuyo nombre dice que guarda una credencial (`token`, `secret`, `password`, `authorization`, `cookie`, `key`, `private`…), sea cual sea;
- se quitan el usuario y la contraseña de una dirección (`postgres://usuario:contraseña@…`);
- un mensaje se corta a los 2,000 bytes, cada valor a los 1,000, y una línea guarda a lo más 40 valores (8,000 bytes); lo que queda fuera se cuenta.

## Cómo leer la página {#reading-the-page}

- Las líneas más recientes van primero; **Ver líneas anteriores** trae las siguientes.
- Filtre por **proceso**, **aplicación**, **nivel** (todo, avisos y errores, o solo errores) y **periodo** (la última hora, 24 horas, 7 o 30 días, **hasta** el momento que elija), y busque en los mensajes.
- Una línea con valores se abre con un clic: sus atributos y un enlace a su ejecución.
- Los avisos y errores también se agrupan, se cuentan y se siguen hasta que se arreglan en la página de [Problemas](problems.md); el registro es donde se ve lo que pasó alrededor de ellos.
- Una vez por semana, [el vigía](vigia.md) lee este registro, las ejecuciones y el uso de la interfaz, y propone unos pocos hallazgos arriba en la página.
