package controller

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"strings"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/rendimiento-ai/rendimiento/api/v1alpha1"
	"github.com/rendimiento-ai/rendimiento/internal/render"
)

// ---- a copy's secrets (decision 181) ----
//
// A copy of an app (a pull request's, <app>-pr-<n>, or a short one en seco,
// <app>-seco-<run>) runs in a namespace of its own and reads its secrets by
// the app's names. Each one, X, is the app's preview variant X-preview,
// copied from the app's namespace into the copy's as X, with the same type
// and keys. Production's X is never copied: without X-preview the copy has
// no X (its pods reference it as optional, and start without it).
//
// The controller writes them on every reconcile of the copy (each refresh
// from a new push, every few minutes, and at once when an X-preview
// changes: SetupWithManager watches them), owned by the copy's App object
// and in its namespace, so deleting the copy deletes them.

// previewSource is the app's preview variant of one secret a copy reads.
type previewSource struct {
	name   string         // the name the copy reads (X)
	secret *corev1.Secret // X-preview in the app's namespace, nil when there is none
}

// copyOf is the app a copy copies ("" when app is not a copy).
func copyOf(app *v1alpha1.App) string {
	if app.Spec.Preview == 0 {
		return ""
	}
	return app.Labels[render.LabelPreviewOf]
}

// copySecretNames are the secrets a copy reads: its services' (on its App
// object) and its tasks' and tests' (the platform's annotation), except the
// database's and cache's credentials, which the copy makes its own.
func copySecretNames(app *v1alpha1.App) []string {
	names := map[string]bool{}
	for _, svc := range app.Spec.Services {
		for _, n := range svc.SecretNames() {
			names[n] = true
		}
	}
	for _, n := range strings.Split(app.Annotations[render.AnnotationCopySecrets], ",") {
		if n = strings.TrimSpace(n); n != "" {
			names[n] = true
		}
	}
	delete(names, render.PostgresSecret)
	delete(names, render.RedisSecret)
	out := make([]string, 0, len(names))
	for n := range names {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// previewSources reads, for a copy, the app's preview variant of each
// secret it reads, and a digest of which exist and their versions (""
// when the copy reads no secret, or app is not a copy).
func (r *AppReconciler) previewSources(ctx context.Context, app *v1alpha1.App) ([]previewSource, string, error) {
	of := copyOf(app)
	if of == "" {
		return nil, "", nil
	}
	names := copySecretNames(app)
	if len(names) == 0 {
		return nil, "", nil
	}
	h := sha256.New()
	out := make([]previewSource, 0, len(names))
	for _, n := range names {
		var sec corev1.Secret
		err := r.Get(ctx, client.ObjectKey{Namespace: of, Name: render.PreviewSecret(n)}, &sec)
		switch {
		case apierrors.IsNotFound(err):
			out = append(out, previewSource{name: n})
			h.Write([]byte(n + "=\n"))
		case err != nil:
			return nil, "", err
		default:
			out = append(out, previewSource{name: n, secret: &sec})
			// Its identity and version, never its values.
			h.Write([]byte(n + "=" + string(sec.UID) + "/" + sec.ResourceVersion + "\n"))
		}
	}
	return out, hex.EncodeToString(h.Sum(nil))[:16], nil
}

// syncCopySecrets writes, in the copy's namespace, each secret from its
// preview variant, and removes the ones it wrote before whose variant is
// gone or that the copy no longer reads. A secret of the same name that it
// did not write (someone made it by hand) is left alone.
func (r *AppReconciler) syncCopySecrets(ctx context.Context, app *v1alpha1.App, sources []previewSource) error {
	if copyOf(app) == "" {
		return nil
	}
	keep := map[string]bool{}
	for _, src := range sources {
		if src.secret == nil {
			continue
		}
		keep[src.name] = true
		if err := r.writeCopySecret(ctx, app, src); err != nil {
			return err
		}
	}
	var live corev1.SecretList
	if err := r.List(ctx, &live, client.InNamespace(app.Name), client.HasLabels{render.LabelPreviewSecret}); err != nil {
		return err
	}
	for i := range live.Items {
		s := &live.Items[i]
		if keep[s.Name] || s.Labels[render.LabelApp] != app.Name {
			continue
		}
		if err := r.Delete(ctx, s); client.IgnoreNotFound(err) != nil {
			return err
		}
		log.FromContext(ctx).Info("removed a copy's secret: its preview variant is gone", "secret", s.Name)
	}
	return nil
}

// writeCopySecret makes or updates one secret of the copy from its preview
// variant: the same type and data, the copy's labels and owner.
func (r *AppReconciler) writeCopySecret(ctx context.Context, app *v1alpha1.App, src previewSource) error {
	want := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: src.name, Namespace: app.Name, Labels: map[string]string{
			render.LabelManagedBy: render.ManagedBy, render.LabelApp: app.Name, render.LabelPreviewSecret: "true",
		}},
		Type: src.secret.Type,
		Data: map[string][]byte{},
	}
	for k, v := range src.secret.Data {
		want.Data[k] = bytes.Clone(v)
	}
	if err := controllerutil.SetControllerReference(app, want, r.Scheme); err != nil {
		return err
	}
	var existing corev1.Secret
	err := r.Get(ctx, client.ObjectKeyFromObject(want), &existing)
	switch {
	case apierrors.IsNotFound(err):
		if err := r.Create(ctx, want); err != nil && !apierrors.IsAlreadyExists(err) {
			return err
		}
		return nil
	case err != nil:
		return err
	}
	if _, ours := existing.Labels[render.LabelPreviewSecret]; !ours {
		log.FromContext(ctx).Info("a copy's secret was not made by rendimiento: left as it is", "secret", existing.Name)
		return nil
	}
	if existing.Type != want.Type {
		// A secret's type cannot change: make it again.
		if err := r.Delete(ctx, &existing); client.IgnoreNotFound(err) != nil {
			return err
		}
		return r.Create(ctx, want)
	}
	if sameData(existing.Data, want.Data) {
		return nil
	}
	existing.Data = want.Data
	return r.Update(ctx, &existing)
}

// sameData says whether two secrets hold the same keys and values.
func sameData(a, b map[string][]byte) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		w, ok := b[k]
		if !ok || !bytes.Equal(v, w) {
			return false
		}
	}
	return true
}

// copiesReading are the copies that read a changed preview variant: every
// copy of the app whose namespace holds it. A secret that is not a preview
// variant reconciles nothing.
func (r *AppReconciler) copiesReading(ctx context.Context, o client.Object) []reconcile.Request {
	if _, ok := render.IsPreviewSecret(o.GetName()); !ok {
		return nil
	}
	var list v1alpha1.AppList
	if err := r.List(ctx, &list, client.MatchingLabels{render.LabelPreviewOf: o.GetNamespace()}); err != nil {
		log.FromContext(ctx).Error(err, "list the copies of an app")
		return nil
	}
	var out []reconcile.Request
	for _, a := range list.Items {
		if a.Spec.Preview > 0 {
			out = append(out, reconcile.Request{NamespacedName: client.ObjectKey{Name: a.Name}})
		}
	}
	return out
}
