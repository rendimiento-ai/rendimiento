# Caminos: las ramas de una aplicación como veredas

La pestaña **Caminos** de cada aplicación cuenta como una historia las últimas dos semanas de sus construcciones. La rama principal es el *camino real*, un camino de tierra que va de izquierda a derecha, de lo más antiguo a lo más reciente. Cada una de las demás ramas es una *vereda*: sale del camino real en la confirmación de la que partió y, al integrarse, regresa a él. Cada ejecución es una parada en el camino.

## Qué se ve {#what-you-see}

| En el mapa | Qué es |
|---|---|
| El **camino real** | la rama principal; las mojoneras marcan los días |
| Una **vereda** | una rama. Sale del camino real donde empezó (la base común) y regresa donde se integró su solicitud de incorporación. Una solicitud apilada, cuya base es otra rama, sale de esa rama y regresa a ella |
| Un **huacal** de maíz entero | una ejecución que pasó |
| Un huacal roto, con el maíz regado | una ejecución que falló; el panel dice en qué paso (`platform: pruebas`, `web: imagen`…) |
| Un huacal gris | una ejecución cancelada |
| Un **puesto del mercado** sobre el camino real | una versión que se desplegó, con su número |
| Una vereda que termina en un **letrero** | una rama que no se integró: *esperando en la garita* mientras corrió en las últimas 48 horas o su solicitud sigue abierta, y *se quedó en el camino* después |
| Una vereda punteada | todavía no se integra |
| Una **casita** al final de una vereda, con un número | la [copia viva](#live-copies) de su solicitud; una punteada es una copia corta en seco. Elíjala (o llegue a ella con Tab) para ir a su tarjeta |

Los ratos de calma se acortan: una tarde con mucho trabajo tiene más espacio que un fin de semana vacío.

Toque una parada o una vereda para ver su historia en el panel: cuándo y de dónde salió, sus viajes, cuántas veces tropezó, cuánto tardó de salir a integrarse, y un enlace a cada ejecución. Sin nada elegido, el panel resume las dos semanas: cuántos viajes llegaron enteros, la vida típica de una vereda, cuántas veredas pasaron al primer viaje, las que no se integraron y los últimos tropiezos.

## Copias vivas {#live-copies}

Una aplicación con [`previews:`](../future/previews.md) tiene una sección arriba del mapa, **Copias vivas · N de max**: una copia viva por cada solicitud de incorporación cuya rama pasó, y las copias cortas que hacen sus ejecuciones solo para correr sus comprobaciones [en seco](tasks.md#dry-runs-against-a-copy) (cuentan para `max`). El vistazo tiene una etiqueta, **N copias vivas**, que lleva aquí.

- **Cuatro cifras:** cuántas copias viven del `max` de la aplicación; la memoria que ocupan (la que usan sus pods, según el servidor de métricas, o la que piden cuando este no dice; la cifra dice cuál); la parte de la memoria del clúster que ocupan las copias de **todas** las aplicaciones, contra el tope de 10 % (en una computadora no hay clúster que compartir); y el `keep` y el `max` de la aplicación, que se cambian en `rendimiento.yaml`.
- **Una tarjeta por copia:** el número, el título y el autor de su solicitud, su dirección, la rama, el commit y la ejecución que muestra, su salud, el resultado de sus pruebas propias (cuántas pasaron y el nombre de las que fallaron) y de sus comprobaciones en seco, la memoria que ocupa y el día en que se borra (`keep` días después de su último envío, o al integrar o cerrar la solicitud). La tarjeta de una copia corta es punteada y enlaza a su ejecución.
- **Borradas hace poco:** las copias de los últimos 7 días y por qué se fue cada una: al integrar, al cerrar, sin envíos durante `keep` días, para dejar lugar bajo `max`, o por una persona.
- **Secretos:** una copia lee en su propio espacio de nombres los secretos que nombran los servicios de la aplicación, y todavía nada los copia ahí; por eso la sección nombra los que le faltan a una copia (solo los nombres, nunca los valores): arranca, pero lo que los necesita no funciona. En una computadora, una copia lee los secretos de su aplicación.

Una persona con permiso de integrar (colaboradora o administradora de la aplicación) tiene un menú **⋯** en la copia de cada solicitud: **Rehacerla con el último commit** (la última ejecución de la rama que pasó; si la copia ya la tiene, sus servicios se reinician), **Dejarla 3 días más** (hasta 30 días después de su último envío; el comentario de la solicitud dice el nuevo día) y **Borrarla ya**, que pregunta en la tarjeta misma. El comentario de una copia borrada dice quién la borró; el barrido no la vuelve a hacer, el siguiente envío a la rama sí. Cada una queda en la bitácora; la llave de un agente no puede hacerlas. Cada rama con una copia viva tiene en el panel una etiqueta **copia viva** que lleva a su tarjeta.

## Contar la historia {#telling-the-story}

**Contar la historia** recorre las dos semanas: un burro con su carga avanza por cada vereda que va en camino y tropieza (polvo, maíz regado) donde falló una ejecución; cada integración deja un anillo en el camino real, y la caravana con su arriero llega al mercado con confeti (o luciérnagas de noche) en cada versión. Elija el ritmo (*paseo*, 3 minutos; *trote*; *galope*, 45 segundos), páusela o toque la franja de abajo para saltar a una fecha.

Arrastre para recorrer el camino; acerque o aleje con los botones **+ / −** o con Ctrl y la rueda. El mapa conserva su forma a cualquier distancia. Sigue el tema de día o de noche de la barra de arriba y se queda quieto cuando el movimiento está apagado.

## De dónde salen los datos {#where-the-data-comes-from}

- **Las ejecuciones** salen de la base de datos de rendimiento: todas las de los últimos 14 días, con los pasos que fallaron y la versión que crearon.
- **Las ramas** salen de GitHub, con la instalación de la aplicación: las 100 solicitudes de incorporación actualizadas más recientemente (integradas o no, con su rama base) y, para cada rama, la confirmación de la que salió. Para una rama ya integrada se busca contra la base de justo antes de la integración (el primer padre de su confirmación de integración o de *squash*), porque después la base ya la contiene.
- Lo que GitHub dice de las confirmaciones nunca cambia, así que se recuerda; la historia completa se vuelve a armar cuando aparece una ejecución nueva o después de dos minutos. Se consultan como máximo 60 ramas, las de actividad más reciente.
- Si GitHub no responde, la pestaña dibuja lo que sabe y avisa que a algunas veredas les puede faltar su solicitud o su punto de salida.

## En el código {#in-the-code}

| Qué | Dónde |
|---|---|
| Las ejecuciones con sus pasos fallidos y su versión | `store.RunHistory` |
| Ramas, puntos de salida e integraciones, a partir de las ejecuciones y de GitHub | `internal/caminos` (`Build`) |
| Las llamadas a GitHub y la caché | `platform/caminos.go` (`Platform.Caminos`), `github` (`RecentPullRequests`, `MergeBase`, `FirstParent`) |
| La API | `GET /api/apps/{app}/caminos` |
| El lienzo y el panel | `web/src/components/caminos.ts`, `caminos-tab.tsx` |
| Las copias vivas: qué tiene cada una, su memoria, las borradas y por qué | `platform/copias_vista.go` (`CopiasOf`, `RemakeCopy`, `ExtendCopy`, `DeleteCopy`), `store.Copias` (tabla `copias`) |
| Su API | `GET /api/apps/{app}/copias`; `POST …/copias/{n}/rehacer`, `POST …/copias/{n}/extender`, `DELETE …/copias/{n}` |
| Su sección | `web/src/components/copias.tsx` |
