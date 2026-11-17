# El vigía

Una vez por semana, **el vigía** lee lo que la plataforma registró y les dice a sus administradores las pocas cosas que merecen su tiempo. Es uno más de los [agentes dentro de la plataforma](agents.md) y trabaja como ellos: por la [pasarela de IA](ai.md), dentro de un tope, con cada llamada en la página IA. Él propone; una persona decide.

Sus hallazgos están arriba en la página [Registro](registro.md), bajo **Lo que vio el vigía**, para los administradores de la plataforma. Cuando llegan los de una semana nueva, la campana lo avisa.

## Lo que lee {#what-it-reads}

La semana, y las tres semanas anteriores para comparar:

- **Pasos:** cuánto tarda cada paso de cada aplicación, cuántas veces es el último de su ejecución en terminar (así que decide cuánto dura la ejecución), cuántas veces falla, corre más de una vez, o falla y luego pasa con la misma confirmación (inestable).
- **Aplicaciones:** las ejecuciones de cada aplicación, las fallidas y las canceladas; una aplicación cuyas ejecuciones pasaron todas es una buena noticia, y también lo dice.
- **El registro:** sus advertencias y errores, agrupados por mensaje, con cuántas veces, en cuántos días y si son nuevos.
- **Solicitudes lentas:** las rutas de la API que tardaron más de un segundo en responder.
- Los **problemas** que siguen abiertos.
- **El uso de la interfaz** (cuando se mide, vea [Uso de la interfaz](uso.md)): páginas que tardan en mostrar sus datos, y páginas que nadie abrió en dos semanas.

Todo se suma primero, en la base de datos, en una lista corta de señales, cada una con una clave estable: lo mismo da la misma clave la semana siguiente. El modelo lee solo esa lista.

## Lo que nunca manda {#what-it-never-sends}

El modelo lee conteos, tiempos, nombres de pasos y de rutas, y los mensajes del registro (la plantilla de la línea, como *could not reach the registry*). Nunca los atributos de una línea, el contenido de un repositorio, el registro de una ejecución ni lo que escribió la gente (comentarios, encargos, la Ruta). Un mensaje que parece un secreto se deja fuera por completo, y a los demás se les quitan las cadenas largas (identificadores, huellas).

## Un hallazgo {#a-finding}

Cada hallazgo es un título y una línea de evidencia: cuántas veces, dónde, desde cuándo y, cuando pide trabajo, qué ayudaría.

> **El paso interfaz-es tarda 11 min y decide cuánto dura cada ejecución**
> 42 ejecuciones · siempre el último en terminar · partirlo en dos lo bajaría a unos 6 min

Guarda a lo más cinco, menos cuando importan menos. Cada uno tiene tres botones:

- **Volverlo encargo:** un [encargo](ruta.md) con el hallazgo de título y la evidencia en su detalle, hecho por usted.
- **Anotarlo en la Ruta:** un pendiente en la Ruta (o una nota, para un dato), con la etiqueta `vigia`, escrito por usted.
- **No por ahora:** se va, y no se vuelve a proponer.

Un hallazgo que es solo un dato, o una buena noticia (*Nadie abrió Mercado en 14 días · dato de uso · sin acción sugerida*), no sugiere trabajo: ofrece solo **Anotarlo en la Ruta** y **No por ahora**.

Nada se vuelve encargo ni entra a la Ruta sin el sí de una persona.

## Lo que recuerda {#what-it-remembers}

Lo que la gente decidió no se vuelve a proponer: un hallazgo dejado para después, nunca; uno que se volvió encargo o nota, no en 90 días. Lo recuerda por la clave de la señal, así que la señal ni siquiera se le muestra al modelo; y le nombra al modelo lo que se decidió, para que no proponga lo mismo con otras palabras.

## Cuándo corre y cuánto cuesta {#when-and-cost}

- **Cada semana**, solo, mientras esté encendido. Después de una revisión que falló (ningún modelo respondió, se gastó el tope) lo intenta de nuevo un día después.
- **Revisar ahora**, en la página Registro, pide una en ese momento; una a la vez.
- Una semana sin nada que contar no le pregunta a ningún modelo.
- Cuesta dinero de IA cada vez: alrededor de un centavo con Claude Haiku, nada con el modelo local. Sus llamadas van firmadas *agent: lookout* en la [página IA](ai.md), con las demás.
- En la página IA, **El vigía** tiene su interruptor (**Revisar cada semana**) y su **tope mensual** (50 centavos si usted no lo cambia). Cuando se gasta el tope del mes, espera al mes siguiente; una revisión pedida a mano también cuenta. El tope de IA de la organización sigue valiendo.

Escribe en el idioma de la plataforma (`NOTIFY_LANG`), como los demás agentes.
