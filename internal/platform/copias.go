package platform

import (
	"context"
	"fmt"
	"strconv"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/rendimiento-ai/rendimiento/api/v1alpha1"
	"github.com/rendimiento-ai/rendimiento/internal/destino"
	"github.com/rendimiento-ai/rendimiento/internal/render"
	"github.com/rendimiento-ai/rendimiento/internal/spec"
	"github.com/rendimiento-ai/rendimiento/internal/store"
)

// copia is a pull request's copy wherever it runs.
type copia struct {
	App       string // the app it copies
	Number    int    // its pull request
	PushedAt  time.Time
	CommentID int64
}

// Copias keeps the copies of pull requests: App objects in the cluster
// (kubeCopias), or containers on this computer (LocalCopias). The rules of
// when a copy is made and when it goes are the same for both, in
// previas.go.
type Copias interface {
	// List is app's copies ("" for every app's).
	List(ctx context.Context, app string) ([]copia, error)
	// Get is pull request number's copy of app, when there is one.
	Get(ctx context.Context, app string, number int) (copia, bool)
	// Put makes or replaces the copy from a run that passed.
	Put(ctx context.Context, a *store.App, org string, copy spec.Spec, images map[string]string, run *store.Run, number int, commentID int64) error
	// Delete removes the copy; a copy already gone is no error.
	Delete(ctx context.Context, c copia) error
	// URL is where the copy answers.
	URL(copy spec.Spec, app string, number int) string
	// TestURL is where the app's tests reach the copy from where they run
	// ("": its domain, as for a release).
	TestURL(copy spec.Spec) string
}

// copias is where this platform keeps copies (nil: nowhere).
func (p *Platform) copias() Copias {
	if p.Copias != nil {
		return p.Copias
	}
	if p.Kube != nil {
		return kubeCopias{p.Kube}
	}
	return nil
}

// kubeCopias keeps each copy as an App object of its own, <app>-pr-<n>; the
// controller deploys it in its own namespace like any app.
type kubeCopias struct{ kube client.Client }

func (k kubeCopias) List(ctx context.Context, app string) ([]copia, error) {
	var list v1alpha1.AppList
	opts := []client.ListOption{client.HasLabels{render.LabelPreview}}
	if app != "" {
		opts = []client.ListOption{client.MatchingLabels{labelPreviewOf: app}}
	}
	if err := k.kube.List(ctx, &list, opts...); err != nil {
		return nil, err
	}
	var out []copia
	for _, a := range list.Items {
		if a.Spec.Preview > 0 {
			out = append(out, copiaOf(a))
		}
	}
	return out, nil
}

func copiaOf(a v1alpha1.App) copia {
	id, _ := strconv.ParseInt(a.Annotations[annotationComment], 10, 64)
	at, err := time.Parse(time.RFC3339, a.Annotations[annotationPushed])
	if err != nil {
		at = a.CreationTimestamp.Time
	}
	return copia{App: a.Labels[labelPreviewOf], Number: a.Spec.Preview, PushedAt: at, CommentID: id}
}

func (k kubeCopias) Get(ctx context.Context, app string, number int) (copia, bool) {
	var cr v1alpha1.App
	if err := k.kube.Get(ctx, client.ObjectKey{Name: PreviewName(app, number)}, &cr); err != nil || cr.Spec.Preview == 0 {
		return copia{}, false
	}
	return copiaOf(cr), true
}

func (k kubeCopias) Put(ctx context.Context, a *store.App, org string, copy spec.Spec, images map[string]string, run *store.Run, number int, commentID int64) error {
	name := PreviewName(a.Name, number)
	for attempt := 0; ; attempt++ {
		var cr v1alpha1.App
		err := k.kube.Get(ctx, client.ObjectKey{Name: name}, &cr)
		creating := apierrors.IsNotFound(err)
		if err != nil && !creating {
			return err
		}
		cr.Name = name
		cr.Labels = map[string]string{"rendimiento.ai/app-id": strconv.FormatInt(a.ID, 10), render.LabelOrg: org,
			render.LabelPreview: strconv.Itoa(number), labelPreviewOf: a.Name}
		cr.Annotations = map[string]string{annotationPushed: time.Now().UTC().Format(time.RFC3339)}
		if commentID != 0 {
			cr.Annotations[annotationComment] = strconv.FormatInt(commentID, 10)
		}
		cr.Spec = v1alpha1.AppSpec{Repo: a.Repo, Services: copy.Services, Postgres: copy.Postgres, Redis: copy.Redis,
			Observe: copy.Observe, Images: images, Release: run.ID, Preview: number}
		if creating {
			err = k.kube.Create(ctx, &cr)
		} else {
			err = k.kube.Update(ctx, &cr)
		}
		if apierrors.IsConflict(err) && attempt < 5 {
			continue
		}
		return err
	}
}

func (k kubeCopias) Delete(ctx context.Context, c copia) error {
	cr := &v1alpha1.App{}
	cr.Name = PreviewName(c.App, c.Number)
	if err := k.kube.Delete(ctx, cr); err != nil && !apierrors.IsNotFound(err) {
		return err
	}
	return nil
}

func (k kubeCopias) URL(copy spec.Spec, _ string, _ int) string { return previewURL(copy) }

func (k kubeCopias) TestURL(spec.Spec) string { return "" }

// LocalCopias keeps copies on this computer, as the local destination's
// containers (destino.Local.Copiar), at <app>-pr-<n>.localhost.
type LocalCopias struct{ Local *destino.Local }

func (l LocalCopias) List(ctx context.Context, app string) ([]copia, error) {
	got, err := l.Local.Copias(ctx, app)
	if err != nil {
		return nil, err
	}
	out := make([]copia, 0, len(got))
	for _, c := range got {
		out = append(out, copia{App: c.App, Number: c.Numero, PushedAt: c.Empujada, CommentID: c.Comentario})
	}
	return out, nil
}

func (l LocalCopias) Get(ctx context.Context, app string, number int) (copia, bool) {
	all, err := l.List(ctx, app)
	if err != nil {
		return copia{}, false
	}
	for _, c := range all {
		if c.Number == number {
			return c, true
		}
	}
	return copia{}, false
}

func (l LocalCopias) Put(ctx context.Context, a *store.App, _ string, copy spec.Spec, images map[string]string, run *store.Run, number int, commentID int64) error {
	return l.Local.Copiar(ctx, a.Name, number, &store.Release{Number: run.ID, Spec: copy, Images: images}, commentID)
}

func (l LocalCopias) Delete(ctx context.Context, c copia) error {
	return l.Local.QuitarCopia(ctx, c.App, c.Number)
}

func (l LocalCopias) URL(_ spec.Spec, app string, number int) string {
	return l.Local.DireccionCopia(app, number)
}

// TestURL is the copy's first service with a port by its name, on the
// copy's own network, where its tests run: the copy has no domain here.
func (l LocalCopias) TestURL(copy spec.Spec) string {
	for _, svc := range copy.Services {
		if svc.Port > 0 {
			return fmt.Sprintf("http://%s:%d", svc.Name, svc.Port)
		}
	}
	return ""
}
