package agentes

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/rendimiento-ai/rendimiento/internal/ia"
	"github.com/rendimiento-ai/rendimiento/internal/store"
)

// Why the coding agent gives an encargo back, recorded with its release
// (store.Cause*) for the AI page's «How the coding agent is doing»: the
// model says it when it calls give_up, and the places where it stops by
// itself (its brake, a person, a restart, an error) set their own.

// giveUpCause is give_up's cause: the ones the model may choose (a person's
// stop and a restart are never its own).
var giveUpCause = map[string]any{
	"type": "string",
	"enum": []string{store.CauseBudget, store.CauseNeedsDecision, store.CauseNoTools, store.CauseCITimeout,
		store.CauseAIError, store.CauseForgeError, store.CauseConflict, store.CauseOther},
	"description": "the kind of reason: budget (it would cost too much), needs_decision (a person must answer or choose), no_tools (you lack a tool you need), ci_timeout (the run did not end), ai_error, forge_error (the repository's host answered with an error), conflict (you could not merge the default branch), other",
}

// causeOfGiveUp is the cause in a give_up call ("" when it gave none, or
// one it may not choose: the note is classified when read).
func causeOfGiveUp(input json.RawMessage) string {
	var in struct{ Cause string }
	_ = json.Unmarshal(input, &in)
	switch c := store.KnownCause(in.Cause); c {
	case store.CauseStopped, store.CauseRestart:
		return ""
	default:
		return c
	}
}

// errCause is the cause of an error that ended its work: before it called
// the model the forja's (preparing the branch and reading its files), then
// the model's (it failed, refused or stopped using its tools), except
// running out of turns or time.
func (j *job) errCause(err error) string {
	switch {
	case errors.Is(err, ia.ErrNoModel), errors.Is(err, ia.ErrNoToolModel):
		return store.CauseAIError
	case errors.Is(err, context.DeadlineExceeded), len(j.costs) >= maxTurns:
		return store.CauseOther
	case len(j.costs) == 0:
		if strings.HasPrefix(err.Error(), "no app ") {
			return store.CauseOther
		}
		return store.CauseForgeError
	}
	return store.CauseAIError
}
