# El ayudante (Tsïntsuni, el colibrí) {#the-helper}

rendimiento tiene muchas páginas y ajustes. Su ayudante contesta preguntas sobre ellos: dónde está un ajuste, qué hace una página, por qué falló una ejecución, cómo hacer algo. Contesta breve, con enlaces a dónde ir. Es un colibrí: *tsïntsuni* en purépecha, como en Tzintzuntzan, «el lugar de los colibríes», en Michoacán.

## Preguntarle {#asking-it}

Un botón redondo con el colibrí está abajo a la derecha en todas las páginas (en el teléfono, encima del menú de abajo). Abre un panel al lado de la página (una hoja desde abajo en el teléfono). Escriba la pregunta, o toque una de las rápidas.

- **Sabe dónde está usted.** La pregunta va con la página desde la que preguntó. En la página de una ejecución también lee la ejecución: su rama, su estado, los pasos que no pasaron y la explicación del diagnosticador, solo de una aplicación que usted puede ver.
- **Contesta con enlaces.** Un enlace a una página de rendimiento la abre ahí mismo (y cierra la hoja en el teléfono); uno a una página de este libro se abre en otra pestaña. Solo enlaza a páginas que existen: la plataforma revisa cada enlace contra el mapa de sus páginas (con las aplicaciones que usted puede ver) y el índice del libro, y quita los demás.
- **Dice qué leyó**: la página, y las páginas del libro que coincidieron con la pregunta.
- **Solo lee y señala.** No cambia nada; si se le pide hacer algo, dice dónde puede hacerlo usted.

## Qué IA contesta {#which-ai}

Contesta primero el modelo local (Ollama, configurado en la [página de IA](ai.md)): es gratis y la pregunta no sale de casa. Cuando no convence, el ayudante ofrece **Preguntarle a la IA de la plataforma**, que manda la misma pregunta al modelo de la plataforma, por más o menos lo que dice el botón, dentro del tope mensual de la organización. Sin modelo local usa la IA de la organización.

## Qué guarda {#what-it-keeps}

La conversación de cada persona es solo suya: nadie más la ve, tampoco los administradores. Se guarda 7 días, y lo más viejo se borra. Las llaves de agente no pueden preguntarle: es para personas con sesión.

## En el código {#in-the-code}

| Qué | Dónde |
|---|---|
| El ayudante, su mapa de páginas y cómo revisa los enlaces | `internal/agentes/ayudante.go` |
| `GET /api/ayudante`, `POST /api/ayudante` y lo que lee de la página de una ejecución | `internal/api/ayudante.go` |
| Las conversaciones, 7 días | `internal/store/ayudante.go`, migración `0070_ayudante.sql` |
| El botón y el panel | `web/src/components/ayudante.tsx` |
