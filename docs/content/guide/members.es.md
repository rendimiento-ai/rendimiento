# Miembros y funciones {#members-and-roles}

Las personas entran a rendimiento con GitHub, y solo pasan los **miembros**. Cada miembro tiene una **función** que dice qué puede hacer; cada ruta de la API la comprueba, así que la interfaz y la API coinciden.

| Función | Puede |
|---|---|
| **Invitado** | ver todo: las aplicaciones, las ejecuciones y sus registros, las versiones, el [Universo](universe.md), la [Ruta](ruta.md) y los encargos |
| **Colaborador** | además: lanzar CI, cancelar ejecuciones, escribir en la Ruta, [pedirles cosas a los agentes](agents.md#encargos) y revisar lo que entregan, editar páginas del [Mercado](mercado.md), encender Renovate en una aplicación |
| **Administrador** | además: desplegar y revertir, leer y cambiar secretos, incorporar y quitar aplicaciones, y manejar complementos, llaves de agentes, miembros y la actualización de la plataforma |

## Agregar a alguien {#adding-someone}

En **Miembros** (su nombre en la barra de arriba), las personas aparecen a la izquierda por función; al elegir una se ven su función y, renglón por renglón, **lo que puede hacer**. Un administrador toca **Agregar a alguien**, escribe el **usuario de GitHub** de la persona y elige su función. Desde ese momento puede entrar, en la misma dirección. Cambiar una función o quitar a alguien cuenta desde su siguiente clic; a quien se quita se le cierra la sesión de inmediato. Una organización siempre conserva al menos un administrador.

## Los primeros administradores {#the-first-administrators}

Una plataforma nueva no tiene miembros, así que el ajuste `ALLOWED_USERS` nombra a los primeros administradores. Se agregan una sola vez, mientras no hay miembros; después, las personas se manejan en la página Miembros y el ajuste ya no cuenta.

## Organizaciones {#organizations}

Todo pertenece a una **organización**: sus aplicaciones (con sus ejecuciones, versiones y solicitudes), su Ruta, sus encargos, sus llaves de agentes y sus miembros. Las personas de una organización nunca ven lo de otra: las listas no lo incluyen y sus páginas responden *no existe*. Una persona puede pertenecer a varias, con una función en cada una, y cambia con el selector de arriba; cuenta la función en la organización en la que está trabajando.

La **organización de la plataforma** (la primera) es la que la opera. Sus administradores son los **operadores**: solo ellos ven las páginas del clúster (Entorno, Complementos, Servicios, Problemas, el Mercado), actualizan la plataforma y, por ahora, crean organizaciones (en Miembros → Organizaciones) e incorporan aplicaciones, en la organización en la que estén trabajando. Una llave de agente pertenece a la organización en la que se creó, y sus herramientas solo ven esa organización.

### Nombres de las aplicaciones {#app-names}

El nombre de una aplicación es también su dirección en rendimiento y su espacio de nombres en el clúster, así que es uno solo en toda la instalación. La organización de la plataforma conserva los nombres que da. En cualquier otra organización el nombre lleva al frente la dirección de la organización: «tienda» en la organización `almacen` es `almacen-tienda`, en rendimiento y en el clúster. Dos organizaciones pueden tener cada una su «tienda», y a ninguna se le dice qué tiene la otra. La página **Nueva aplicación** muestra el nombre completo antes de confirmar.

Tres reglas mantienen separados los nombres:

- La organización de la plataforma no puede nombrar una aplicación con la dirección de otra organización al frente (`almacen-…`).
- La dirección de una organización nueva no puede ser el comienzo de la dirección de otra (`almacen` y `almacen-norte`), ni del nombre de una aplicación que ya existe.
- Con la dirección al frente, un nombre tiene a lo más 63 caracteres, el límite de un espacio de nombres. Conviene que la dirección de una organización sea corta.

Migrar una aplicación que ya corre en el clúster (tomar su espacio de nombres) es solo de la organización de la plataforma.

Todas las organizaciones comparten el clúster de la plataforma. Ahí, el espacio de nombres de cada aplicación lleva la etiqueta de su organización, y el operador puede encender una política de red que solo deja entrar a los espacios de nombres de la misma organización y al controlador de entrada, y una cuota de CPU y memoria por organización. Una sola cuenta de servicio sigue administrando todos los espacios de nombres, y las construcciones comparten un espacio de nombres, BuildKit y registro: vea [Las organizaciones en el clúster](../architecture/security.md#organizations-in-the-cluster). Las organizaciones en un clúster virtual dedicado, o en su propio clúster en cualquier nube, llegan después.

## Ligas de demostración {#demo-links}

Una **liga de demostración** deja que alguien sin cuenta de GitHub aquí (un revisor, un inversionista) recorra una organización en modo de solo lectura. Un administrador la hace al final de **Miembros**, con una nota para sí mismo (para quién es) y cuánto dura, de 7 a 90 días. La dirección, `https://<plataforma>/demo/<código>`, se muestra una sola vez; rendimiento solo guarda el resumen criptográfico del código.

Al abrirla, el navegador entra como el **invitado** propio de esa liga, con *Demostración de solo lectura* en la barra de arriba. El invitado ve las aplicaciones, las ejecuciones, los registros, los encargos y la Ruta de la organización, y nada más: no puede actuar aunque alguien le suba la función, nunca ve las páginas del clúster y no puede cambiarse a otra organización. **Apagar** termina la liga y las sesiones que abrió de inmediato; una liga vencida deja de funcionar sola.

La organización de la plataforma no tiene ligas de demostración, porque sus invitados ven el clúster. Cree una organización para la demostración (Miembros → Organizaciones), cámbiese a ella y haga ahí la liga.
