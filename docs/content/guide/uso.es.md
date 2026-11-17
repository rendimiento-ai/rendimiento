# Uso de la interfaz

rendimiento anota cómo se usan sus propias páginas, para quitar clics donde más cuestan: qué páginas abre la gente, desde cuál, cuánto tardan en mostrar sus datos y qué botones principales se usan. Se guarda en la base de datos de rendimiento, en totales por día, y nunca dice quién. Los administradores de la plataforma lo ven en la página **Uso** (**Miembros → Uso de la interfaz**, o `/uso`), y ahí lo apagan.

## Qué se anota {#what-is-noted}

En cada paso dentro de la interfaz, el navegador forma un renglón y envía la fila cada 15 segundos, y cuando la pestaña se oculta o se cierra:

- **La página abierta, por su tipo:** su patrón de ruta, como `/apps/:name/runs/:id` («una ejecución»), nunca su dirección. Ni el nombre de la aplicación, ni el número de la ejecución, ni una búsqueda.
- **La página de la que venía**, de la misma forma (nada en la primera página de una visita).
- **Cuánto tardó la página en mostrar sus datos:** desde que cambia la dirección hasta que la página pone sus datos en pantalla. Solo se miden las páginas que avisan cuando ya tienen sus datos: Aplicaciones, una aplicación, una ejecución, el Universo, una solicitud, Entorno y la página de Uso. Las demás solo cuentan sus visitas, en vez de un tiempo equivocado. Un tiempo de más de 60 segundos, o en una pestaña oculta, no se anota.
- **Los botones principales que se usan**, con un nombre corto y fijo: *Construir ahora*, *Reintentar una ejecución*, *Cancelar una ejecución*, *Integrar esta rama*, *Revertir*, *Descartar un problema*, *Preguntarle al agente por qué falló*, *Abrir un paso*, *Cambiar entre día y noche*, *Cambiar el idioma*.

El servidor acepta solo los patrones de ruta de la propia interfaz y esa lista cerrada de acciones, a lo más 100 renglones a la vez, y rechaza todo lo demás. Guarda **totales por día** (UTC): cuántas veces se abrió cada página, cada paso de una página a otra y cada acción, y de los tiempos un conteo, una suma y cinco cubetas (menos de 0.5 s, 1 s, 2 s, 4 s, y 4 s o más). Los días de hace más de 90 se borran.

## Qué nunca se anota {#what-is-never-noted}

- **Quién:** ni la cuenta, ni la sesión, ni la dirección de la computadora. Un envío tampoco se escribe en la [bitácora](../architecture/security.md#the-bitacora), que lo ataría a quien lo mandó.
- Nada de lo que la gente escribe, ni lo que muestran las páginas.
- Nada de un enlace de demostración de solo lectura.
- Nada sale de la instalación: el uso no forma parte de las [señales de vida](senales.md).

## Cómo apagarlo {#turning-it-off}

Viene encendido. Un administrador de la plataforma lo apaga con el interruptor de arriba en la página **Uso**; la interfaz lo sabe al abrirse y desde entonces no envía nada. Lo que ya se contó se queda hasta que pasan sus 90 días.

## Cómo leer la página de Uso {#reading-the-use-page}

Elija los últimos 7, 14 o 30 días.

- **Páginas más abiertas:** dónde pasa la gente sus visitas.
- **Las que más tardan en mostrar sus datos:** el tiempo promedio de cada página medida; ponga el puntero sobre un renglón para ver qué parte de las visitas tardó 2 segundos o más. La tabla de abajo tiene los mismos números.
- **Los pasos más comunes:** de una página a la siguiente. Un paso que se repite mucho entre dos páginas es candidato a un atajo.
- **Las acciones más usadas:** qué botones, en qué página.
- **El camino común más largo:** una estimación. Desde la primera página más común sigue, en cada página, la siguiente más común, mientras ese paso siga siendo común. No necesariamente lo recorrieron completo las mismas personas; muestra cuántos clics cuesta una tarea frecuente.

Los números empiezan a aparecer conforme la gente usa la interfaz.
