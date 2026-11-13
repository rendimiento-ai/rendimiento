package destino

import (
	"context"
	"fmt"
	"strconv"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/rendimiento-ai/rendimiento/api/v1alpha1"
	"github.com/rendimiento-ai/rendimiento/internal/render"
	"github.com/rendimiento-ai/rendimiento/internal/store"
)

// Kubernetes runs releases in the cluster: each app is an App object, which
// the controller turns into its namespace, workloads, ingresses and needs.
type Kubernetes struct {
	Kube client.Client
	// Clientset runs deploy tasks (Jobs in app namespaces) and reads their
	// logs.
	Clientset kubernetes.Interface
	// Org is the address of an app's organization, by the app's id; the
	// App object carries it (render.LabelOrg) to the cluster. Nil: every
	// app is the platform's.
	Org func(ctx context.Context, appID int64) (string, error)
}

// org is the address of the app's organization.
func (k Kubernetes) org(ctx context.Context, appID int64) (string, error) {
	if k.Org == nil {
		return render.PlatformOrg, nil
	}
	o, err := k.Org(ctx, appID)
	if err != nil {
		return "", fmt.Errorf("the app's organization: %w", err)
	}
	return o, nil
}

// Nombre is "kubernetes".
func (k Kubernetes) Nombre() string { return "kubernetes" }

// Crear writes the app's App object, labeled with its id and its
// organization. An app with
// nothing to run (a library, or only builds) gets none.
func (k Kubernetes) Crear(ctx context.Context, app *store.App, adopt bool) error {
	if !app.Spec.Runs() {
		return nil
	}
	org, err := k.org(ctx, app.ID)
	if err != nil {
		return err
	}
	cr := &v1alpha1.App{
		ObjectMeta: metav1.ObjectMeta{Name: app.Name, Labels: map[string]string{"rendimiento.ai/app-id": strconv.FormatInt(app.ID, 10), render.LabelOrg: org}},
		Spec: v1alpha1.AppSpec{Repo: app.Repo, Services: app.Spec.Services, Jobs: app.Spec.Jobs, SharedNamespace: app.Spec.SharedNamespace,
			Postgres: app.Spec.Postgres, Redis: app.Spec.Redis, Observe: app.Spec.Observe, Adopt: adopt},
	}
	if err := k.Kube.Create(ctx, cr); err != nil {
		return fmt.Errorf("create App object: %w", err)
	}
	return nil
}

// Aplicar writes the release into the App object; the controller does the
// rest. It also labels the App object with the app's organization, so
// Apps made before organizations reached the cluster get it on their next
// release. A conflict (the controller wrote its status meanwhile) is retried.
func (k Kubernetes) Aplicar(ctx context.Context, app string, rel *store.Release) error {
	org, err := k.org(ctx, rel.AppID)
	if err != nil {
		return err
	}
	for attempt := 0; ; attempt++ {
		var cr v1alpha1.App
		if err := k.Kube.Get(ctx, client.ObjectKey{Name: app}, &cr); err != nil {
			return err
		}
		if cr.Labels == nil {
			cr.Labels = map[string]string{}
		}
		cr.Labels[render.LabelOrg] = org
		cr.Spec.Services = rel.Spec.Services
		cr.Spec.Jobs = rel.Spec.Jobs
		cr.Spec.SharedNamespace = rel.Spec.SharedNamespace
		cr.Spec.Postgres, cr.Spec.Redis = rel.Spec.Postgres, rel.Spec.Redis
		cr.Spec.Observe = rel.Spec.Observe
		cr.Spec.Images = rel.Images
		cr.Spec.Release = rel.Number
		err = k.Kube.Update(ctx, &cr)
		if apierrors.IsConflict(err) && attempt < 5 {
			continue
		}
		return err
	}
}

// Estado reads the App object: healthy once the controller reports the
// release Healthy, failed when it reports it Degraded or in Error.
func (k Kubernetes) Estado(ctx context.Context, app string, release int64) (Estado, string, error) {
	var cr v1alpha1.App
	if err := k.Kube.Get(ctx, client.ObjectKey{Name: app}, &cr); err != nil {
		return Pendiente, "", err
	}
	if cr.Spec.Release != release {
		return Reemplazada, "", nil
	}
	if cr.Status.Release != release || cr.Status.ObservedGeneration < cr.Generation {
		return Pendiente, "the controller has not applied it yet", nil
	}
	switch cr.Status.Phase {
	case v1alpha1.PhaseHealthy:
		return Sana, "", nil
	case v1alpha1.PhaseDegraded, v1alpha1.PhaseError:
		msg := cr.Status.Message
		for _, s := range cr.Status.Services {
			if s.Message != "" {
				msg = s.Name + ": " + s.Message
				break
			}
		}
		return Fallida, msg, nil
	}
	return Pendiente, string(cr.Status.Phase), nil
}

// Quitar deletes the App object; the controller then removes DNS and,
// through owner references, the namespace.
func (k Kubernetes) Quitar(ctx context.Context, app string) error {
	err := k.Kube.Delete(ctx, &v1alpha1.App{ObjectMeta: metav1.ObjectMeta{Name: app}})
	if apierrors.IsNotFound(err) {
		return nil
	}
	return err
}
