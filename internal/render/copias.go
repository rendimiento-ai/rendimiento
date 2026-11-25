package render

import "strings"

// A copy of an app (a pull request's, <app>-pr-<n>, or a short one for
// checks en seco, <app>-seco-<run>) reads its secrets by the same names as
// the app, in its own namespace. Their values are the app's preview
// secrets, never production's: for a secret X the copy gets the app's
// Secret X-preview, copied into its namespace as X (decision 181). Without
// X-preview the copy has no X.
const (
	// PreviewSecretSuffix ends the name of the app's preview variant of a
	// secret: X-preview, beside X in the app's namespace.
	PreviewSecretSuffix = "-preview"
	// LabelPreviewOf is on a copy's App object: the app it copies.
	LabelPreviewOf = "rendimiento.ai/preview-of"
	// LabelPreviewSecret marks a Secret in a copy's namespace made from
	// the app's preview variant: only those are updated or removed.
	LabelPreviewSecret = "rendimiento.ai/preview-secret"
	// AnnotationCopySecrets is on a copy's App object: every secret its
	// spec reads (services, tasks and tests), comma-separated, written by
	// the platform. The App object only holds the services, so the tasks'
	// and tests' names come from here.
	AnnotationCopySecrets = "rendimiento.ai/secrets"
	// AnnotationSecretsVersion is on a copy's pod templates: a digest of
	// which preview secrets it got and their versions, so an edit to one
	// rolls the copy's pods (environment variables are read at start).
	AnnotationSecretsVersion = "rendimiento.ai/preview-secrets"
)

// PreviewSecret is the name of the preview variant of secret name.
func PreviewSecret(name string) string { return name + PreviewSecretSuffix }

// IsPreviewSecret says whether name is a preview variant (X-preview), and
// of which secret.
func IsPreviewSecret(name string) (string, bool) {
	base, ok := strings.CutSuffix(name, PreviewSecretSuffix)
	return base, ok && base != ""
}
