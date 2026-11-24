package store

import (
	"context"
	"testing"
)

// The coding agent may run commands unless the organization says no, and
// what it ran goes to the encargo's history.
func TestCodeRun(t *testing.T) {
	s := open(t)
	ctx := context.Background()
	org, err := s.CreateOrg(ctx, "con-terminal", "Con terminal", "ana")
	if err != nil {
		t.Fatal(err)
	}
	if a, _ := s.AISettingsOf(ctx, org.ID); !a.CodeRun {
		t.Fatal("off by default")
	}
	if err := s.SetCodeConcurrency(ctx, org.ID, 2, "ana"); err != nil {
		t.Fatal(err)
	}
	if a, _ := s.AISettingsOf(ctx, org.ID); !a.CodeRun {
		t.Fatal("a row made by another setting turned it off")
	}
	if err := s.SetCodeRun(ctx, org.ID, false, "ana"); err != nil {
		t.Fatal(err)
	}
	if a, _ := s.AISettingsOf(ctx, org.ID); a.CodeRun || a.CodeConcurrency != 2 {
		t.Fatalf("after turning it off: %+v", a)
	}
	e, err := s.CreateEncargo(ctx, Encargo{Title: "Correr las pruebas", App: "terminal-app", CreatedBy: "ana"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.NoteEncargoRun(ctx, e.ID, "agente de código", "npm test: exit 0"); err != nil {
		t.Fatal(err)
	}
	got, err := s.Encargo(ctx, e.ID)
	if err != nil {
		t.Fatal(err)
	}
	last := got.Log[len(got.Log)-1]
	if last.What != "ran" || last.Note != "npm test: exit 0" {
		t.Fatalf("log %+v", got.Log)
	}
}
