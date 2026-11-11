# Inicio rápido

De una computadora que nunca ha visto rendimiento a una aplicación suya con dirección, en menos de diez minutos. La mayor parte de ese tiempo es de usted, en las páginas de GitHub; la parte de la máquina tarda menos de un minuto.

Necesita una computadora con **Linux** (amd64 o arm64, con systemd y `curl`) y una **cuenta de GitHub**. No hace falta un motor de contenedores: el instalador usa containerd o Docker cuando la computadora tiene uno, y trae containerd cuando no tiene ninguno. En Windows, use [la aplicación](../environment/deploy.md#on-windows) (versión previa); para un clúster de Kubernetes, vea [Instalar con Helm](../environment/deploy.md#installing-with-helm).

## 1. Instalar {#install}

```sh
curl -fsSL https://rendimiento.ai/instalar | sh
```

Le pregunta su usuario de GitHub (el primer administrador) y, solo cuando tiene que instalar containerd, usa `sudo` una vez. Deja rendimiento corriendo como un servicio de su usuario en **<http://localhost:8080>**, y al terminar le dice cómo leer el **token de configuración**:

```sh
cat ~/.config/rendimiento/ficha-configuracion
```

## 2. Copiar el ejemplo {#copy-the-example}

[`rendimiento-ai/hola`](https://github.com/rendimiento-ai/hola) es la aplicación más pequeña que rendimiento puede desplegar: una página, servida por nginx, con su `rendimiento.yaml`. Abra **[Use this template](https://github.com/rendimiento-ai/hola/generate)** y cree la copia en su cuenta (pública o privada, las dos sirven).

Con una aplicación suya, sáltese este paso: rendimiento lee el repositorio y resuelve cómo construirlo.

## 3. Conectar GitHub {#connect-github}

Abra <http://localhost:8080>. La página de configuración hace esto una sola vez:

1. Pegue el **token de configuración**.
2. Oprima **Crear la aplicación de GitHub**. GitHub se abre con el nombre y los permisos ya llenos; confirme.
3. GitHub pregunta qué repositorios puede ver la aplicación: elija su copia de `hola` (o todos; se puede cambiar después).
4. De vuelta en rendimiento, **Iniciar sesión con GitHub**.

La aplicación de GitHub es suya, en su cuenta: el código de rendimiento nunca pasa por servidores de alguien más.

## 4. Desplegar {#deploy}

Oprima **Nueva aplicación**, elija su copia de `hola` en la lista (**¿Falta un repositorio?** lo lleva a GitHub para darle acceso a la aplicación) y oprima **Desplegar**. La página de la ejecución muestra la construcción mientras ocurre; cuando termina, la aplicación responde en

**<http://hola.localhost:8000>**

## 5. Cambiarla {#change-it}

Edite el saludo en `index.html` y envíelo a `main`. En menos de un minuto rendimiento ve el envío, lo construye y reemplaza la versión que corre sin interrupción. (GitHub no alcanza a `localhost` con un aviso web, así que en una computadora rendimiento le pregunta a GitHub cada minuto.)

## Cuánto tarda {#how-long-it-takes}

Cada versión de rendimiento, y cada cambio al instalador, se instala en una máquina amd64 nueva (de GitHub, con una conexión rápida), despliega `hola` y se desinstala; la ejecución falla cuando la instalación y el despliegue juntos pasan de diez minutos ([la prueba del inicio rápido](../develop/testing.md)). En noviembre de 2026 midió:

| Paso | La computadora tenía Docker | Sin motor de contenedores |
|---|---|---|
| El instalador, hasta que rendimiento responde | 25 s | 31 s |
| De **Desplegar** hasta que la dirección responde | 15 s | 13 s |
| Los dos | 40 s | 44 s |

Los pasos 2 y 3 son una persona en las páginas de GitHub, y la prueba no los cuenta: ahí un sustituto responde por GitHub. Cuente con unos minutos la primera vez.

## Quitarlo otra vez {#take-it-off-again}

```sh
curl -fsSL https://rendimiento.ai/instalar | sh -s -- --desinstalar
```

Detiene y quita rendimiento, los contenedores de sus aplicaciones y el programa, y pregunta antes de borrar sus datos. La aplicación de GitHub se queda en su cuenta (Settings → Developer settings → GitHub Apps), donde puede borrarla.

## A dónde seguir {#where-to-go-next}

- [Un recorrido por la interfaz](tour.md): cada página y para qué sirve.
- [Referencia de rendimiento.yaml](../guide/spec.md): servicios, bases de datos con `needs: [postgres]`, pruebas, tamaños.
- [Hecho para agentes](../guide/agents.md): dele una llave a su agente de código, por MCP.
- [En una computadora](../environment/deploy.md#on-one-computer): qué dejó el instalador, y qué no está todavía.
