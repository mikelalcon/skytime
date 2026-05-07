package credfile

import (
	"fmt"

	"github.com/mikelalcon/skytime/pkg/extension"
)

// fileShape is the top-level TOML structure: a single `credentials`
// table whose keys are the credential IDs and whose values are
// type-tagged credentialEntry rows.
type fileShape struct {
	Credentials map[string]credentialEntry `toml:"credentials"`
}

// credentialEntry is the TOML row for one credential. The `type` tag
// selects which other fields are required:
//
//   - "bearer":  Token is required.
//   - "basic":   Username + Password are required.
//   - "apikey":  Key (header NAME) + Value (secret VALUE) are required.
//
// Mapping to Go types is in buildCredentials below.
type credentialEntry struct {
	Type     string `toml:"type"`
	Token    string `toml:"token,omitempty"`    // bearer
	Username string `toml:"username,omitempty"` // basic
	Password string `toml:"password,omitempty"` // basic
	Key      string `toml:"key,omitempty"`      // apikey: header NAME (e.g. "X-API-Key")
	Value    string `toml:"value,omitempty"`    // apikey: header VALUE (the secret)
}

// buildCredentials maps the parsed fileShape into a registry of sealed
// extension.Credential values keyed by ID. Per-entry validation errors
// are returned with the credential ID quoted so users can locate them
// in a multi-entry file. The TOML file path is appended by the caller
// in resolver.go (we return the bare error here so unit tests can
// table-drive without a path string).
func buildCredentials(raw fileShape) (map[string]extension.Credential, error) {
	out := make(map[string]extension.Credential, len(raw.Credentials))
	for id, e := range raw.Credentials {
		switch e.Type {
		case "bearer":
			if e.Token == "" {
				return nil, fmt.Errorf("credential %q (bearer): token is required", id)
			}
			out[id] = &extension.BearerCredential{
				ID_:   id,
				Token: extension.NewSecret(e.Token),
			}
		case "basic":
			if e.Username == "" || e.Password == "" {
				return nil, fmt.Errorf("credential %q (basic): username and password are required", id)
			}
			out[id] = &extension.BasicCredential{
				ID_:      id,
				User:     e.Username,
				Password: extension.NewSecret(e.Password),
			}
		case "apikey":
			if e.Key == "" || e.Value == "" {
				return nil, fmt.Errorf("credential %q (apikey): key (header name) and value (secret) are required", id)
			}
			out[id] = &extension.APIKeyCredential{
				ID_:        id,
				HeaderName: e.Key,
				Key:        extension.NewSecret(e.Value),
			}
		case "":
			return nil, fmt.Errorf("credential %q: type is required (one of: bearer, basic, apikey)", id)
		default:
			return nil, fmt.Errorf("credential %q: unknown type %q (one of: bearer, basic, apikey)", id, e.Type)
		}
	}
	return out, nil
}
