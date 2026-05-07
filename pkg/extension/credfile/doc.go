// Package credfile implements a file-based extension.CredentialHandler
// that loads credentials from a TOML file (default: $HOME/.skytime-credentials).
//
// Schema (one [credentials.<id>] table per credential):
//
//	[credentials.github_token]
//	type  = "bearer"
//	token = "ghp_..."
//
//	[credentials.svc_account]
//	type     = "basic"
//	username = "..."
//	password = "..."
//
//	[credentials.partner_api]
//	type  = "apikey"
//	key   = "X-API-Key"   # the HEADER name
//	value = "..."         # the secret VALUE
//
// Each entry maps 1:1 to a sealed extension.Credential type:
//   - "bearer" → *extension.BearerCredential
//   - "basic"  → *extension.BasicCredential
//   - "apikey" → *extension.APIKeyCredential
//
// Security:
//   - The file MUST be `chmod 600`. World/group-readable files (mode & 0o044 != 0)
//     trigger a slog.Warn by default; WithStrictMode() refuses to load them.
//   - Secrets are wrapped in extension.Secret, which redacts in String()/MarshalJSON
//     and only releases the raw value via .Reveal().
//   - Unknown credential IDs wrap extension.ErrUnknownCredential so the activity
//     layer classifies the failure as NonRetryable.
//
// The file is loaded ONCE at construction. Restart the worker to pick up edits;
// hot-reload is intentionally deferred to v2.
package credfile
