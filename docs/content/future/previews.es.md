# Entornos de vista previa {#preview-environments}

**Estado: aprobado (decisión 136), rebanadas 1 a 3 construidas.** `previews:` ya está en [rendimiento.yaml](../guide/spec.md#top-level). Cuando pasa la ejecución de una rama que tiene una solicitud de incorporación abierta del propio repositorio de la aplicación, su copia se escribe como el objeto App `<app>-pr-<número>` con las imágenes que construyó esa ejecución (nada se construye dos veces), y un check **preview** en el commit enlaza a la dirección de la copia (`https://pr-<número>.<dominio>`, o `<nombre>-pr-<número>.<zona>` para un subdominio, para que el certificado comodín de un proxy la cubra). La copia se va cuando la solicitud se cierra o se integra (el webhook), y una barrida cada 10 minutos borra las copias de solicitudes cerradas y las que pasan `keep` sin envíos; si se pasa de `max`, se va primero la que lleva más tiempo sin envíos. Un solo comentario en la solicitud, editado en cada envío, da la dirección, el commit y el día en que se va la copia, y dice cuándo y por qué se borró. Las copias corren en un clúster, no en una computadora. Sigue el agente (rebanada 4). p0dxD respondió las preguntas abiertas: la app de propinas si tiene pruebas de navegador (si no, ping-demo), hasta el 10 % del clúster con 3 copias por app, y **sin `copy`** de los datos de producción por ahora. Esta página es la decisión 95, paso 12 del plan: cada rama o solicitud de incorporación tiene su propia copia en vivo, con su propia dirección, y un agente la recorre y comenta en la solicitud antes de que una persona la integre. Nada de esto corre hoy; dice qué construir, en qué orden y cuánto cuesta. La [hoja de ruta](roadmap.md) apunta aquí.

## Lo que ve una persona {#what-a-person-sees}

En una solicitud de incorporación de una aplicación que pidió vistas previas (`previews:` en `rendimiento.yaml`, apagado por omisión):

1. Una comprobación en la solicitud, **vista previa**, cuyo enlace es la dirección de la copia (`https://pr-42.<dirección de la app>`).
2. Un solo comentario, **conservado y editado** (nunca uno nuevo por cada envío): la dirección, el commit que muestra, cuándo se borrará y lo que encontró el agente.
3. Cuando la persona integra o cierra la solicitud, la copia desaparece y el comentario lo dice.

## Una copia por solicitud {#one-copy}

- **Dónde vive.** Un espacio de nombres `<app>-pr-<número>`, no contenedores junto a los de la aplicación: `render.Render` ya recibe el espacio de nombres aparte del nombre de la aplicación (`Input.Namespace`), así que se generan los mismos objetos de la versión para la copia, con su propio espacio de nombres, un prefijo `pr-<número>` en la dirección, una réplica por servicio y la etiqueta `rendimiento.ai/preview=<número>`. Un espacio de nombres da gratis la política de red, la cuota y el borrado: al borrarlo se borra todo.
- **Qué solicitudes.** Las del repositorio de la propia aplicación. La solicitud de un fork nunca se construye (su código correría con el token de clonado), así que nunca tiene copia.
- **La imagen.** La que la ejecución de la rama ya construyó y pasó. No hay una segunda construcción: la vista previa se despliega cuando pasan los pasos de la ejecución, como una versión, pero nunca toca el objeto `App` real de la aplicación, su historial, su verificación ni sus reversiones.
- **El disparador.** El manejador del webhook de GitHub (`internal/api/server.go`) hoy ignora los eventos `pull_request` porque un envío ya corre la rama. Debe suscribirse a `pull_request` (abierta, reabierta, sincronizada, cerrada) y pasarlos a `platform`, que busca la ejecución del commit de la punta. La App de GitHub ya tiene el permiso *pull requests: write* que necesita para comentar.
- **La dirección.** `pr-<número>.<dirección de la app>`, en las zonas DNS que la aplicación ya tiene, revisada como cualquier dirección (no debe chocar con la de otra aplicación). Sin una zona con comodín, no hay vista previa: la página de la aplicación dice por qué.

## Datos {#data}

Tres opciones, en este orden, una por aplicación en `previews.data`:

| `data:` | Qué tiene la copia | Cuándo usarla |
|---|---|---|
| `empty` (la predeterminada) | Una base de datos nueva y vacía hecha por el `needs: [postgres]` de la aplicación, con sus migraciones corridas por las tareas previas al despliegue | Casi siempre: nada real puede filtrarse y no ocupa espacio |
| `seed` | Lo mismo, y luego una tarea de la aplicación (`previews.seed`) que carga datos de ejemplo | Aplicaciones cuyas páginas están vacías sin datos (una de propinas necesita personas, lugares y algunas propinas) |
| `copy` | Una copia del último respaldo nocturno de la aplicación, con vaciadas las columnas que la aplicación liste en `previews.scrub` | Solo cuando una base vacía o con datos de ejemplo no puede mostrar el cambio; nunca sin `scrub`, y la persona lo activa en Ajustes |

Los secretos de la copia son los secretos de **vista previa** de la aplicación, nunca los de producción: una aplicación que necesita una llave de pagos usa ahí su llave de prueba. Redis (`needs: [redis]`) siempre está vacío. Los volúmenes no se copian.

## Cuándo se borran {#when-deleted}

- Al **cerrar o integrar** la solicitud, de inmediato (el espacio de nombres y el registro DNS).
- A los **7 días sin envíos** (`previews.keep`, hasta 30), para que una solicitud abandonada no conserve una copia para siempre.
- Como máximo **3 copias vivas por aplicación** (`previews.max`): la siguiente reemplaza a la más inactiva y lo dice en su comentario.
- Un ciclo en `platform`, como `mergeLoop`, borra todo espacio de nombres con la etiqueta de vista previa cuya solicitud esté cerrada o ya no exista, para que un webhook perdido no deje una copia olvidada.

## Qué hace el agente {#what-the-agent-does}

Cuando la copia responde, corre un **agente verificador**, como uno de los agentes de la plataforma (o sea, por la pasarela de IA, dentro del tope de la organización y registrado en la Ruta):

1. **Con las pruebas de navegador de la aplicación.** Si las tiene (las de tipo `web/e2e`, Playwright), corren como una tarea contra la dirección de la copia, en la imagen de Playwright, con el usuario de prueba de la propia copia. Son la primera opción porque son deterministas y no gastan IA.
2. **Con pruebas generadas (paso 13).** Cuando la aplicación no las tiene, o el cambio toca páginas que no cubren, el agente genera un recorrido a partir del cambio y de las páginas que alcanza, lo corre con la misma imagen de Playwright y deja el guion en el comentario, para que una persona pueda agregarlo al repositorio.
3. **Leer el resultado.** Mira los pasos fallidos, las capturas, los errores de consola, y las trazas y registros de la copia (los datos de `observe:` de la aplicación, en el espacio de nombres de la copia), y escribe qué hizo, qué pasó, qué falló y qué no pudo comprobar.

El agente **comenta; nunca aprueba, integra ni despliega**: integrar sigue siendo de una persona, como en todo rendimiento. Si no alcanza la copia o las pruebas son inestables, lo dice en lugar de decir que pasó.

## Cuánto cuesta {#cost}

- **Clúster:** una réplica por servicio, solicitudes de recursos a la cuarta parte de las de la aplicación, una `ResourceQuota` por espacio de nombres de vista previa y el tope de 3 copias. En un clúster de Raspberry Pi ese es el número que importa: la página de la aplicación muestra la memoria que retienen sus copias vivas, y las comprobaciones del Entorno avisan cuando las vistas previas ocupan más de una parte del clúster (10 % por omisión).
- **Base de datos:** una base vacía es gratis. `copy` cuesta el tamaño del respaldo por cada copia, por eso es opcional y tiene tope.
- **IA:** cada verificación cuenta en el tope mensual de la organización; las pruebas generadas cuestan más que las de la propia aplicación. La página de la aplicación muestra lo que gastó el agente por solicitud, y la verificación puede fijarse en *solo las pruebas de la aplicación*.
- **Construcciones:** ninguna extra: reutiliza la ejecución de la rama.

## Orden de trabajo {#order-of-work}

Cada tramo se integra por separado y sirve solo:

1. **La especificación y el render:** `previews:` en `internal/spec` (validar, marcadores del CRD, `web/src/api.ts`, un ejemplo en `internal/spec/ejemplos`) y el espacio de nombres de vista previa en `internal/render`, con pruebas de envtest.
2. **La copia:** el webhook `pull_request`, desplegar la ejecución que pasó, la dirección, la comprobación con su enlace, y el borrado al cerrar, por inactividad y por el tope.
3. **El comentario:** un solo comentario editado con el enlace y la fecha límite.
4. **El agente, con las pruebas de la aplicación**, y luego **con pruebas generadas** (paso 13).
5. **Datos:** `seed`, y luego `copy` con `scrub`.

La meta se cumple cuando una solicitud de la aplicación de propinas muestra el enlace a su vista previa y el comentario del agente. Hasta el tramo 4 el comentario es solo el enlace y la fecha límite.

## Preguntas abiertas {#open-questions}

- ¿Cuál es la aplicación de propinas y tiene hoy pruebas de navegador? El tramo 4 depende de ello.
- ¿Es correcto el 10 % del clúster para el de Pi, o las vistas previas deben quedar apagadas ahí y correr solo en los nodos más grandes?
- ¿Debe existir `copy`, o basta `seed`? Es el único tramo que pone datos reales fuera de producción.
