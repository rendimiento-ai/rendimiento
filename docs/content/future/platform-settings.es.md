# Ajustes de la plataforma desde la página Entorno (diseño) {#platform-settings-from-the-environment-page-design}

**Estado: propuesto, a la espera de su aprobación.** Nada de esta página está construido todavía. Responde al encargo 25: hoy, encender la frontera entre organizaciones exige editar a mano el ConfigMap privado, dar `make deploy` desde la máquina del clúster y reiniciar los pods. Debería poder hacerse desde rendimiento. Apruebe esta página (o cámbiela) y el trabajo sigue en las fases del final.

La página parte de lo que ya existe: la frontera entre organizaciones (`ORG_NETWORK_POLICY` y compañía, vea [Seguridad](../architecture/security.md)) ya está integrada, y la página de capacidad (`GET/PUT /api/environment/capacity`, guardada en `platform_settings`) es el modelo.

## Qué se puede cambiar en vivo y qué no {#what-can-change-live-and-what-cannot}

Un ajuste es **en vivo** cuando la plataforma puede volver a leerlo mientras corre y nada de lo ya construido depende de él al arrancar. Los demás se quedan en el ConfigMap, porque la plataforma los necesita para arrancar (o para saber de dónde leer los ajustes en vivo).

| Ajuste | ¿En vivo? | Por qué |
|---|---|---|
| `ORG_NETWORK_POLICY` | sí | el controlador genera la política en cada conciliación |
| `INGRESS_NAMESPACE` | sí | entra en la misma política |
| `ORG_NETWORK_POLICY_NAMESPACES` | sí | igual |
| `ORG_NETWORK_POLICY_CIDRS` | sí | igual |
| `ORG_QUOTA_CPU`, `ORG_QUOTA_MEMORY` | sí | se leen al revisar una versión |
| `BASE_URL`, `DATABASE_URL`, `SECRETS_KEY_FILE`, fichas, `GITHUB_*` | no | hacen falta para arrancar, o para abrir la base de datos donde se guardan los valores en vivo; los secretos nunca llegan a la página |
| `MODE`, `ROLE`, `INGRESS_CLASS`, `STORAGE_CLASS`, `REGISTRY`, `DNS_*`, `CLUSTER_ISSUER` | no | cambian lo que ya existe (ingresos, volúmenes, registros), así que piden un despliegue y revisar el resultado |
| capacidad (`PARALLEL_RUNS`, …) | ya lo es | su propia sección de la página, sin cambios |

La página nueva muestra **todos** los ajustes con su explicación, de dónde viene su valor y si se pueden cambiar ahí. Los que no, dicen por qué y dónde cambiarlos. Solo los ajustes en vivo tienen control de edición. La lista vive en el código como una sola tabla (nombre, grupo, validador, clave de la explicación): un ajuste pasa a ser en vivo agregando una fila, nunca con una página nueva.

## Dónde se guardan y qué valor gana {#where-they-are-kept-and-which-value-wins}

- En **Postgres**, en la tabla `platform_settings` que la capacidad ya usa: una fila por ajuste (`key = 'env:ORG_QUOTA_CPU'`, el valor como cadena JSON, quién y cuándo). Sin tabla nueva ni columna nueva: la migración no tiene nada que agregar, y la versión anterior simplemente ignora esas filas, así que una actualización con las dos versiones a la vez es segura.
- El **ConfigMap es el valor inicial**. Un proceso arranca con él (como hoy) y luego lee las filas guardadas.
- **Gana el valor guardado, ajuste por ajuste.** Un ajuste sin fila sigue al ConfigMap (y debajo, al valor por omisión), así que cambiar el ConfigMap sigue funcionando para todo lo que nadie cambió en la página. Un ajuste con fila ignora el ConfigMap hasta que se quita la fila con **Volver al valor del ConfigMap**.
- Cuando los dos difieren, la página lo dice y muestra ambos: «guardado: `true`, por ana el 12 de noviembre; el ConfigMap dice `false` y se ignora». Es la única sorpresa del diseño, y por eso está en la página y en la [referencia de configuración](../reference/config.md), no escondida.
- Si no se puede leer la base de datos, cada proceso conserva el último valor que tenía; al arrancar, sin nada leído, usa el ConfigMap. Un valor guardado que ya no valida (por ejemplo, un espacio de nombres borrado después) se ignora, se registra un problema y se usa el valor del ConfigMap.

## Cómo surte efecto, sin reiniciar {#how-it-takes-effect-with-no-restart}

Cada proceso (`api`, `control`, `worker`) guarda los ajustes en vivo en un contenedor como el de la capacidad: se llena al arrancar con el ConfigMap y se refresca desde Postgres cada 15 segundos; el proceso que guarda se refresca al instante.

- **Cuota:** `platform.Config.OrgQuota` se lee por ese contenedor al revisar una versión. No hay más que hacer.
- **Frontera:** el controlador lee `render.Options.OrgNetwork` por el contenedor en cada conciliación (hoy queda fijo al arrancar). Cuando el valor guardado cambia, encola todas las aplicaciones, así las políticas aparecen (o se van) en cerca de un minuto y no en la resincronización de cinco.
- Apagar la frontera quita las políticas en la siguiente conciliación, como ya dice el libro.

## Quién puede cambiarlos y la bitácora {#who-can-change-them-and-the-bitacora}

- **Ver y cambiar** piden la función de **administrador** en la organización propia de la plataforma (una ruta `plat(store.MemberAdmin, …)`, como crear organizaciones): los rangos y espacios de nombres muestran cómo está armado el clúster, así que la página no se muestra a colaboradores ni invitados. Las personas de otras organizaciones nunca la ven. Las llaves de agente no pueden cambiar ajustes; la ruta es para personas con sesión.
- Cada cambio es una fila en la **bitácora** de la organización de la plataforma: acción `ajuste.cambiar` (o `ajuste.restablecer`), sobre `{ajuste: ORG_QUOTA_CPU}`, quién (`persona`, su usuario), por `interfaz`, y el valor anterior y el nuevo en `detalle`. El valor es el propio ajuste, nunca un secreto, así que es seguro registrarlo completo. Un cambio que el validador rechaza no se guarda: responde `400` con el motivo y no escribe fila.
- La fila se escribe en la misma petición que el cambio; la página enlaza a la bitácora filtrada por esa acción.

## Validación {#validation}

Cada cambio se valida en la API antes de guardarse, y el cambio completo como una unidad (así `ORG_NETWORK_POLICY=true` con el espacio de nombres del ingress vacío se rechaza, como hoy al arrancar):

- los booleanos son `true` o `false`;
- los CIDR pasan por `net.ParseCIDR`, máximo 50;
- cada espacio de nombres es una etiqueta DNS válida **y existe** (un `Get` al espacio de nombres; la plataforma ya puede leerlos, así que no hay permiso nuevo);
- las cuotas son cantidades positivas de Kubernetes (`4`, `8Gi`), como al arrancar.

Los validadores son los que `cmd/rendimiento/frontera.go` usa al arrancar, movidos a un paquete que ambos comparten, para que el arranque y la página no puedan discrepar.

## El aviso antes de encender la política {#the-warning-before-turning-the-policy-on}

Antes de guardar `ORG_NETWORK_POLICY=true`, la página consulta `GET /api/environment/settings/boundary-check` y muestra los **espacios de nombres que hoy llaman a las aplicaciones y quedarían bloqueados**: los que están fuera de lo que la política deja entrar (el de la plataforma, el del controlador de entrada, los listados y los propios de cada organización).

rendimiento no puede ver el tráfico real (no tiene registros de flujo), así que la lista es de **llamadores probables**, a partir de lo que el clúster declara:

- los espacios de nombres de los controladores de entrada (por `IngressClass`);
- los espacios de nombres con un `ServiceMonitor`, un `PodMonitor` o pods de un recolector que apunten a espacios de nombres de aplicaciones;
- los espacios de nombres de los complementos que rendimiento aplica;
- los espacios de nombres cuyos pods o Services mencionan el servicio de una aplicación por su nombre en el clúster (`<app>.<espacio>.svc`) en su entorno.

Cada uno se muestra con el motivo por el que está en la lista y un botón, **Dejarlo entrar**, que lo agrega a `ORG_NETWORK_POLICY_NAMESPACES`. La página dice con claridad que es una guía, no una prueba, y deja a la vista la marcha atrás: apagar la frontera es un interruptor, ahora sin reinicio.

## Una sola computadora (`MODE=local`) y Helm {#single-computer-modelocal-and-helm}

- **Una sola computadora:** no hay clúster, ni NetworkPolicy, ni ConfigMap. Las variables de entorno son los valores iniciales y las filas guardadas ganan, igual. Los ajustes de la frontera se muestran como «no aplica en una sola computadora» y no se editan; la cuota sí aplica (aquí también se revisan las versiones) y se puede cambiar. Las rutas solo de clúster ya responden `503` con ese motivo cuando no hay clúster, y las nuevas lo siguen.
- **Helm:** el chart sigue generando el ConfigMap con sus valores (`orgBoundary.*`), como valores iniciales. Un `helm upgrade` no sobrescribe una fila guardada; la página muestra la diferencia, como arriba. No hace falta cambiar el chart por los permisos (el rol de la plataforma ya lee espacios de nombres y pods y administra NetworkPolicies). Los comentarios de `values.yaml` dirán que, después, la página es el modo de cambiarlos.
- Con los roles separados (`ROLE=api`, `control`, `worker`) cada rol se refresca por su cuenta, así que el orden de los reinicios no importa.

## Qué se construye, en orden {#what-is-built-in-order}

1. **Servidor:** la tabla de ajustes y los validadores (compartidos con el arranque), el contenedor, `GET/PUT /api/environment/settings`, `DELETE /api/environment/settings/{key}`, la revisión de la frontera, las filas de la bitácora; el controlador y la plataforma leen por el contenedor. Pruebas: validadores, precedencia (guardado, ConfigMap, valor por omisión), permisos por función, la fila de la bitácora, el controlador regenerando con la política encendida y apagada (envtest, con nombres propios de aplicación y espacio de nombres), la lista de la revisión.
2. **Interfaz:** la página en Entorno, junto a la capacidad: cada ajuste con su explicación, origen y diferencia, controles de edición para los que están en vivo, el aviso antes de la política, en inglés y español de México. Pruebas de la página con la infraestructura de pruebas de la interfaz; `npm run typecheck`.
3. **Libro:** `reference/config.md` (qué ajustes son en vivo y la precedencia), `architecture/security.md` (encender la frontera desde la página en vez del ConfigMap), ambos en inglés y español, y `cambios/<rama>.md`.

## Por decidir {#to-decide}

1. **Gana el valor guardado sobre el ConfigMap** (arriba). La alternativa, que gane el ConfigMap, dejaría inútil la página después del primer `make deploy`.
2. **Llamadores probables, no comprobados**, para el aviso. Los comprobados piden registros de flujo (Cilium Hubble o similar), que la mayoría de los clústeres de aquí no tienen.
3. **Solo administradores de la organización principal**, también para ver. Diga si los colaboradores deben ver los valores en solo lectura.
