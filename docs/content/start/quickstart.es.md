# Inicio rápido

De una computadora que nunca ha visto rendimiento a una aplicación suya con dirección, en menos de diez minutos. La mayor parte de ese tiempo es de usted, en las páginas de GitHub; la parte de la máquina tarda menos de un minuto.

Necesita una computadora con **Linux** (amd64 o arm64, con systemd y `curl`) y una **cuenta de GitHub** (o de GitLab o de Bitbucket: [los mismos pasos ahí](#with-gitlab-or-bitbucket)). No hace falta un motor de contenedores: el instalador usa containerd o Docker cuando la computadora tiene uno, y trae containerd cuando no tiene ninguno. En Windows, use [la aplicación](../environment/deploy.md#on-windows) (versión previa); con Kubernetes, los mismos pasos sirven en [un clúster en su computadora](#on-a-cluster), e [Instalar con Helm](../environment/deploy.md#installing-with-helm) cubre un clúster con dominio.

## 1. Instalar {#install}

```sh
curl -fsSL https://rendimiento.ai/instalar | sh
```

Le pregunta dónde vive su código (conteste **GitHub** para este ejemplo, o déjelo para la página de configuración) y, solo cuando tiene que instalar containerd, usa `sudo` una vez. No hay que nombrar a nadie: quien autorice la forja en la página de configuración queda como primer administrador. Deja rendimiento corriendo como un servicio de su usuario en **<http://localhost:8080>**, y al terminar abre la página de configuración en su navegador con la **ficha de configuración** ya en la dirección (también imprime esa dirección). Para abrirla a mano, la ficha está aquí:

```sh
cat ~/.config/rendimiento/ficha-configuracion
```

`curl -fsSL https://rendimiento.ai/instalar | sh -s -- --forja github` contesta la pregunta por adelantado.

## 2. Copiar el ejemplo {#copy-the-example}

[`rendimiento-ai/hola`](https://github.com/rendimiento-ai/hola) es la aplicación más pequeña que rendimiento puede desplegar: una página, servida por nginx, con su `rendimiento.yaml`. Abra **[Use this template](https://github.com/rendimiento-ai/hola/generate)** y cree la copia en su cuenta (pública o privada, las dos sirven).

Con una aplicación suya, sáltese este paso: rendimiento lee el repositorio y resuelve cómo construirlo.

## 3. Conectar GitHub {#connect-github}

La página que abrió el instalador (o <http://localhost:8080>) es la página de configuración. Hace esto una sola vez:

1. La **ficha de configuración**: ya está en la mano cuando viene del instalador; si no, péguela.
2. **¿Dónde vive su código?** **GitHub**, y oprima **Crear la aplicación de GitHub**. GitHub se abre con el nombre y los permisos ya llenos; confirme.
3. GitHub pregunta qué repositorios puede ver la aplicación: elija su copia de `hola` (o todos; se puede cambiar después).
4. GitHub lo trae de vuelta a rendimiento, que inicia su sesión con su cuenta de GitHub (GitHub puede pedirle que lo autorice), como **primer administrador**. Agregue a otros ahora o después en Miembros, y oprima **Terminar y entrar**.

La aplicación de GitHub es suya, en su cuenta: el código de rendimiento nunca pasa por servidores de alguien más.

## 4. Desplegar {#deploy}

Oprima **Nueva aplicación**, elija su copia de `hola` en la lista (**¿Falta un repositorio?** lo lleva a GitHub para darle acceso a la aplicación) y oprima **Desplegar**. La página de la ejecución muestra la construcción mientras ocurre; cuando termina, la aplicación responde en

**<http://hola.localhost:8000>**

## 5. Cambiarla {#change-it}

Edite el saludo en `index.html` y envíelo a `main`. En menos de un minuto rendimiento ve el envío, lo construye y reemplaza la versión que corre sin interrupción. (GitHub no alcanza a `localhost` con un aviso web, así que en una computadora rendimiento le pregunta a GitHub cada minuto.)

## En un clúster en su computadora {#on-a-cluster}

Para probar rendimiento como corre en Kubernetes, [k3d](https://k3d.io) hace un clúster dentro de Docker. Necesita Docker, `k3d`, `helm` y `kubectl`, y un rendimiento más nuevo que `v0.1.10`. Dos comandos reemplazan el paso 1: el clúster con su registro de imágenes, y luego rendimiento.

```sh
k3d cluster create rendimiento -p "8080:80@loadbalancer" --registry-create rendimiento-registro

helm install rendimiento oci://ghcr.io/rendimiento-ai/charts/rendimiento \
  -n rendimiento-system --create-namespace \
  --set baseURL=http://rendimiento.localhost:8080 \
  --set registry.host=rendimiento-registro:5000 \
  --set ingress.className=traefik
```

En cosa de un minuto rendimiento responde en **<http://rendimiento.localhost:8080>** (su navegador resuelve todo nombre `.localhost` a su propia computadora). Aquí tampoco se nombra a nadie por adelantado: quien autorice la forja en la página de configuración queda como primer administrador (`--set firstAdmins=ana,luis@gitlab.com` nombra a más). La ficha de configuración está en el clúster:

```sh
kubectl -n rendimiento-system get secret rendimiento-setup -o jsonpath='{.data.token}' | base64 -d
```

Los pasos 2 a 5 son los mismos, en esa dirección, y la aplicación responde en **<http://hola.localhost:8080>**: en una dirección `.localhost` rendimiento publica cada aplicación por HTTP simple, sin certificado ni registro de DNS, y le pregunta a GitHub por commits nuevos cada minuto. `k3d cluster delete rendimiento` lo quita todo.

## Con GitLab o Bitbucket {#with-gitlab-or-bitbucket}

Los mismos pasos, en la otra forja:

```sh
curl -fsSL https://rendimiento.ai/instalar | sh -s -- --forja gitlab                                     # gitlab.com
curl -fsSL https://rendimiento.ai/instalar | sh -s -- --forja gitlab --gitlab https://git.example.com     # el suyo propio
curl -fsSL https://rendimiento.ai/instalar | sh -s -- --forja bitbucket
```

- **Copie el ejemplo** con la importación de su forja (GitLab: **New project → Import project → Repository by URL**; Bitbucket: **Create → Import repository**), desde `https://github.com/rendimiento-ai/hola.git`.
- **Conéctela**: la página de configuración se abre en esa forja y pide una aplicación OAuth: en GitLab, **Configuración → Aplicaciones** de su grupo o de su usuario, con el URI de redirección `http://localhost:8080/api/forjas/gitlab/callback`, confidencial, con los alcances `api` y `read_user`; en Bitbucket, **Configuración → Consumidores OAuth** del espacio de trabajo, con la URL de retorno `http://localhost:8080/api/forjas/bitbucket/callback`. El instalador imprime la que necesita, y la página la muestra con un botón que la copia. Ahí sirve una dirección `http://localhost` sin https: es su navegador el que la sigue, y nada en internet tiene que llegar a su computadora. Pegue el identificador y el secreto de la aplicación, autorícela y regresa como primer administrador.
- **Despliéguela y cámbiela** como arriba. GitLab y Bitbucket tampoco alcanzan a `localhost` con avisos web, así que en una computadora rendimiento les pregunta por confirmaciones nuevas cada minuto, igual que a GitHub.

## Cuánto tarda {#how-long-it-takes}

Cada versión de rendimiento, y cada cambio al instalador, se instala en una máquina amd64 nueva (de GitHub, con una conexión rápida), despliega `hola` y se desinstala; la ejecución falla cuando la instalación y el despliegue juntos pasan de diez minutos ([la prueba del inicio rápido](../develop/testing.md)). En noviembre de 2026 midió:

| Paso | La computadora tenía Docker | Sin motor de contenedores |
|---|---|---|
| El instalador, hasta que rendimiento responde | 25 s | 31 s |
| De **Desplegar** hasta que la dirección responde | 15 s | 13 s |
| Los dos | 40 s | 44 s |

El clúster se prueba igual cuando cambia el chart o la forma en que se publica una aplicación: k3d hizo el clúster en 18 s, rendimiento respondió entre 22 y 47 s después de `helm install`, y `hola` respondió 17 s después de **Desplegar**. Ahí la imagen de rendimiento ya estaba en la máquina; la suya se descarga primero.

Los pasos 2 y 3 son una persona en las páginas de GitHub, y la prueba no los cuenta: ahí un sustituto responde por GitHub (la prueba todavía nombra a su administrador por adelantado, con `RENDIMIENTO_GITHUB`). Cuente con unos minutos la primera vez.

## Quitarlo otra vez {#take-it-off-again}

```sh
curl -fsSL https://rendimiento.ai/instalar | sh -s -- --desinstalar
```

Detiene y quita rendimiento, los contenedores de sus aplicaciones y el programa, y pregunta antes de borrar sus datos. La aplicación de GitHub se queda en su cuenta (Settings → Developer settings → GitHub Apps), igual que una aplicación de GitLab o un consumidor OAuth de Bitbucket, donde puede borrarlos.

## A dónde seguir {#where-to-go-next}

- [Un recorrido por la interfaz](tour.md): cada página y para qué sirve.
- [Referencia de rendimiento.yaml](../guide/spec.md): servicios, bases de datos con `needs: [postgres]`, pruebas, tamaños.
- [Hecho para agentes](../guide/agents.md): dele una llave a su agente de código, por MCP.
- [En una computadora](../environment/deploy.md#on-one-computer): qué dejó el instalador, y qué no está todavía.
