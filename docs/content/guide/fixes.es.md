# Arreglos declarados

Algunas fallas no están en el código sino en un archivo que escribe una herramienta: un `package-lock.json` que ya no coincide con `package.json`, un `go.sum` sin la entrada de un módulo nuevo. Nadie debería editarlos a mano, y el agente de código no puede correr `npm`. Un **arreglo declarado** es el comando que regenera ese archivo, escrito en `rendimiento.yaml` bajo `fixes:`. rendimiento lo corre en la rama que lo necesita, en un pod de su espacio de nombres de construcción, y conserva solo los archivos que el arreglo puede escribir, como una confirmación en esa misma rama, cuya ejecución normal los prueba.

```yaml
fixes:
  - name: candado-web           # una receta: su comando, su imagen y sus archivos
    recipe: npm
    path: web
  - name: candado-movil         # un comando propio de la aplicación
    path: mobile
    image: node:22-bookworm
    command: npm install --package-lock-only --ignore-scripts --no-audit --no-fund
    writes: [package-lock.json]
    timeout: 600
```

Los campos están en la [referencia](spec.md#fixes).

## Recetas {#recipes}

Una receta nombra un arreglo conocido. Trae su comando, los archivos que puede escribir y su imagen, cuya versión sale de lo que la aplicación ya usa en esa carpeta.

| Receta | Comando | Puede escribir | La versión de la imagen sale de |
|---|---|---|---|
| `npm` | `npm install --package-lock-only --ignore-scripts --no-audit --no-fund` (con el npm que nombre `packageManager`, si lo nombra) | `package-lock.json` | `.nvmrc`, `.node-version`, `engines.node`, el `Dockerfile` de la carpeta (`FROM node:…`); si no, Node 22 |
| `pnpm` | `pnpm install --lockfile-only --ignore-scripts`, con corepack | `pnpm-lock.yaml` | como en npm |
| `yarn` | Yarn 2 en adelante: `yarn install --mode update-lockfile` sin guiones de instalación; Yarn 1: `yarn install --ignore-scripts` | `yarn.lock` | como en npm |
| `go` | `go mod tidy` | `go.mod`, `go.sum` | la línea `toolchain` o `go` de `go.mod`, el `Dockerfile` (`FROM golang:…`); si no, Go 1.24 |
| `uv` | `uv lock` | `uv.lock` | `.python-version`, el `requires-python` de `pyproject.toml`, el `Dockerfile`; si no, Python 3.12 |
| `poetry` | `poetry lock` | `poetry.lock` | como en uv |
| `pip-tools` | `pip-compile` | `requirements.txt` | como en uv |

El registro del arreglo dice qué imagen usó y por qué ("version from .nvmrc", o "no version found in the folder: the default"). Un comando propio de la aplicación (`image`, `command`, `writes`) corre tal como está escrito.

## Cómo corre un arreglo {#how-a-fix-runs}

1. **Alguien lo pide**: el agente de código (`run_fix`), una persona con el botón en la página de una ejecución que falló, o rendimiento mismo en una rama de Renovate.
2. **La definición sale de la rama principal.** rendimiento lee `fixes:` del `rendimiento.yaml` de la rama principal, nunca de la rama que arregla: una solicitud de incorporación no puede agregarse un comando y hacer que corra. Un arreglo tampoco corre nunca en la rama principal: esta solo recibe lo que una persona integra.
3. **Un pod en el espacio de nombres de construcción** (`rendimiento-builds`) clona la rama en la confirmación, pone encima los archivos que quien lo pide lleva sin guardar (los cambios del agente de código) y corre el comando con `sh -c` en la carpeta del arreglo. No recibe secretos ni el entorno de la aplicación, solo `CI=true`; tiene red, para los registros de paquetes. Las recetas pasan `--ignore-scripts` (o lo equivalente de la herramienta), así que no corren los guiones de instalación de los paquetes.
4. **Solo vuelve lo que puede escribir**: los archivos que cambiaron dentro de su carpeta y coinciden con sus patrones, hasta 20 MB. Lo demás que haya cambiado se deja fuera, y el registro lo dice.
5. **Nada se guarda sin probarse.** Los archivos se vuelven una confirmación en la misma rama (o se suman a los cambios del agente de código, que su siguiente `commit_and_test` guarda), y la ejecución normal de la rama los prueba.

Un arreglo se detiene pasado su `timeout` (cinco minutos si no dice otra cosa, y nunca más que el límite de los pasos de la plataforma). Cada arreglo guarda su registro completo, como un paso, con quién lo pidió, los archivos que escribió y lo que dejó fuera.

## Dónde lo ve usted {#where-you-see-it}

**En la página de una ejecución que falló.** Cuando el registro de un paso que falló dice que un archivo de bloqueo está desactualizado (el *package.json and package-lock.json are not in sync* o *lock file … does not satisfy* de npm, el `ERR_PNPM_OUTDATED_LOCKFILE` de pnpm, el *lockfile needs to be updated* de yarn, el *missing go.sum entry* o *updates to go.mod needed* de Go, los avisos de uv y de Poetry de que su archivo de bloqueo no coincide) y la aplicación declara un arreglo cuya carpeta contiene la del paso, la página lo dice y ofrece:

- **Regenerar &lt;archivo&gt;**: corre el arreglo en la rama de la ejecución y guarda lo que escribió en una confirmación, con usted como coautor; sigue una ejecución nueva. No interviene la IA, así que no cuesta dinero de IA. Lo puede pulsar cualquiera que pueda lanzar ejecuciones; nunca aparece para la rama principal.
- **Pedírselo al agente**: un encargo para el agente de código, que corre el arreglo con `run_fix` y sigue desde ahí.

Cuando la aplicación no declara un arreglo para esa carpeta, la página ofrece en su lugar **Agregar el arreglo &lt;receta&gt; a rendimiento.yaml**: una rama, hecha desde la principal, que agrega la receta a `fixes:` y conserva los comentarios del archivo. Usted abre su solicitud de incorporación y la integra; desde entonces el botón regenera el archivo.

**En la historia de un encargo.** Cada `run_fix` del agente de código es una línea, con los archivos que escribió y cuánto tardó.

**En una rama de Renovate, por sí solo.** Vea [Renovate](renovate.md#lockfiles).

## Límites {#limits}

- Solo corre lo declarado. rendimiento reconoce el tipo de proyecto, pero no ofrece recetas que nadie declaró.
- Un arreglo propio de la aplicación (un comando, no una receta) corre solo cuando alguien lo pide: las ramas de Renovate reciben solo recetas.
- En una computadora sin Kubernetes (`MODE=local`) los arreglos todavía no corren: no hay espacio de nombres de construcción.
