package platform

import (
	"testing"
	"time"

	"github.com/rendimiento-ai/rendimiento/internal/i18n"
	"github.com/rendimiento-ai/rendimiento/internal/store"
)

// Messages about a change outside merging (previews, the board) say it in
// the forja's words, and the Spanish catalog knows each.
func TestInForjaWordsOutsideMerging(t *testing.T) {
	gl, bb := &store.App{Forja: "gitlab"}, &store.App{Forja: "bitbucket"}
	run := &store.Run{ID: 3, SHA: "0123456789abcdef"}
	comment := previewComment(7, "https://a-pr-7.example.com", run, "https://rendimiento.example.com", "a", time.Date(2026, 11, 30, 0, 0, 0, 0, time.UTC))
	for _, tc := range []struct {
		app     *store.App
		msg, en string
		spanish bool
	}{
		{gl, i18n.M("**Preview of #%d:** deleted, %s.", 7, i18n.M("the pull request was closed")), "**Preview of !7:** deleted, the merge request was closed.", true},
		{gl, i18n.M("to make room for a newer pull request's copy"), "to make room for a newer merge request's copy", true},
		{gl, i18n.M("GitHub did not close the pull request: %s", "no"), "GitLab did not close the merge request: no", true},
		{gl, i18n.M("GitHub did not delete the branch: %s", "no"), "GitLab did not delete the branch: no", true},
		{bb, i18n.M("GitHub did not close the pull request: %s", "no"), "Bitbucket did not close the pull request: no", true},
		{bb, i18n.M("GitHub did not delete the branch: %s", "no"), "Bitbucket did not delete the branch: no", true},
		{gl, i18n.M("This pull request's copy. It is deleted when the pull request is closed, or after %d days without a push.", 5),
			"This merge request's copy. It is deleted when the merge request is closed, or after 5 days without a push.", true},
		{&store.App{}, i18n.M("the pull request was closed"), "the pull request was closed", true},
	} {
		got := inForjaWords(tc.app, tc.msg)
		if got != tc.en {
			t.Errorf("%q: %q, want %q", tc.msg, got, tc.en)
		}
		if es := i18n.Translate(i18n.Spanish, got); tc.spanish && es == got {
			t.Errorf("no Spanish for %q", got)
		}
	}
	got := inForjaWords(gl, comment)
	if want := "**Preview of !7:** https://a-pr-7.example.com\n\nIt shows commit `0123456` (run [#3](https://rendimiento.example.com/apps/a/runs/3)). It is deleted when this merge request is closed or merged, or on 2026-11-30 if nothing is pushed before."; got != want {
		t.Errorf("comment: %q", got)
	}
	if i18n.Translate(i18n.Spanish, got) == got {
		t.Error("no Spanish for the GitLab preview comment")
	}
}
