# Dormir y apagar (en una computadora)

rendimiento [en una computadora](../environment/deploy.md#on-one-computer) puede dormirse cuando usted no lo necesita, y despertar como estaba. Dormido, detiene los contenedores de sus aplicaciones y la CI; los datos, las versiones y la Ruta se quedan como están. **Apagar del todo** además detiene a rendimiento mismo, hasta que la computadora vuelva a prender.

En un clúster nada de esto aparece: un clúster atiende a otras personas y se queda despierto (el servidor responde *409* a estas rutas).

## Dormirlo {#putting-it-to-sleep}

Los administradores de la plataforma ven en la barra superior un botón **Dormir** con una luna. Abre un diálogo que dice lo que hace dormir, y pregunta dos cosas:

- **¿Cuándo?**, solo cuando se está publicando una versión. **Al terminar la publicación** (lo predeterminado) la espera, y la aplicación despierta en la versión nueva; las ejecuciones de ramas se detienen de inmediato. **Ya** corta también la publicación: la aplicación se queda en la versión que tiene, y la nueva se publica al despertar.
- **Despertar solo a las** una hora (apagado al principio, 08:00 al encenderlo): la siguiente vez que el reloj llega a esa hora después de dormirse, despierta solo.

Mientras se duerme, y luego mientras duerme, una línea bajo la barra superior dice desde cuándo, cuántos avisos esperan y cuándo despierta; un administrador lo despierta desde ahí con **Despertar ahora**.

Lo que hace dormir:

- **Ejecuciones.** Ya no se toma nada de la cola. Una ejecución en curso se corta y vuelve a la cola desde su primer paso: **no se marca fallida**, su comprobación en la forja dice que espera, y se repite al despertar. Los envíos nuevos se encolan y esperan también.
- **Las aplicaciones.** Se detiene (no se borra) cada contenedor de sus aplicaciones, de sus Postgres y Redis y de las copias de las solicitudes: los volúmenes y las versiones se quedan. Los contenedores propios de rendimiento (su base de datos, su registro de imágenes y BuildKit) siguen corriendo.
- **Avisos web.** Lo que GitHub, GitLab o Bitbucket envían mientras tanto se revisa como siempre (su firma) y se guarda; al despertar se atiende en el orden en que llegó. En una computadora sin túnel las forjas no pueden enviar avisos web, y rendimiento les pregunta en cambio por commits nuevos cada minuto: dormido deja de preguntar, y la primera mirada al despertar encuentra lo que se movió.
- **Visitantes.** El proxy de rendimiento (`http://<app>.localhost:8000`, y lo que le llegue por un túnel) responde *Volvemos pronto* (*Back soon* a un navegador en inglés) con *503* y `Retry-After`, para que los buscadores y los monitores sepan que es por un rato.
- **El vigía.** Las horas dormidas se guardan, y [el vigía](vigia.md) deja fuera las líneas del registro escritas en ellas: las aplicaciones estaban dormidas, no rotas.
- **Un reinicio.** El estado se guarda en la base de datos. Si la computadora se reinicia mientras rendimiento duerme, los contenedores de las aplicaciones arrancan con ella (se reinician con la computadora), y rendimiento, que arranca dormido, los vuelve a detener.

## Despertarlo {#waking-it}

**Despertar ahora** (o la hora que se le dio) arranca los mismos contenedores, o sea las mismas aplicaciones en las mismas versiones; atiende los avisos que guardó, en orden; y suelta la cola, así que las ejecuciones que cortó se repiten. Luego la página muestra lo que hizo: las aplicaciones prendidas, la publicación que esperó, los avisos atendidos y las ejecuciones que se repiten.

## Apagar del todo {#shutting-down-completely}

**Apagar del todo** se duerme como arriba (esperando una publicación, o no) y luego detiene rendimiento: bajo el servicio del instalador le pide a systemd que lo detenga (`systemctl --user stop rendimiento`; en la distribución de WSL de la aplicación para Windows, su servicio del sistema), así que no vuelve a arrancar hasta que la computadora prenda; iniciado a mano, el proceso termina. Cuando vuelve a arrancar con la computadora, despierta solo.

## La tapa de una laptop {#on-a-laptops-lid}

*Dormir al cerrar la tapa* le toca a la aplicación de escritorio, que es la que puede ver la tapa o que el sistema se suspende; todavía no está, así que la opción no se muestra. rendimiento guarda el ajuste (`tapa`, apagado) para ella. Mientras tanto, una computadora que se suspende simplemente pausa todo, y rendimiento sigue donde estaba cuando vuelve.

## Para agentes y programas {#for-agents-and-scripts}

Solo los administradores de la plataforma, con su sesión, pueden dormirlo, despertarlo o cambiar sus opciones; las llaves de agente (MCP) no. Cada cambio queda en la [bitácora](../architecture/security.md#the-bitacora).

| Ruta | Quién | Qué |
| --- | --- | --- |
| `GET /api/dormir` | cualquiera con sesión | cómo está: `estado` (`despierto`, `durmiendose`, `dormido`), desde cuándo, los avisos que esperan, las ejecuciones en curso, las publicaciones que esperaría, el siguiente despertar y el resumen del último |
| `POST /api/dormir` `{"ya": false, "apagar": false}` | administradores | dormir (`ya`: sin esperar; `apagar`: apagar del todo) |
| `POST /api/despertar` | administradores | despertar ahora |
| `PUT /api/dormir/opciones` `{"despertarA": "08:00", "tapa": false}` | administradores | la hora de despertar (`""`: apagada) y el ajuste de la tapa |

El código está en `internal/platform/dormir.go` (los estados), `internal/destino/local_dormir.go` (los contenedores y la página del proxy), `internal/api/dormir.go` y `web/src/components/dormir.tsx`.
