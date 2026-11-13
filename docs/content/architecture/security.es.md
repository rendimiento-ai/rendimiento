# Modelo de seguridad

Una plataforma de despliegue puede ejecutar el código de cualquiera y cambiar cualquier cosa en el clúster, así que su modelo de seguridad merece un capítulo. Este dice qué está protegido, cómo, y qué todavía no.

## Identidades {#identities}

| Identidad | Puede | La usa |
|---|---|---|
| **Las personas** ([miembros](../guide/members.md), por su cuenta de GitHub) | según su función: los invitados leen; los colaboradores además lanzan CI, escriben en la Ruta y les piden cosas a los agentes; los administradores además despliegan, revierten, y manejan secretos, llaves, complementos y miembros | el panel |
| **La aplicación de GitHub** (tokens de instalación, 1 hora) | leer código, enviar ramas, abrir solicitudes de incorporación, escribir comprobaciones y administrar incidencias (Renovate), solo en los repositorios donde está instalada | la plataforma, los pods de construcción (para clonar), Renovate |
| Cuenta de servicio **`rendimiento`** | administrar objetos App y Addon; espacios de nombres, Deployments, Services, Ingresses, CronJobs, PVCs y Secrets en todo el clúster; pods de construcción en `rendimiento-builds`; leer nodos, métricas y CRD; **suplantar solo a `rendimiento-addons`** | el proceso de la plataforma |
| Cuenta de servicio **`rendimiento-addons`** | `cluster-admin`; ningún pod se ejecuta con ella | el controlador de complementos, por suplantación |
| **Pods de construcción** | nada en Kubernetes (sin token montado) | los pasos de integración continua, Renovate |

## Aislamiento del código no confiable {#isolation-of-untrusted-code}

El código de los repositorios se ejecuta en la integración continua: las pruebas y las líneas `RUN` de los Dockerfiles. Se contiene en capas:

1. **Sin credenciales**: los pods de construcción no reciben un token de cuenta de servicio de Kubernetes; el único secreto es un token para clonar en esa ejecución.
2. **Red**: la política de red `isolate-builds` permite solo DNS, los demonios de BuildKit e internet. Las demás aplicaciones, las bases de datos, la API de Kubernetes, la red de los nodos y la red doméstica (`192.168.0.0/16`) son inalcanzables. Se comprobó con pods de prueba en cada nodo.
3. **Nodos**: las construcciones nunca corren en el plano de control ni en los nodos que aparecen en `BUILD_EXCLUDE_NODES` (por ejemplo, los nodos que no pueden aplicar políticas de red).
4. **Límites**: un plazo por paso y límites de memoria por pod de pruebas.
5. **Bifurcaciones**: las solicitudes de incorporación desde bifurcaciones nunca se construyen.

Los demonios de BuildKit corren en modo **privilegiado** (crean contenedores); los pods de construcción hablan con ellos por TCP. Un `RUN` malicioso se ejecuta dentro del aislamiento de BuildKit, no en el contenedor del propio demonio, pero un escape de BuildKit llegaría a ese nodo. BuildKit sin privilegios está en la [hoja de ruta](../future/roadmap.md).

## Las organizaciones en el clúster {#organizations-in-the-cluster}

En un clúster, las aplicaciones de cada organización tienen una barda hecha para los equipos de una misma empresa: detiene los accidentes y la curiosidad, no a un atacante decidido. Su parte de red y su cuota están **apagadas de forma predeterminada**, así que una actualización no cambia nada hasta que el operador las encienda.

- **Etiquetas, siempre.** Cada objeto App lleva la dirección de su organización en la etiqueta `rendimiento.ai/org`, y también el espacio de nombres que rendimiento crea para él. Un App hecho antes no la lleva y cuenta como de la plataforma (`principal`) hasta que su siguiente versión escriba la etiqueta. El espacio de nombres de una aplicación con `sharedNamespace` es de alguien más y nunca se etiqueta.
- **Una política de red, si se enciende** (`ORG_NETWORK_POLICY=true`; en Helm, `orgBoundary.networkPolicy`). Cada espacio de nombres de aplicación que rendimiento administra recibe la NetworkPolicy `rendimiento-org`, que solo deja entrar tráfico desde:
    - sus propios pods (un servicio y la base de datos de su `needs:`, las tareas de despliegue);
    - los espacios de nombres de la misma organización (una aplicación que llama al servicio de otra);
    - el espacio de nombres de la plataforma: las revisiones de disponibilidad y la verificación de cada versión llaman a cada servicio en su dirección dentro del clúster;
    - el espacio de nombres del controlador de entrada (`INGRESS_NAMESPACE`): todo lo que llega por un dominio, incluidas las revisiones públicas de disponibilidad y los retos de cert-manager;
    - los espacios de nombres y rangos de direcciones que el operador indique (`ORG_NETWORK_POLICY_NAMESPACES`, `ORG_NETWORK_POLICY_CIDRS`): Prometheus u otro recolector de métricas, o la red de los nodos cuando el controlador de entrada usa la red del nodo.

    Un servicio con `lan:` recibe una segunda política, `<servicio>-lan`, que abre su propio puerto a todos, porque la red local llega a él por el balanceador de carga, desde direcciones que ningún espacio de nombres reconoce. La salida sigue abierta: las aplicaciones siguen alcanzando internet, el colector de trazas y los servicios de otras organizaciones que las dejen entrar. Un espacio de nombres con `sharedNamespace` no recibe política.
- **Una cuota, si se define** (`ORG_QUOTA_CPU`, `ORG_QUOTA_MEMORY`; en Helm, `orgBoundary.quota`). Una ResourceQuota de Kubernetes cuenta un solo espacio de nombres, y una organización tiene muchos, así que la plataforma cuenta en su lugar: antes de crear una versión de una aplicación de cualquier organización que no sea la de la plataforma, suma lo que pide esa versión (cada servicio y cada base de datos o caché de `needs:`, por sus réplicas) a lo que pide la última versión de cada otra aplicación de la organización, y rechaza la versión si el total pasa la cuota. La ejecución dice por qué. Las tareas programadas y las de despliegue van y vienen y no se cuentan, las reversiones no se revisan, y nada impide que alguien con `kubectl` la rebase.

Lo que la barda no es:

- **Una sola cuenta de servicio.** La cuenta de servicio `rendimiento` sigue administrando los espacios de nombres de todas las organizaciones; lo que las separa es el código de la plataforma, no los permisos de Kubernetes. No hay una cuenta de servicio por organización.
- **Construcciones compartidas.** Las construcciones no son por organización: cada paso de integración continua corre en `rendimiento-builds`, en los mismos demonios privilegiados de BuildKit, y sube sus imágenes al mismo registro, del que cada nodo descarga sin credenciales.
- **Solo donde la red la aplica.** Una NetworkPolicy solo se cumple donde el complemento de red del clúster la aplica (Calico, Cilium, el controlador de políticas de red propio de k3s). Flannel solo, o k3s iniciado con `--disable-network-policy`, acepta el objeto y no aplica nada.

### Encenderla {#turning-the-boundary-on}

1. Despliegue la versión que la trae (`make deploy`, o `helm upgrade`): la cuenta de servicio de la plataforma recibe el permiso de escribir políticas de red.
2. Averigüe qué llama a las aplicaciones desde otros espacios de nombres: el controlador de entrada (`INGRESS_NAMESPACE`; vacío, `ingress-nginx` para la clase `nginx` y `kube-system` para `traefik`), un recolector de métricas, un complemento. Anótelos en `ORG_NETWORK_POLICY_NAMESPACES`. Si el controlador de entrada usa la red del nodo, agregue el rango de los nodos a `ORG_NETWORK_POLICY_CIDRS`.
3. Ponga `ORG_NETWORK_POLICY=true` y reinicie los servicios de la plataforma (`kubectl rollout restart deployment -n rendimiento-system`). Cada aplicación recibe su política en su siguiente reconciliación, en menos de cinco minutos, antes de que se toquen sus cargas de trabajo.
4. Revise que una aplicación de cada organización siga respondiendo en su dirección y en su pestaña de disponibilidad, y que un pod en el espacio de nombres de otra organización no pueda alcanzarla.

Para dar marcha atrás, ponga `ORG_NETWORK_POLICY=false` y reinicie otra vez: las políticas de cada aplicación se quitan en su siguiente reconciliación. Con prisa, después de eso, `kubectl delete networkpolicy -A -l rendimiento.ai/org` las quita todas de una vez (la política de las construcciones no lleva esa etiqueta).

## Secretos {#secrets}

- **Nunca en git en texto plano.** Las aplicaciones se refieren a los secretos por su nombre en `rendimiento.yaml`; los valores se definen en la interfaz (**Configuración → Secretos**) o se confirman como **secretos sellados** (cifrados con la llave del clúster).
- **Nunca en claro en la base de datos de la plataforma.** Postgres guarda huellas de las sesiones, no tokens. Los secretos propios de la plataforma (las credenciales de la aplicación de GitHub, las llaves de IA de las organizaciones) son Secrets de Kubernetes en un clúster. En una computadora (`MODE=local`), o en un clúster con `SECRETS_KEY_FILE`, se **sellan en Postgres** con AES-256-GCM, con una llave que vive fuera de la base de datos: un archivo que solo rendimiento lee (`~/.config/rendimiento/llave-secretos` en una computadora). Cada valor queda atado a su nombre, así que un valor sellado copiado con otro nombre no abre (`internal/secretos`). En una computadora, los secretos de las aplicaciones y las contraseñas de su Postgres y su Redis se sellan igual. Quien tenga a la vez la base de datos y el archivo de la llave puede abrirlos: guarde la copia de la llave aparte de los respaldos de la base de datos.
- **Generados cuando se puede.** Las credenciales de `needs:` se generan una vez, se guardan en un Secret y se inyectan por referencia (`secretKeyRef`), así la contraseña nunca aparece en una especificación ni en un registro.
- **De corta vida** donde se puede: los tokens de instalación de GitHub duran una hora y se emiten para cada construcción y cada ejecución de Renovate.
- **Fuera de los registros.** El analizador de registros de Renovate, los registros de construcción y los mensajes de error nunca incluyen tokens.

## La superficie web {#the-web-surface}

- Inicio de sesión con OAuth de GitHub; las sesiones son tokens aleatorios guardados como huella, válidos 7 días y comprobados en cada petición contra la membresía de la persona y la función que pide la ruta.
- Las galletas son `HttpOnly`, `Secure` (en HTTPS) y `SameSite=Lax`; las peticiones que cambian el estado desde otro origen se rechazan.
- Los avisos web se verifican con su firma HMAC, comparada en tiempo constante.
- La página de configuración de la aplicación de GitHub requiere un token de configuración de un solo uso.
- Encabezados de seguridad: sin adivinar el tipo MIME, sin marcos, referencias solo del mismo origen.

## La bitácora {#the-bitacora}

Cada acción se escribe en la **bitácora**, un renglón por acción, sea de una persona o de un agente:

- **Personas**: cada ruta de la API que cambia algo (todas menos `GET`) anota quién la usó, la ruta (`POST /api/apps/{app}/rollback`), sobre qué fue (los valores de su ruta: la aplicación, la ejecución, el *nombre* de un secreto) y el estado HTTP con que terminó. Nunca se guarda el cuerpo de la petición, porque puede traer secretos. Una prueba recorre cada ruta registrada, así que una nueva no se escapa (`TestEveryChangeIsInTheBitacora`).
- **Agentes**: cada llamada a una herramienta MCP anota la llave, la herramienta, sus argumentos (ocultos cuando parecen un secreto) y cómo terminó.
- **Una cadena por organización**: cada renglón lleva la huella del anterior. Un renglón cambiado o borrado en la base de datos rompe la cadena desde ahí, y `GET /api/bitacora/verificar` dice dónde.

La leen los colaboradores y los administradores en la página **Bitácora**: si la cadena está completa, filtros por persona, acción o aplicación, y la exportación en JSONL de los renglones que se ven (`GET /api/bitacora`). Los invitados y los enlaces de demostración no.

## Validación en las orillas {#validation-at-the-edges}

`rendimiento.yaml` se valida antes de ejecutar cualquier cosa: los nombres son etiquetas DNS, las rutas no pueden salirse del repositorio, las anotaciones de la entrada se limitan a `nginx.ingress.kubernetes.io/*` y **se rechazan los fragmentos de código** (permiten configuración arbitraria de nginx), las direcciones de la red local deben ser privadas y se comprueban las referencias a secretos. El controlador rechaza espacios de nombres y nombres de host que no le pertenecen.

## Huecos conocidos {#known-gaps}

Estos son los puntos débiles, dichos con honestidad, más o menos en orden de importancia para una instalación con varios usuarios o pública:

1. **Las organizaciones son frontera en la API, todavía no debajo de ella.** Las personas tienen roles (invitado, colaborador, administrador) dentro de organizaciones, y cada aplicación pertenece a una. Una prueba llama cada ruta de la API como administradora de otra organización, con los nombres y números de la primera, y no encuentra nada de ella (`TestEveryRouteStaysInItsOrg`, que recorre las rutas tal como se registran, así que una ruta nueva también se revisa). Las herramientas MCP revisan la organización de la llave y sus aplicaciones. Una organización que no es la de la plataforma crea aplicaciones solo desde las instalaciones de GitHub que se le dieron (un operador, o su administrador al conectar la aplicación instalada en su propia cuenta de GitHub, cuyo usuario comprueba su inicio de sesión), y el servidor lo revisa en cada ruta que recibe una instalación ([Cuentas de GitHub](../guide/members.md#github-accounts)). Una aplicación puede tener su propia lista de quién entra, con una función por persona para esa aplicación ([Quién entra a una aplicación](../guide/members.md#who-enters-an-app)); una sola función decide la función de una persona en una aplicación (`store.AppAccess.Role`), cada ruta sobre una aplicación la consulta antes de revisar la función que pide, y una aplicación de cuya lista alguien queda fuera le responde *no existe*. Una segunda prueba recorre cada ruta como un miembro que quedó fuera de la lista de una aplicación y no encuentra nada de ella (`TestEveryRouteKeepsToTheAppsList`). Falta: en un clúster una sola cuenta de servicio maneja el espacio de nombres de cada aplicación, así que la frontera es el código de la plataforma, no Kubernetes; la frontera de red opcional solo limita qué espacios de nombres llegan a una aplicación ([Las organizaciones en el clúster](#organizations-in-the-cluster)). Eso hace falta antes de que desconocidos compartan un clúster. Vea [Convertirse en un proyecto de código abierto](../future/open-source.md).
2. **Permisos en todo el clúster** para la plataforma (para crear espacios de nombres para cualquier aplicación). Un modo más restringido le daría a cada aplicación un espacio de nombres creado de antemano por un administrador.
3. Demonios de BuildKit **privilegiados**.
4. **Registro en HTTP simple** en la red local.
5. **Los respaldos se quedan en el clúster.** Los volúmenes se respaldan cada noche en Garage, que corre en las mismas máquinas, así que los respaldos no sobrevivirían a perder todo el clúster. Una copia fuera de casa está en la hoja de ruta.
6. **La bitácora todavía no se envía fuera** (a un SIEM, o una copia fuera de la base de datos). Las acciones automáticas de la plataforma misma (una reversión tras una verificación fallida, Renovate) todavía no están en ella.
