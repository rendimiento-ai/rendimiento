package platform

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/rendimiento-ai/rendimiento/internal/events"
	. "github.com/rendimiento-ai/rendimiento/internal/i18n" //nolint:revive // M marks messages for people
	"github.com/rendimiento-ai/rendimiento/internal/pipeline"
	"github.com/rendimiento-ai/rendimiento/internal/store"
)

// MaxRetries is how often what failed in a run may be run again.
const MaxRetries = 2

// FlakyAfter is how many stumbles in store.StumbleWindow make a step flaky:
// it is then a problem, and the run page says so.
const FlakyAfter = 3

// NotRetryable says why a run cannot be retried.
type NotRetryable struct{ Why string }

func (e *NotRetryable) Error() string { return e.Why }

// RetryRun runs again what did not pass in a failed run, in the same run:
// what passed keeps its result and its image. A run of the default branch
// deploys when it passes, so only the branch's latest run may be retried
// (an older one would put back an older commit); who may ask is checked by
// the caller (administrators for the default branch).
func (p *Platform) RetryRun(ctx context.Context, id int64) (*store.Run, error) {
	run, err := p.Store.GetRun(ctx, id)
	if err != nil {
		return nil, err
	}
	if run.Deploy {
		app, err := p.Store.GetAppByID(ctx, run.AppID)
		if err != nil {
			return nil, err
		}
		if last, err := p.Store.LastRunOf(ctx, app.ID, app.DefaultBranch); err == nil && last.ID != run.ID {
			return nil, &NotRetryable{Why: M("run #%d of %s came after this one: retrying this one would deploy an older commit", last.ID, app.DefaultBranch)}
		}
	}
	run, err = p.Store.RetryRun(ctx, id, MaxRetries)
	switch {
	case errors.Is(err, store.ErrNotFailed):
		return nil, &NotRetryable{Why: M("only a run that failed can be retried")}
	case errors.Is(err, store.ErrRetriesSpent):
		return nil, &NotRetryable{Why: M("this run was already retried %d times: push a commit, or build it again", MaxRetries)}
	case err != nil:
		return nil, err
	}
	if app, err := p.Store.GetAppByID(ctx, run.AppID); err == nil {
		p.Hub.Publish(appTopic(app.Name), events.Event{Type: "run", Data: run})
	}
	return run, nil
}

// noteStumbles records the steps of a retried run that failed before and
// passed now (a stumble), and calls a step flaky when it stumbled
// FlakyAfter times in store.StumbleWindow.
func (p *Platform) noteStumbles(ctx context.Context, app *store.App, run *store.Run, ran []pipeline.Step, results map[string]pipeline.StepResult, tried map[string]int) {
	for _, st := range ran {
		if tried[st.ID] <= 1 || results[st.ID].Status != pipeline.StatusSucceeded {
			continue
		}
		n, err := p.Store.RecordStumble(ctx, app.ID, st.ID, run.ID)
		if err != nil {
			p.Log.Warn("could not record a stumble", "app", app.Name, "step", st.ID, "err", err)
			continue
		}
		if n < FlakyAfter {
			continue
		}
		if err := p.Store.RecordProblem(ctx, store.Problem{Fingerprint: fmt.Sprintf("flaky:%s:%s", app.Name, st.ID), Level: "warn", Component: "ci", App: app.Name,
			Message: M("step %s is flaky: it failed and then passed on the same commit %d times in 30 days", st.ID, n),
			Detail:  M("the last time in run #%d; retrying hides it, fixing it does not", run.ID), LastAt: time.Now()}); err != nil {
			p.Log.Warn("could not record a flaky step", "app", app.Name, "step", st.ID, "err", err)
		}
	}
}
