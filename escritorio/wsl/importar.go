package wsl

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// archivo is the distribution as each release publishes it. Only amd64 is.
const archivo = "rendimiento-wsl-amd64.tar.gz"

var arquitectura = runtime.GOARCH

// Importar downloads the latest release's distribution into Carpeta,
// checks it against the release's SHA256SUMS and imports it. avance (may
// be nil) hears how many bytes of the download are done, and of how many
// (the same number when the server did not say). A distribution that is
// already imported is left as it is.
func (d *Distro) Importar(ctx context.Context, avance func(hecho, total int64)) error {
	if arquitectura != "amd64" {
		return &Problema{Clave: Otro, Detalle: "the distribution is published for amd64 only, and this computer is " + arquitectura}
	}
	estado, err := d.mirar(ctx)
	switch {
	case err != nil:
		return err
	case estado == SinWSL:
		return &Problema{Clave: Otro, Detalle: "WSL is not installed"}
	case estado == "":
		return nil
	}
	if err := os.MkdirAll(d.disco(), 0o755); err != nil {
		return &Problema{Clave: Otro, Detalle: err.Error()}
	}
	suma, err := d.sumaPublicada(ctx)
	if err != nil {
		return err
	}
	// A download that is whole stays from a try that could not import
	// (Windows had to restart): it is not fetched again.
	destino := filepath.Join(d.Carpeta, archivo)
	if tiene, _ := sumaDe(destino); tiene != suma {
		if err := d.descargar(ctx, destino, suma, avance); err != nil {
			return err
		}
	}
	// What was known of an earlier distribution by this name is not true
	// of the new one.
	_ = os.Remove(d.marca())
	out, err := d.wsl(ctx, 0, nil, "--import", d.Nombre, d.disco(), destino, "--version", "2")
	switch {
	case err == nil, strings.Contains(out, "ERROR_ALREADY_EXISTS"): // imported meanwhile: it is there
	case ctx.Err() != nil:
		return ctx.Err()
	default:
		return problema(out, err)
	}
	_ = os.Remove(destino)
	return nil
}

// sumaPublicada is the distribution's SHA-256 in the release's SHA256SUMS
// (lines of "<hex>  <file name>").
func (d *Distro) sumaPublicada(ctx context.Context) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, esperaHTTP)
	defer cancel()
	resp, err := d.bajar(ctx, d.Descargas+"/SHA256SUMS")
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	lineas := bufio.NewScanner(io.LimitReader(resp.Body, 1<<20))
	for lineas.Scan() {
		if f := strings.Fields(lineas.Text()); len(f) == 2 && strings.TrimPrefix(f[1], "*") == archivo {
			return strings.ToLower(f[0]), nil
		}
	}
	if err := lineas.Err(); err != nil {
		return "", &Problema{Clave: Descarga, Detalle: "SHA256SUMS: " + err.Error()}
	}
	return "", &Problema{Clave: Descarga, Detalle: "the release's SHA256SUMS does not name " + archivo}
}

// bajar asks for a release's file; anything but a 200 is a Problema.
func (d *Distro) bajar(ctx context.Context, url string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, &Problema{Clave: Descarga, Detalle: err.Error()}
	}
	resp, err := d.cliente().Do(req)
	if err != nil {
		return nil, &Problema{Clave: Descarga, Detalle: err.Error()}
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, &Problema{Clave: Descarga, Detalle: fmt.Sprintf("%s: %s", url, resp.Status)}
	}
	return resp, nil
}

// descargar brings the distribution to destino through a .part file
// beside it, so that destino is only ever a whole file with the right sum.
func (d *Distro) descargar(ctx context.Context, destino, suma string, avance func(hecho, total int64)) (err error) {
	resp, err := d.bajar(ctx, d.Descargas+"/"+archivo)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return err
	}
	defer resp.Body.Close()
	parte := destino + ".part"
	f, err := os.Create(parte)
	if err != nil {
		return &Problema{Clave: Descarga, Detalle: err.Error()}
	}
	defer func() {
		f.Close()
		if err != nil {
			os.Remove(parte)
		}
	}()

	var (
		h      = sha256.New()
		buf    = make([]byte, 256<<10)
		hecho  int64
		ultimo time.Time
	)
	for {
		n, errLeer := resp.Body.Read(buf)
		if n > 0 {
			if _, err := f.Write(buf[:n]); err != nil {
				return &Problema{Clave: Descarga, Detalle: err.Error()}
			}
			h.Write(buf[:n])
			hecho += int64(n)
			// 174 MB come in thousands of pieces; a window wants a few a second.
			if avance != nil && resp.ContentLength > 0 && time.Since(ultimo) >= 100*time.Millisecond {
				avance(hecho, resp.ContentLength)
				ultimo = time.Now()
			}
		}
		if errLeer == io.EOF {
			break
		}
		if errLeer != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return &Problema{Clave: Descarga, Detalle: errLeer.Error()}
		}
	}
	if err := f.Close(); err != nil {
		return &Problema{Clave: Descarga, Detalle: err.Error()}
	}
	if tiene := hex.EncodeToString(h.Sum(nil)); tiene != suma {
		return &Problema{Clave: Suma, Detalle: fmt.Sprintf("%s has sha256 %s, and the release says %s", archivo, tiene, suma)}
	}
	if avance != nil {
		avance(hecho, hecho)
	}
	if err := os.Rename(parte, destino); err != nil {
		return &Problema{Clave: Descarga, Detalle: err.Error()}
	}
	return nil
}

// sumaDe is the SHA-256 of a file.
func sumaDe(ruta string) (string, error) {
	f, err := os.Open(ruta)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
