package wsl

import (
	"context"
	"regexp"
)

// rutaFicha is where rendimiento keeps the setup token it made on its
// first start, inside the distribution.
const rutaFicha = "/root/.config/rendimiento/ficha-configuracion"

var soloHex = regexp.MustCompile(`^[0-9a-fA-F]+$`)

// Ficha is the setup token rendimiento made on its first start, for the
// setup page, where the person picks where their code lives (GitHub,
// GitLab or Bitbucket) and becomes the first administrator by authorizing
// it. It is a secret: never log it.
func (d *Distro) Ficha(ctx context.Context) (string, error) {
	out, err := d.wsl(ctx, esperaDentro, nil, "-d", d.Nombre, "-u", "root", "--exec", "cat", rutaFicha)
	if err != nil {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		// Not a word of what it printed: a part of the token could be there.
		return "", &Problema{Clave: clave(out), Detalle: "the setup token could not be read: " + err.Error()}
	}
	// The token is alone unless wsl.exe added a warning of its own.
	todas := lineas(out)
	if len(todas) == 1 {
		return todas[0], nil
	}
	for i := len(todas) - 1; i >= 0; i-- {
		if soloHex.MatchString(todas[i]) {
			return todas[i], nil
		}
	}
	return "", &Problema{Clave: Otro, Detalle: "the setup token is not in " + rutaFicha}
}
