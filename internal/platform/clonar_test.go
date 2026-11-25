package platform

import (
	"context"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"

	"github.com/rendimiento-ai/rendimiento/internal/forja"
	"github.com/rendimiento-ai/rendimiento/internal/pipeline"
	"github.com/rendimiento-ai/rendimiento/internal/store"
)

// gitlabClone is a GitLab connection: its own token, its own address.
type gitlabClone struct{ forja.Repos }

func (gitlabClone) CloneToken(_ context.Context, installation int64) (string, error) {
	if installation != 9 {
		return "", nil
	}
	return "glpat-example", nil
}

func (gitlabClone) CloneURL(_ context.Context, _ int64, repo string) (string, error) {
	return "https://" + repo + ".git", nil
}

// A pod outside a run (the coding agent's sandbox, a declared fix) clones
// as a run's steps do (Ruta 178: a private repository refused the
// sandbox's anonymous clone): ForClone carries the app's installation, so
// the executor's token is the installation's, and gives its forja's
// address. The sandbox's clone secret holds that token, and its clone
// container fetches from that address at the commit it is given.
func TestForClone(t *testing.T) {
	p := &Platform{Forja: &fakeGitHub{}, Forjas: map[string]forja.Repos{"gitlab": gitlabClone{}}}
	ctx := context.Background()
	if tok, _ := p.CloneToken(ctx, "o/tienda"); tok != "" {
		t.Fatalf("a token without an installation: %q", tok)
	}
	gh := &store.App{Name: "tienda", Repo: "o/tienda", InstallationID: 7}
	cctx, repo := p.ForClone(ctx, gh)
	if tok, _ := p.CloneToken(cctx, repo); tok != "ghs_x" || repo != "o/tienda" {
		t.Fatalf("GitHub: token %q, repo %q", tok, repo)
	}

	gl := &store.App{Name: "catalogo", Repo: "gitlab.example.com/tienda/catalogo", Forja: "gitlab", InstallationID: 9}
	cctx, repo = p.ForClone(ctx, gl)
	if repo != "https://gitlab.example.com/tienda/catalogo.git" {
		t.Fatalf("GitLab's address %q", repo)
	}
	cs := fake.NewSimpleClientset()
	var tokens []string
	var pods []*corev1.Pod
	cs.PrependReactor("create", "secrets", func(a k8stesting.Action) (bool, runtime.Object, error) {
		tokens = append(tokens, string(a.(k8stesting.CreateAction).GetObject().(*corev1.Secret).Data["token"]))
		return false, nil, nil
	})
	cs.PrependReactor("create", "pods", func(a k8stesting.Action) (bool, runtime.Object, error) {
		pods = append(pods, a.(k8stesting.CreateAction).GetObject().(*corev1.Pod))
		return true, nil, apierrors.NewBadRequest("the test stops at the pod")
	})
	sandboxes := &pipeline.Sandboxes{K: &pipeline.KubeExecutor{Client: cs, Namespace: "rendimiento-builds", Token: p.CloneToken}}
	_, err := sandboxes.Run(cctx, pipeline.SandboxRun{Key: "encargo-48", Src: pipeline.Source{Repo: repo, Branch: "encargo-48", SHA: "base123"},
		Image: "node:22-bookworm-slim", Command: "true"})
	if err == nil || !strings.Contains(err.Error(), "the test stops at the pod") {
		t.Fatalf("run: %v", err)
	}
	if len(tokens) != 1 || tokens[0] != "glpat-example" {
		t.Fatal("the sandbox's clone secret lacks the installation's token")
	}
	if len(pods) != 1 {
		t.Fatalf("%d pods", len(pods))
	}
	env := map[string]string{}
	for _, e := range pods[0].Spec.InitContainers[0].Env {
		env[e.Name] = e.Value
	}
	if env["CLONE_URL"] != repo || env["REF"] != "base123" {
		t.Fatalf("the clone fetches %q at %q", env["CLONE_URL"], env["REF"])
	}
	for _, c := range pods[0].Spec.Containers {
		for _, e := range c.Env {
			if e.ValueFrom != nil || strings.Contains(e.Value, "glpat-example") {
				t.Fatalf("the token reached %s", c.Name)
			}
		}
	}
}
