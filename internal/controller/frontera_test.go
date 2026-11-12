package controller

import (
	"context"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/rendimiento-ai/rendimiento/api/v1alpha1"
	"github.com/rendimiento-ai/rendimiento/internal/render"
	"github.com/rendimiento-ai/rendimiento/internal/spec"
)

// fronteraApp is an app of one service, released, in org (""; no label:
// an App made before organizations reached the cluster).
func fronteraApp(name, org string, lan bool) *v1alpha1.App {
	svc := spec.Service{Name: "web", Path: ".", Port: 8080, Size: spec.SizeSmall, Replicas: 1, Build: spec.Build{Dockerfile: "Dockerfile"}}
	if lan {
		svc.LAN = &spec.LANOptions{Port: 8443}
	}
	a := &v1alpha1.App{ObjectMeta: metav1.ObjectMeta{Name: name}, Spec: v1alpha1.AppSpec{
		Services: []spec.Service{svc}, Images: map[string]string{"web": "reg/web@sha256:1"}}}
	if org != "" {
		a.Labels = map[string]string{render.LabelOrg: org}
	}
	return a
}

// With the boundary on, each namespace rendimiento owns carries its
// organization and a policy that lets in only its own; an App without the
// label is the platform's; a shared namespace is left alone.
func TestOrgBoundaryOn(t *testing.T) {
	opt := render.DefaultOptions()
	opt.OrgNetwork = render.OrgNetwork{Enabled: true, Namespaces: []string{"rendimiento-system", "ingress-nginx"}}
	c, _ := setupWith(t, opt)
	ctx := context.Background()

	if err := c.Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "frontera-compartido", Labels: map[string]string{"equipo": "otro"}}}); err != nil {
		t.Fatal(err)
	}
	shared := fronteraApp("frontera-compartido", "acme", false)
	shared.Spec.Adopt, shared.Spec.SharedNamespace = true, true
	for _, a := range []*v1alpha1.App{fronteraApp("acme-frontera", "acme", true), fronteraApp("frontera-plataforma", "", false), shared} {
		if err := c.Create(ctx, a); err != nil {
			t.Fatal(err)
		}
	}

	var pol networkingv1.NetworkPolicy
	eventually(t, "the organization's policy", func() bool {
		return c.Get(ctx, client.ObjectKey{Namespace: "acme-frontera", Name: render.NetworkPolicyName}, &pol) == nil
	})
	var ns corev1.Namespace
	_ = c.Get(ctx, client.ObjectKey{Name: "acme-frontera"}, &ns)
	if ns.Labels[render.LabelOrg] != "acme" {
		t.Errorf("namespace labels = %v", ns.Labels)
	}
	if len(pol.OwnerReferences) != 1 || len(pol.Spec.Ingress) != 1 || len(pol.Spec.Ingress[0].From) != 4 ||
		pol.Spec.Ingress[0].From[1].NamespaceSelector.MatchLabels[render.LabelOrg] != "acme" {
		t.Fatalf("policy = %+v", pol)
	}
	var lan networkingv1.NetworkPolicy
	if err := c.Get(ctx, client.ObjectKey{Namespace: "acme-frontera", Name: "web-lan"}, &lan); err != nil || len(lan.Spec.Ingress[0].From) != 0 {
		t.Fatalf("the LAN service's port is open to all: %v %+v", err, lan.Spec)
	}

	eventually(t, "the platform's app's policy", func() bool {
		return c.Get(ctx, client.ObjectKey{Namespace: "frontera-plataforma", Name: render.NetworkPolicyName}, &pol) == nil
	})
	_ = c.Get(ctx, client.ObjectKey{Name: "frontera-plataforma"}, &ns)
	if ns.Labels[render.LabelOrg] != render.PlatformOrg || pol.Spec.Ingress[0].From[1].NamespaceSelector.MatchLabels[render.LabelOrg] != render.PlatformOrg {
		t.Errorf("an App without the label is the platform's: namespace %v, policy %+v", ns.Labels, pol.Spec.Ingress[0].From)
	}

	eventually(t, "the shared namespace's service", func() bool {
		return c.Get(ctx, client.ObjectKey{Namespace: "frontera-compartido", Name: "web"}, &appsv1.Deployment{}) == nil
	})
	var pols networkingv1.NetworkPolicyList
	if err := c.List(ctx, &pols, client.InNamespace("frontera-compartido")); err != nil || len(pols.Items) != 0 {
		t.Errorf("a shared namespace gets no policy: %v %d", err, len(pols.Items))
	}
	_ = c.Get(ctx, client.ObjectKey{Name: "frontera-compartido"}, &ns)
	if _, ok := ns.Labels[render.LabelOrg]; ok || ns.Labels["equipo"] != "otro" {
		t.Errorf("a shared namespace is not labeled: %v", ns.Labels)
	}

	// The LAN service goes: so does its policy; the boundary stays.
	updateApp(t, c, "acme-frontera", func(a *v1alpha1.App) { a.Spec.Services[0].LAN = nil })
	eventually(t, "LAN policy removed", func() bool {
		return apierrors.IsNotFound(c.Get(ctx, client.ObjectKey{Namespace: "acme-frontera", Name: "web-lan"}, &lan))
	})
	if err := c.Get(ctx, client.ObjectKey{Namespace: "acme-frontera", Name: render.NetworkPolicyName}, &pol); err != nil {
		t.Fatalf("the boundary stays: %v", err)
	}
}

// With the boundary off (the default), the namespace still carries its
// organization, and a policy rendimiento made before is removed; anyone
// else's stays.
func TestOrgBoundaryOff(t *testing.T) {
	c, _ := setup(t)
	ctx := context.Background()
	if err := c.Create(ctx, fronteraApp("acme-sinfrontera", "acme", false)); err != nil {
		t.Fatal(err)
	}
	eventually(t, "deployment", func() bool {
		return c.Get(ctx, client.ObjectKey{Namespace: "acme-sinfrontera", Name: "web"}, &appsv1.Deployment{}) == nil
	})
	var ns corev1.Namespace
	_ = c.Get(ctx, client.ObjectKey{Name: "acme-sinfrontera"}, &ns)
	if ns.Labels[render.LabelOrg] != "acme" {
		t.Errorf("namespace labels = %v", ns.Labels)
	}
	ingress := []networkingv1.NetworkPolicyIngressRule{{}}
	ours := &networkingv1.NetworkPolicy{ObjectMeta: metav1.ObjectMeta{Namespace: "acme-sinfrontera", Name: render.NetworkPolicyName,
		Labels: map[string]string{render.LabelApp: "acme-sinfrontera", render.LabelManagedBy: render.ManagedBy}},
		Spec: networkingv1.NetworkPolicySpec{Ingress: ingress}}
	theirs := &networkingv1.NetworkPolicy{ObjectMeta: metav1.ObjectMeta{Namespace: "acme-sinfrontera", Name: "de-alguien"},
		Spec: networkingv1.NetworkPolicySpec{Ingress: ingress}}
	for _, p := range []*networkingv1.NetworkPolicy{ours, theirs} {
		if err := c.Create(ctx, p); err != nil {
			t.Fatal(err)
		}
	}
	updateApp(t, c, "acme-sinfrontera", func(a *v1alpha1.App) { a.Spec.Services[0].Replicas = 2 })
	eventually(t, "rendimiento's policy removed", func() bool {
		return apierrors.IsNotFound(c.Get(ctx, client.ObjectKeyFromObject(ours), &networkingv1.NetworkPolicy{}))
	})
	if err := c.Get(ctx, client.ObjectKeyFromObject(theirs), &networkingv1.NetworkPolicy{}); err != nil {
		t.Errorf("someone else's policy stays: %v", err)
	}
}
