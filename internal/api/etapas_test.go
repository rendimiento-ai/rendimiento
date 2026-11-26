package api

import (
	"context"
	"errors"
	"testing"

	"github.com/rendimiento-ai/rendimiento/internal/store"
)

// reposEtapa answers a stage's questions: the files a release changed, its
// commit's subject and its change file.
type reposEtapa struct {
	files   []string
	subject string
	cambios map[string]string
	falla   bool
	pedidos int
}

func (r *reposEtapa) ChangedFiles(context.Context, int64, string, string, string) ([]string, bool, error) {
	r.pedidos++
	if r.falla {
		return nil, false, errors.New("GitHub did not answer")
	}
	return r.files, false, nil
}
func (r *reposEtapa) CommitSubject(context.Context, int64, string, string) (string, error) {
	return r.subject, nil
}
func (r *reposEtapa) FileAt(_ context.Context, _ int64, _, path, _ string) ([]byte, error) {
	if c, ok := r.cambios[path]; ok {
		return []byte(c), nil
	}
	return nil, store.ErrNotFound
}

// A release that touched web/ is a stage, named by its branch and its change
// file's line; one that did not is not; each commit is asked once; a
// repository that does not answer is asked again.
func TestEtapaDe(t *testing.T) {
	s := &Server{}
	app := &store.App{Repo: "o/rendimiento"}
	ctx := context.Background()
	r := &reposEtapa{files: []string{"web/src/pages/IA.tsx", "docs/content/guide/ai.md"}, subject: "Merge pull request #337 from rendimiento-ai/ia-en-pestanas",
		cambios: map[string]string{"cambios/ia-en-pestanas.md": "- **AI page:** it is in tabs now.\n"}}
	info, ok := s.etapaDe(ctx, r, app, "aaa", "etapa-1")
	if !ok || !info.interfaz || info.rama != "ia-en-pestanas" || info.pull != 337 || info.texto != "it is in tabs now." {
		t.Fatalf("a stage: %+v %v", info, ok)
	}
	if _, ok := s.etapaDe(ctx, r, app, "aaa", "etapa-1"); !ok || r.pedidos != 1 {
		t.Fatalf("asked %d times", r.pedidos)
	}
	r.files = []string{"internal/store/uso.go"}
	if info, _ := s.etapaDe(ctx, r, app, "bbb", "etapa-2"); info.interfaz {
		t.Fatal("a release without web/ is a stage")
	}
	r.falla = true
	if _, ok := s.etapaDe(ctx, r, app, "ccc", "etapa-3"); ok {
		t.Fatal("an unanswered repository gave a stage")
	}
	r.falla = false
	if _, ok := s.etapaDe(ctx, r, app, "ccc", "etapa-3"); !ok {
		t.Fatal("not asked again")
	}
}

// A side's measures: views a day, pages for each run opened, the time to data,
// and the section opened most with its share.
func TestMedirEtapa(t *testing.T) {
	u := store.ResumenUso{
		Paginas: []store.PaginaUso{{Ruta: "/apps/:name/runs/:id", Vistas: 40, Medidas: 40, MsTotal: 40_000}, {Ruta: "/", Vistas: 60, Medidas: 10, MsTotal: 5_000}},
		Acciones: []store.AccionUsada{{Ruta: "/apps/:name/:tab", Accion: "open_runs", Veces: 6}, {Ruta: "/apps/:name/:tab", Accion: "open_roads", Veces: 2},
			{Ruta: "/apps/:name/runs/:id", Accion: "select_step", Veces: 30}},
	}
	m := medirEtapa(u, 5)
	if m.VistasDia != 20 || m.PaginasPorCorrida != 2.5 || m.MsDatos != 900 || m.Seccion != "open_runs" || m.SeccionParte != 0.75 {
		t.Fatalf("measures: %+v", m)
	}
	if m := medirEtapa(store.ResumenUso{}, 7); m.VistasDia != 0 || m.PaginasPorCorrida != 0 || m.Seccion != "" {
		t.Fatalf("an empty side: %+v", m)
	}
}
