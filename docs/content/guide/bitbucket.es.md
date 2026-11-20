# Conectar Bitbucket {#connecting-bitbucket}

rendimiento construye aplicaciones cuyo código vive en **Bitbucket Cloud** (bitbucket.org), además de GitHub y GitLab. Cada **espacio de trabajo** de Bitbucket se conecta una vez, en la página de **Entorno**, con un consumidor OAuth de ese espacio; después, **Nueva aplicación** ofrece sus repositorios junto a los demás, y cada aplicación dice en qué forja vive. Bitbucket Data Center (su propio servidor) viene después.

## Lo que hace ahí {#what-it-does-there}

Lo mismo que en GitHub, dicho con las palabras de Bitbucket:

| rendimiento necesita | En GitHub | En Bitbucket |
|---|---|---|
| Conectarse | La App de GitHub (manifiesto) | Un consumidor OAuth del espacio de trabajo |
| Saber de cada push | Webhook firmado con HMAC | Webhook del repositorio firmado con HMAC-SHA256 (`X-Hub-Signature`), con un secreto que pone rendimiento |
| Clonar | La ficha de la instalación | La ficha OAuth (como el usuario `x-token-auth`) |
| Decir si pasó una confirmación | Check run | Estado de construcción: INPROGRESS, SUCCESSFUL, FAILED, STOPPED |
| «Una persona debe integrarla» | `action_required` | Estado **INPROGRESS**, con una descripción que lo dice |
| Abrir e integrar cambios | Pull request | Pull request (`#7`) |
| Iniciar sesión | El OAuth de GitHub | El OAuth de Bitbucket |

Cuando una aplicación [se integra sola](apps.md), rendimiento integra la solicitud de incorporación en cuanto pasa su ejecución. La integración de Bitbucket no recibe una confirmación, así que rendimiento lee antes la solicitud y solo la integra mientras su punta siga siendo la confirmación probada; una confirmación más nueva espera su propia ejecución.

## Crear el consumidor OAuth {#creating-the-oauth-consumer}

1. En Bitbucket, abra **Configuración del espacio de trabajo → Consumidores OAuth** y presione **Agregar consumidor**.
2. **URL de retorno**: la dirección de rendimiento seguida de `/api/forjas/bitbucket/callback`, por ejemplo `https://rendimiento.example.com/api/forjas/bitbucket/callback`. La página de Entorno muestra la suya.
3. Deje marcada la casilla **Este es un consumidor privado**.
4. **Permisos**: Cuenta **lectura**; Repositorios **escritura**; Solicitudes de incorporación **escritura**; Webhooks **lectura y escritura**.
5. Guárdelo y tenga a la mano su **clave** y su **secreto**.

Bitbucket pide los permisos del consumidor en cada autorización, también al iniciar sesión: no acepta un alcance menor por solicitud. rendimiento solo guarda las fichas de la autorización de la conexión; las de un inicio de sesión se usan una vez, para saber quién es usted, y se descartan.

## Conectarlo {#connecting-it}

En **Entorno → Dónde vive su código**, la tarjeta de Bitbucket pide:

- **Espacio de trabajo**: su nombre, como en su dirección `bitbucket.org/mi-equipo` (también sirve la dirección completa).
- **Clave** y **secreto** del consumidor de arriba.

**Conectar Bitbucket** lo lleva a Bitbucket para autorizarlo. Desde entonces, rendimiento actúa ahí como la cuenta que lo autorizó: lista los repositorios del espacio de trabajo que esa cuenta **administra** (debe poder agregar webhooks) y guarda sus fichas. Las fichas de acceso de Bitbucket duran dos horas, así que rendimiento las renueva antes de que se acaben y guarda las nuevas cada vez. **Autorizar de nuevo**, en la tarjeta, lo hace otra vez, como otra persona o después de que Bitbucket revocó la ficha.

El secreto del consumidor, el secreto de los webhooks y las fichas se guardan con los demás secretos de la plataforma (un Secret de Kubernetes llamado `rendimiento-bitbucket-<id>`, o sellados en la base de datos en una sola computadora), nunca en una tabla simple y nunca se vuelven a mostrar. La base de datos solo guarda el espacio de trabajo, la clave del consumidor y quién lo autorizó.

## Lo que crea rendimiento {#what-rendimiento-creates}

Al crear una aplicación desde un repositorio de Bitbucket, rendimiento:

- agrega el **webhook** del repositorio hacia `https://rendimiento.example.com/api/webhooks/bitbucket`, para `repo:push` y los eventos de las solicitudes de incorporación (`created`, `updated`, `fulfilled`, `rejected`), con el secreto de la conexión, para que cada entrega llegue firmada (una vez por repositorio: crearlo otra vez lo actualiza);
- abre la **solicitud de incorporación** que agrega `rendimiento.yaml`, como en GitHub (o construye de inmediato si el repositorio ya tiene uno);
- escribe un **estado de construcción** llamado `rendimiento: <app>` en cada confirmación que ejecuta, con el enlace a la ejecución.

El repositorio de una aplicación de Bitbucket se escribe con `bitbucket.org` al principio, `bitbucket.org/mi-equipo/mi-repo`, para que nunca se mezcle con uno de GitHub o de GitLab. Una entrega firmada con el secreto de un espacio de trabajo solo cuenta para los repositorios de ese espacio.

## Iniciar sesión con Bitbucket {#signing-in-with-bitbucket}

En cuanto hay un espacio de trabajo conectado, la página de bienvenida ofrece **Iniciar sesión con Bitbucket** junto a GitHub. Quién entra:

- Si la cuenta de Bitbucket está **ligada** a una persona, es esa persona, con sus permisos y su Ruta. Cada quien liga su propia cuenta desde **Sus cuentas**, en el menú de la cuenta: entra como siempre, presiona **Ligar mi cuenta de Bitbucket** y la autoriza en Bitbucket.
- Una cuenta que no está ligada a nadie entra como `apodo@bitbucket.org` (por ejemplo `ana@bitbucket.org`), y solo si un administrador agregó ese nombre en [Miembros](members.md).

## Renovate, arreglos y el agente de código {#renovate-fixes-and-the-coding-agent}

Esto también funciona en las aplicaciones de Bitbucket, por medio de la conexión:

- **[Renovate](renovate.md)** se ejecuta para ellas en un pod de su espacio de trabajo, con su ficha OAuth (que se renueva antes si no alcanzaría para toda la ejecución), y abre solicitudes de incorporación como la cuenta que autorizó la conexión. Sus ramas se construyen y se comprueban como cualquier otra.
- **Los [arreglos declarados](fixes.md)** confirman en la rama por medio de la API de Bitbucket, y **declarar un arreglo** abre una rama cuya solicitud de incorporación usted abre desde el enlace que le da.
- **El agente de código** trabaja en una rama del repositorio y entrega un enlace para abrir su solicitud de incorporación (rendimiento la abre cuando se entrega el encargo).
- El diagnosticador enumera los **archivos que cambiaron** desde la última ejecución buena (el diffstat de Bitbucket).

Un push de varias ramas a la vez ejecuta cada una. Una solicitud de incorporación integrada se anota en la libreta con su confirmación completa, aunque el webhook de Bitbucket solo dé 12 caracteres de ella.

## Todavía no {#not-yet}

rendimiento no junta un conflicto por sí mismo en Bitbucket (la API de Bitbucket escribe confirmaciones con un solo padre): lo hace una persona. Una conexión con Bitbucket la usa la organización de la plataforma. Bitbucket Data Center viene después.
