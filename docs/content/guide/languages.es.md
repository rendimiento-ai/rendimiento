# Idiomas

rendimiento habla **inglés** y **español de México** (es-MX).

## En el navegador {#in-the-browser}

El menú de su cuenta (su nombre, a la derecha de la barra superior; en un teléfono, la hoja de abajo) tiene un selector de idioma (**ES** / **EN**). Lo que elija se recuerda en ese navegador. Las fechas y los números también siguen al idioma.

Mientras usted no elija, el idioma se escoge así:

1. el que eligió antes con el selector, en este navegador;
2. si no, el primero de los idiomas del navegador (en su orden de preferencia) que sea español o inglés;
3. si no, cuando el navegador no pide ninguno de los dos, el del país desde el que nos visita, según Cloudflare (el encabezado `CF-IPCountry`): español para México, España y los demás países hispanohablantes; inglés para los demás. El servidor pone en la página solo `es` o `en` (`<meta name="rendimiento-lang-pais">`), nunca el país, y no guarda nada; sin Cloudflare enfrente, este paso no ocurre;
4. si no, inglés.

La API responde de la misma manera (`i18n.FromRequest`). Una solicitud sin `Accept-Language`, como la del rastreador de un buscador o un programa, se responde en inglés, venga de donde venga.

El español es formal (*usted*) y traduce los términos técnicos: *despliegue*, *construcción* (build), *versión*, *reversión*, *complemento*, *confirmación* (commit), *envío* (push), *solicitud de incorporación* (pull request), *cubeta* (bucket), *balanceador de carga*, *entrada* (ingress), *demonio* (daemon).

## En este libro {#in-this-book}

El libro también está en los dos idiomas: el selector está en su encabezado. La versión en español de una página vive junto a ella (`page.es.md` al lado de `page.md`, con [mkdocs-static-i18n](https://github.com/ultrabug/mkdocs-static-i18n)); una página que todavía no está traducida se muestra en inglés dentro del libro en español. Los títulos traducidos conservan su ancla en inglés (`## Título {#english-title}`), así que las ligas entre páginas funcionan en los dos libros.

## En los correos {#in-emails}

`NOTIFY_LANG` define el idioma de los [correos de notificación](notifications.md): `en` (el predeterminado) o `es`.

## Los mensajes de la plataforma {#messages-from-the-platform}

Lo que la plataforma escribe para las personas (las comprobaciones del entorno, los resultados de ejecuciones y versiones, los problemas, los errores de validación, los correos) se escribe en inglés con `i18n.M`, que es `fmt.Sprintf` y además marca el formato para traducirlo. Se traduce **cuando se muestra**, no cuando se crea:

- la API ve `Accept-Language: es` y traduce el JSON con el que responde: las oraciones (`message`, `summary`, `details`, `error`…) mediante los patrones del catálogo; las etiquetas cortas (`name`, `title`) palabra por palabra, o con un patrón solo cuando este tiene suficiente texto fijo propio, así el nombre de una aplicación nunca cambia;
- un mensaje en inglés ya terminado se compara con los formatos del catálogo y se arma el mensaje en español con los mismos valores, que pueden cambiar de lugar (`%[2]s`) y que a su vez se traducen; las listas (`a; b`) se traducen parte por parte.

Así también se traducen los mensajes guardados hace mucho, y el código que crea mensajes no necesita saber el idioma.

## Agregar texto {#adding-text}

- **En la interfaz**, escriba la frase en inglés dentro de `t("…")` (o `tn(n, "uno", "varios")` para una cantidad) y agregue su español en `web/src/i18n/es.ts`. `npm run typecheck` falla si una frase no tiene español o si sus `{marcadores}` no coinciden.
- **En Go**, cree el mensaje con `M("formato %s", valor)` y agregue el español del formato en `internal/i18n/es.go`. `TestCatalog` falla si un formato no tiene español o si tiene un número distinto de valores.
