package platform

import (
	"testing"

	gh "github.com/rendimiento-ai/rendimiento/internal/github"
)

// A "Validation Failed" from GitHub says what it was about.
func TestGithubSays(t *testing.T) {
	err := &gh.APIError{Status: 422, Body: `{"message":"Validation Failed","errors":[{"resource":"PullRequest","code":"custom","message":"No commits between main and vistazo"}]}`}
	if got := githubSays(err); got != "Validation Failed: No commits between main and vistazo" {
		t.Fatalf("githubSays = %q", got)
	}
	field := &gh.APIError{Status: 422, Body: `{"message":"Validation Failed","errors":[{"resource":"PullRequest","code":"invalid","field":"head"}]}`}
	if got := githubSays(field); got != "Validation Failed: head invalid" {
		t.Fatalf("githubSays = %q", got)
	}
	if got := githubSays(&gh.APIError{Status: 404, Body: `{"message":"Not Found."}`}); got != "Not Found" {
		t.Fatalf("githubSays = %q", got)
	}
}
