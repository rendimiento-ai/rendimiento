package platform

import (
	"context"

	"github.com/rendimiento-ai/rendimiento/internal/pruebas"
	"github.com/rendimiento-ai/rendimiento/internal/spec"
	"github.com/rendimiento-ai/rendimiento/internal/store"

	. "github.com/rendimiento-ai/rendimiento/internal/i18n" //nolint:revive // M marks messages for people
)

// judgeTests reads the app's own tests from the release's tests task, moves
// each test's state, keeps the results with the release, and returns the
// failures that roll it back (blocking tests that failed) and what a person
// should know. Nothing when the app has no tests: block.
func (p *Platform) judgeTests(ctx context.Context, app *store.App, rel *store.Release, results []store.ReleaseTask) (failures, warnings []string) {
	if rel.Spec.Tests == nil {
		return nil, nil
	}
	var task *store.ReleaseTask
	for i := range results {
		if results[i].Name == spec.TestsTaskName {
			task = &results[i]
		}
	}
	if task == nil || task.Status != store.TaskSucceeded {
		return nil, nil // judgeTasks already says why it did not run
	}
	got, reported := pruebas.Parse(task.Log)
	if !reported {
		return nil, []string{M("the tests left no report: see the log of the %s task", spec.TestsTaskName)}
	}
	prev, err := p.Store.AppTests(ctx, app.ID)
	if err != nil {
		p.Log.Warn("tests: reading their states", "app", app.Name, "err", err)
		return nil, nil
	}
	next, failures, warnings := pruebas.Judge(prev, got, rel.Spec.Tests.BlockAfter())
	if err := p.Store.SaveTests(ctx, app.ID, rel.ID, got, next); err != nil {
		p.Log.Warn("tests: keeping their results", "app", app.Name, "release", rel.Number, "err", err)
	}
	return failures, warnings
}
