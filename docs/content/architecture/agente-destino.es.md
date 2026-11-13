# El agente de destino

La arquitectura A de la decisión 95: rendimiento aloja el panel y la API, y en cada clúster o computadora corre un **agente** que solo se conecta hacia *fuera*. El agente es una implementación más de [`Destino`](../reference/code-map.md) (`internal/destino`), como Argo CD administrado por Akuity: la plataforma construye, prueba y decide; el agente lo hace correr y dice cómo le fue. Nada abre un puerto del lado del destino.

Esta página es el diseño (paso 11 del plan). El prototipo, con una computadora de la casa como destino del panel alojado, es la última sección; lo escrito aquí es lo que el prototipo debe cumplir.

## La forma {#the-shape}

```text
  rendimiento (alojado)                      una computadora de la casa
 ┌──────────────────────┐   wss, solo salida ┌─────────────────────────┐
 │ panel + API          │◀───────────────────│ rendimiento agente      │
 │ destino.Remoto       │   órdenes ───────▶ │  └─ destino.Local       │
 │  (uno por agente)    │   ◀── resultados   │     (motor, Caja, proxy)│
 └──────────────────────┘                    └─────────────────────────┘
```

- **`destino.Remoto`** vive en la plataforma. Implementa `Destino`; cada método se vuelve una orden que se envía al agente del destino de la aplicación, y espera el resultado.
- **El agente** es el mismo binario (`rendimiento agente`). Marca hacia la plataforma y ejecuta cada orden en un `Destino` *local*: `destino.Local` en una computadora con motor de contenedores; después uno de Kubernetes, para los clústeres.
- El agente es el único que marca. Si la conexión se cae, reintenta con espera creciente; la plataforma nunca intenta llegar al agente.

## Protocolo {#protocol}

**WebSocket sobre HTTPS (`wss`), una conexión larga por agente**, en el mismo puerto HTTPS de la plataforma (`/agente/v1/conectar`). Se eligió sobre gRPC porque pasa por lo que deje pasar una red doméstica, un proxy corporativo o una CDN (443 normal, actualización de HTTP/1.1), no pide un oyente ni un certificado nuevos en la plataforma y los mensajes son pequeños.

Los mensajes son tramas JSON con un `id`, un `tipo` y un cuerpo:

| Dirección | Trama | Qué |
|---|---|---|
| agente → plataforma | `hola` | la primera trama: versión del agente, rango de protocolo, capacidades (`local`, `kubernetes`), versión del motor, lectura del reloj |
| plataforma → agente | `orden` | `id`, `metodo` (`crear`, `aplicar`, `estado`, `tarea`, `quitar`, `situacion`, `recursos`, `reiniciar`, `impacto`, `desconectar`, `llaves`, `guardar_secreto`), `app`, el cuerpo |
| agente → plataforma | `resultado` | el `id` de la orden, `ok` o un `error` con un texto para personas |
| agente → plataforma | `evento` | lo que cambia sin que se pida: una versión queda sana o falla, un servicio muere, líneas de la bitácora de una tarea |
| ambos | `latido` | cada 20 s; con tres perdidos la conexión se da por muerta |

Los métodos de `Destino` corresponden uno a uno con las órdenes, así que pasar después a gRPC cambia el transporte y no el diseño. La plataforma guarda lo último que dijo el agente (`Situacion`, `Estado`) para que la página de aplicaciones responda aunque el agente esté desconectado, marcado como *visto por última vez a las …*.

**Las órdenes son idempotentes y llevan su número de versión.** `aplicar` de la versión *n* dos veces es lo mismo que una. Una orden que encuentra al agente desconectado espera en una cola con plazo (el tiempo límite del propio despliegue); al reconectar, la plataforma vuelve a enviar el **estado deseado** (la versión actual de cada aplicación de ese destino) y el agente converge, así que una orden perdida nunca se pierde y una vieja nunca pisa una versión más nueva.

## Credenciales por agente {#credentials}

- **Una persona da de alta al agente** desde el panel (solo administradores): *Destinos → Agregar computadora* muestra un código de alta de un solo uso, válido 15 minutos, con el comando por ejecutar. El agente cambia el código por su propia **credencial**: un token aleatorio de 256 bits, mostrado al agente una sola vez, guardado por la plataforma solo como hash (igual que las llaves de agente) y escrito por el agente en un archivo con modo `0600`.
- Cada credencial nombra a **un agente de una organización**. Rotar emite un token nuevo por la conexión viva y el anterior muere tras el cambio; revocar corta la conexión al instante y queda en la [bitácora](security.md#the-bitacora).
- La credencial nunca llega al navegador, a los registros ni a la Ruta. Autoriza la conexión y nada más: no puede leer la API del panel.
- Después: certificados de cliente (mTLS) firmados por la plataforma, para que un archivo robado no baste sin la llave del equipo. El token es el primer paso, no el final.

## `org_id` desde el primer día {#org-id}

Las organizaciones ya separan los datos (vea el [Modelo de seguridad](security.md)); un agente no debe ser el hueco.

- La tabla de destinos lleva `org_id` desde su primera migración, y cada aplicación apunta a un destino de **su propia organización**. Un destino pertenece a una sola organización; para usar una computadora con dos organizaciones se corren dos agentes.
- La plataforma toma el `org_id` **de la credencial**, nunca de una trama. El agente no puede decir para quién trabaja, y no puede recibir una orden sobre una aplicación que no sea de su organización y su destino; `destino.Remoto` lo comprueba antes de escribir la trama, y el manejador de la conexión lo vuelve a comprobar al leer resultados y eventos.
- Los resultados y eventos se escriben solo en las filas de esa organización; la entrada de la bitácora de cada orden lleva el `org_id` y el agente.
- Las pruebas que lo acompañan: las mismas pruebas de barda entre dos organizaciones que las de las herramientas MCP, con un agente de la organización B al que se le pide aplicar una aplicación de A (no debe encontrar nada), y un `evento` que nombra una aplicación ajena (debe descartarse y anotarse).

## Desfase de versiones entre panel y agente {#version-skew}

La plataforma la actualiza una persona en un momento; los agentes de las computadoras de la casa no. Deben poder ser más viejos (o más nuevos, tras una reversión) durante un tiempo.

- Dos números en `hola`: la **versión** del agente (para personas) y su **rango de protocolo** `[min, max]` (para la máquina). El panel admite el protocolo **actual y el anterior**; habla el más alto que ambos conozcan.
- Los cambios al protocolo solo **agregan**: campos opcionales nuevos, órdenes nuevas. Una orden que el agente no conoce se responde con `error: unsupported`, y la plataforma lo dice en la aplicación: *el agente de esta computadora es muy viejo para esto; actualícelo*. Un campo desconocido se ignora.
- La **especificación** (`rendimiento.yaml` como la entiende la plataforma) se envía al agente ya resuelta dentro de la versión; el agente no lee el repositorio. Una versión que usa algo que las `capabilities` del agente no tienen se rechaza *antes* de aplicarse, con el motivo.
- La página de destinos muestra la versión de cada agente y una marca cuando va por detrás de la plataforma. La autoactualización está **apagada** de forma predeterminada (una persona instala el agente, así que una persona lo actualiza); encendida, es una versión firmada del mismo canal que la de la plataforma.
- Fuera del rango admitido, la plataforma rechaza la conexión con un texto que dice qué versión instalar.

## Qué se queda en el agente {#what-stays}

| Se queda en el agente | Por qué |
|---|---|
| **Los valores de los secretos** | `guardar_secreto` envía un valor una sola vez por el canal, y el agente lo sella en su propia `Caja`. La plataforma guarda solo los *nombres* de las llaves (lo que responde `LlavesSecreto`) y nunca vuelve a leer un valor. Reiniciar una aplicación tras un cambio es la orden `reiniciar`. |
| **Las credenciales del motor y de la aplicación** | la credencial para jalar del registro, las de su Postgres y su Redis (hechas una vez, en el agente, como ya lo hace `Local`). |
| **Las imágenes** | la plataforma nunca envía bytes de imagen. Una versión nombra una imagen por su digest; el agente **la jala él mismo** y verifica el digest. Las construcciones se quedan en el grupo de BuildKit de la plataforma. |
| **Volúmenes y datos** | los datos de la aplicación no salen de la computadora; los respaldos son una orden posterior, hacia un destino que la organización elija. |

Lo que esto implica para el registro: una computadora de solo salida debe poder *llegar* al registro. En el prototipo es el registro de la plataforma por HTTPS con una credencial de solo lectura por agente; un registro que solo está en la red local de la plataforma no sirve desde fuera y pide uno público o un espejo de caché. Es una decisión para la persona.

Como la plataforma nunca tiene los valores, perder un agente significa volver a capturar los secretos de la aplicación; el panel dice qué llaves faltan.

## Llegar a las aplicaciones {#reaching-the-apps}

«Sin puertos abiertos» se refiere al camino de *control*: ningún puerto en el router para que rendimiento entre. Las aplicaciones mismas:

- en la red de la casa, el proxy del agente escucha en la dirección de la computadora en la red local y las aplicaciones están en nombres del estilo `<app>.<computadora>.lan`, como `Local` lo hace con `.localhost`;
- desde fuera, un túnel de salida (un paso posterior, con su propio diseño) llevaría el tráfico de la misma manera.

## El prototipo {#the-prototype}

Una computadora de la casa (una máquina de la capa privada, que aquí no se nombra) es el destino del panel alojado. Lo que hace falta, en el orden para construirlo:

1. `internal/agente`: las tramas del protocolo como tipos de Go, con una prueba de que las tramas viejas aún se decodifican.
2. La tabla de destinos (`org_id`, nombre, tipo, hash de la credencial, versión, última vez visto) y el punto de alta; la migración solo agrega.
3. `destino.Remoto`, y `rendimiento agente` corriendo `destino.Local` detrás de la conexión; probado primero contra un agente falso.
4. La elección del *destino* de la aplicación en el panel y en `spec`, con un ejemplo en `internal/spec/ejemplos`.
5. La prueba: una aplicación del panel alojado se despliega en la computadora **sin abrir ningún puerto**, se mata al agente a la mitad y se reconecta, y la aplicación de otra organización sigue fuera de alcance.

**Queda cuando** una aplicación del panel de rendimiento.ai alojado se despliega en una computadora de la casa sin abrir puertos. Esta página es el primer paso; los pasos 1 a 5 siguen pendientes.

## Decisiones abiertas {#open-decisions}

- WebSocket o gRPC de forma definitiva (esta página dice WebSocket para el prototipo, con la puerta abierta).
- Dónde vive el registro para que una computadora de solo salida llegue a él.
- Si el agente de un clúster de Kubernetes corre dentro del clúster con su propia cuenta de servicio (mínimo privilegio, como el de Argo CD) o desde fuera.
