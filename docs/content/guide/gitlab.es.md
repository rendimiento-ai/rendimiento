# Conectar GitLab {#connecting-gitlab}

rendimiento construye aplicaciones cuyo código vive en **GitLab**, además de GitHub: gitlab.com o un GitLab propio (autogestionado, en una dirección como `https://git.example.com`). Cada GitLab se conecta una vez, en la página de **Entorno**; después, **Nueva aplicación** ofrece sus proyectos junto a sus repositorios de GitHub, y cada aplicación dice en qué forja vive.

## Lo que hace ahí {#what-it-does-there}

Lo mismo que en GitHub, dicho con las palabras de GitLab:

| rendimiento necesita | En GitHub | En GitLab |
|---|---|---|
| Conectarse | La App de GitHub (manifiesto) | Una aplicación OAuth de su grupo o de su cuenta |
| Saber de cada push | Webhook firmado con HMAC | Webhook del proyecto con ficha secreta (`X-Gitlab-Token`) |
| Clonar | La ficha de la instalación | La ficha OAuth (como el usuario `oauth2`) |
| Decir si pasó una confirmación | Check run | Commit status: pending, running, success, failed, canceled |
| «Una persona debe integrarla» | `action_required` | Estado **pending**, con una descripción que lo dice |
| Abrir e integrar cambios | Pull request | Merge request (`!7`) |
| Iniciar sesión | El OAuth de GitHub | El OAuth de GitLab |

Cuando una aplicación [se integra sola](apps.md), rendimiento integra el merge request en cuanto pasa su ejecución; si GitLab todavía no lo integra (su propio pipeline sigue corriendo), rendimiento lo deja en **merge when pipeline succeeds**, y la ejecución lo dice.

## Crear la aplicación OAuth {#creating-the-oauth-application}

1. En su GitLab, abra **Configuración → Aplicaciones** de su grupo (o, para su propia cuenta, **Preferencias → Aplicaciones**) y agregue una aplicación nueva.
2. **URI de redirección**: la dirección de rendimiento seguida de `/api/forjas/gitlab/callback`, por ejemplo `https://rendimiento.example.com/api/forjas/gitlab/callback`. La página de Entorno muestra la suya.
3. **Confidencial**: sí.
4. **Alcances**: `api` (para leer el código, escribir los estados de las confirmaciones, abrir e integrar merge requests y agregar webhooks) y `read_user` (para saber quién la autorizó y para iniciar sesión).
5. Guárdela y tenga a la mano su **identificador de aplicación** y su **secreto**.

## Conectarlo {#connecting-it}

En **Entorno → Dónde vive su código**, la tarjeta de GitLab pide:

- **Dirección**: vacía para gitlab.com, o la de su GitLab, como `https://git.example.com`. Un `http://` simple solo se acepta para una dirección de su propia red (`git.example.lan`, una IP).
- El **identificador de la aplicación** y el **secreto** de la aplicación de arriba.

**Conectar GitLab** lo lleva a GitLab para autorizarla. Desde entonces rendimiento actúa ahí como la cuenta que la autorizó: lista los proyectos que esa cuenta **mantiene** (debe poder agregar webhooks) y guarda sus fichas, renovándolas cuando GitLab las vence, cada dos horas. **Autorizar de nuevo**, en la tarjeta, lo hace otra vez, como otra persona o después de que GitLab revocó la ficha.

El secreto de la aplicación, la ficha de los webhooks y las fichas de acceso se guardan con los demás secretos de la plataforma (un Secret de Kubernetes llamado `rendimiento-gitlab-<id>`, o sellados en la base de datos en una computadora), nunca en una tabla a la vista y nunca se vuelven a mostrar. La base de datos solo guarda la dirección, el identificador de la aplicación y quién la autorizó.

## Lo que rendimiento crea {#what-rendimiento-creates}

Al crear una aplicación desde un proyecto de GitLab, rendimiento:

- agrega el **webhook** del proyecto hacia `https://rendimiento.example.com/api/webhooks/gitlab`, para los eventos de push y de merge request, con la ficha secreta de la conexión (uno por proyecto: crearlo de nuevo lo actualiza);
- abre el **merge request** que agrega `rendimiento.yaml`, como en GitHub (o construye de inmediato si el proyecto ya lo tiene);
- escribe un **commit status** llamado `rendimiento: <aplicación>` en cada confirmación que corre, con el enlace a la ejecución.

El repositorio de una aplicación de GitLab se escribe con la dirección de su GitLab al frente, `git.example.com/grupo/proyecto`, para que dos forjas nunca mezclen sus proyectos.

## Iniciar sesión con GitLab {#signing-in-with-gitlab}

Con un GitLab conectado, la página de bienvenida ofrece **Iniciar sesión con GitLab** junto a GitHub. Quién entra:

- Si la cuenta de GitLab está **ligada** a una persona, entra esa persona, con sus funciones y su Ruta. Cada quien liga su propia cuenta desde **Sus cuentas**, en el menú de la cuenta: entra como siempre, presiona **Ligar mi cuenta de…** y la autoriza en GitLab.
- Una cuenta que no está ligada a nadie entra como `usuario@host` (por ejemplo `ana@gitlab.com`), y solo si un administrador agregó ese nombre en [Miembros](members.md). Los usuarios de GitHub no llevan `@`, así que nunca se mezclan.

## Todavía no {#not-yet}

Esto sigue funcionando solo con GitHub y, por ahora, se salta las aplicaciones de GitLab: [Renovate](renovate.md), los [arreglos declarados](fixes.md), el agente de código, y que rendimiento junte un conflicto por sí mismo (la API de GitLab escribe confirmaciones con un solo padre). Una conexión con GitLab la usa la organización de la plataforma. Para Bitbucket, vea [Conectar Bitbucket](bitbucket.md).
