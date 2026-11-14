package destino

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/rendimiento-ai/rendimiento/internal/spec"
	"github.com/rendimiento-ai/rendimiento/internal/store"
)

// A pull request's copy on this computer (decision 136, step 12): the
// containers of a release of its own, <app>-pr-<n>, on a network of its
// own, with a Postgres and a Redis of its own (empty), reading its app's
// secrets, reached through the proxy at <app>-pr-<n>.localhost. What the
// cluster keeps on the copy's App object, its labels keep here: whose copy
// it is, of which pull request, when its branch last ran and the pull
// request's comment about it.
const (
	etCopiaDe    = "rendimiento.ai/preview-of"
	etCopia      = "rendimiento.ai/preview"
	etEmpujada   = "rendimiento.ai/pushed-at"
	etComentario = "rendimiento.ai/comment"
)

// Copia is a pull request's copy running on this computer.
type Copia struct {
	App        string // the app it copies
	Numero     int    // its pull request
	Empujada   time.Time
	Comentario int64
}

// NombreCopia is the name of pull request numero's copy of app.
func NombreCopia(app string, numero int) string { return fmt.Sprintf("%s-pr-%d", app, numero) }

// Copiar starts pull request numero's copy of app from rel (the copy's spec
// and the images its branch's run built), beside the copy it replaces, as
// a release starts beside the running one. Its domains are the cluster's:
// here it answers at <app>-pr-<n>.localhost.
func (l *Local) Copiar(ctx context.Context, app string, numero int, rel *store.Release, comentario int64) error {
	nombre := NombreCopia(app, numero)
	sp := rel.Spec
	sp.Services = append([]spec.Service(nil), sp.Services...)
	for i := range sp.Services {
		sp.Services[i].Domain, sp.Services[i].Routes = "", nil
	}
	copia := *rel
	copia.Spec = sp
	for _, svc := range sp.Services {
		if svc.Image == "" && copia.Images[svc.Name] == "" {
			return fmt.Errorf("service %s has no image in run %d", svc.Name, rel.Number)
		}
	}
	et := map[string]string{etCopiaDe: app, etCopia: strconv.Itoa(numero), etEmpujada: time.Now().UTC().Format(time.RFC3339)}
	if comentario != 0 {
		et[etComentario] = strconv.FormatInt(comentario, 10)
	}
	l.poner(nombre, estadoLocal{version: rel.Number, estado: Pendiente, mensaje: "starting"})
	go l.desplegar(context.WithoutCancel(ctx), nombre, &copia, despliegue{secretosDe: app, etiquetas: et})
	return nil
}

// Copias are the copies running here: app's ("" for every app's), one per
// pull request, as its newest containers say.
func (l *Local) Copias(ctx context.Context, app string) ([]Copia, error) {
	filtro := etCopia
	if app != "" {
		filtro = etCopiaDe + "=" + app
	}
	names, err := l.Motor.Nombres(ctx, filtro)
	if err != nil {
		return nil, err
	}
	por := map[string]Copia{}
	for _, n := range names {
		info, err := l.Motor.Inspeccionar(ctx, n)
		if err != nil || info.Etiquetas[etParte] != "service" {
			continue
		}
		c := copiaDe(info.Etiquetas)
		if c.Numero == 0 {
			continue
		}
		k := NombreCopia(c.App, c.Numero)
		if cur, ok := por[k]; !ok || c.Empujada.After(cur.Empujada) {
			por[k] = c
		}
	}
	out := make([]Copia, 0, len(por))
	for _, c := range por {
		out = append(out, c)
	}
	return out, nil
}

func copiaDe(et map[string]string) Copia {
	n, _ := strconv.Atoi(et[etCopia])
	at, _ := time.Parse(time.RFC3339, et[etEmpujada])
	id, _ := strconv.ParseInt(et[etComentario], 10, 64)
	return Copia{App: et[etCopiaDe], Numero: n, Empujada: at, Comentario: id}
}

// QuitarCopia removes a copy: its containers, its data and its network.
func (l *Local) QuitarCopia(ctx context.Context, app string, numero int) error {
	return l.Quitar(ctx, NombreCopia(app, numero))
}

// DireccionCopia is where a copy answers: its first service, through the
// proxy.
func (l *Local) DireccionCopia(app string, numero int) string {
	return l.url(NombreCopia(app, numero) + ".localhost")
}
