package wsl

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// The commands sent into the distribution are fixed: what a person types
// (the login) only ever travels on standard input.
const (
	leerEnv     = "cat /etc/rendimiento/env 2>/dev/null || true"
	escribirEnv = "umask 077; mkdir -p /etc/rendimiento; cat > /etc/rendimiento/env"
	rutaFicha   = "/root/.config/rendimiento/ficha-configuracion"
)

var (
	loginValido = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9-]{0,38})$`)
	lineaEnv    = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*=`)
	soloHex     = regexp.MustCompile(`^[0-9a-fA-F]+$`)
)

// Configurar writes who signs in first (ALLOWED_USERS in
// /etc/rendimiento/env) and restarts rendimiento inside the distribution.
func (d *Distro) Configurar(ctx context.Context, usuario string) error {
	usuario = strings.TrimSpace(usuario)
	if !loginValido.MatchString(usuario) {
		return &Problema{Clave: Usuario, Detalle: "a GitHub login is letters, digits and hyphens, 39 at most"}
	}
	env, err := d.dentro(ctx, nil, "sh", "-c", leerEnv)
	if err != nil {
		return err
	}
	if _, err := d.dentro(ctx, []byte(conUsuario(env, usuario)), "sh", "-c", escribirEnv); err != nil {
		return err
	}
	if _, err := d.dentro(ctx, nil, "systemctl", "restart", "rendimiento"); err != nil {
		return err
	}
	d.marcar(usuario)
	return nil
}

// conUsuario is the env file with ALLOWED_USERS set to usuario and its
// other settings kept. Lines that are not settings or comments go: wsl.exe
// can print a warning of its own with the file.
func conUsuario(env, usuario string) string {
	var out []string
	puesto := false
	for _, l := range strings.Split(env, "\n") {
		l = strings.TrimSpace(l)
		switch {
		case strings.HasPrefix(l, "ALLOWED_USERS="):
			if !puesto {
				out = append(out, "ALLOWED_USERS="+usuario)
				puesto = true
			}
		case strings.HasPrefix(l, "#"), lineaEnv.MatchString(l):
			out = append(out, l)
		}
	}
	if !puesto {
		out = append(out, "ALLOWED_USERS="+usuario)
	}
	return strings.Join(out, "\n") + "\n"
}

// usuarioDe is who may sign in by an env file ("" when nobody).
func usuarioDe(env string) string {
	for _, l := range lineas(env) {
		if v, ok := strings.CutPrefix(l, "ALLOWED_USERS="); ok {
			if v = strings.Trim(v, `"' `); v != "" {
				return v
			}
		}
	}
	return ""
}

// marca is a file on the Windows side that says the distribution has its
// ALLOWED_USERS: asking the distribution itself would start it, and Estado
// is asked often. Configurar writes it; Estado does, once, for a
// distribution imported and configured by hand.
func (d *Distro) marca() string { return filepath.Join(d.Carpeta, "usuario") }

// marcar remembers; when it cannot, Estado asks the distribution again.
func (d *Distro) marcar(usuario string) {
	if os.MkdirAll(d.Carpeta, 0o755) == nil {
		_ = os.WriteFile(d.marca(), []byte(usuario+"\n"), 0o644)
	}
}

// Ficha is the setup token rendimiento made on its first start, for the
// page that connects GitHub. It is a secret: never log it.
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
