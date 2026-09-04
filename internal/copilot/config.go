// Package copilot provides GitHub Copilot Chat integration for LazySQL:
// authentication (device flow / PAT), a chat client, and helpers to build
// database context to send to the model.
//
// The Copilot Chat endpoints, OAuth client ID and required headers are not a
// formally documented public REST surface. They are isolated in this package so
// they can be updated in a single place and mocked in tests.
package copilot

import "github.com/jorgerojas26/lazysql/models"

const (
	// AuthMethodDevice uses the GitHub OAuth device authorization flow.
	AuthMethodDevice = "device"
	// AuthMethodPAT uses a user-supplied Personal Access Token.
	AuthMethodPAT = "pat"

	// DefaultModel is the default Copilot Chat model.
	DefaultModel = "gpt-4o"
	// DefaultMaxRows caps how many result rows may be sent as context.
	DefaultMaxRows = 50
)

// DefaultConfig returns the default Copilot configuration. Everything that could
// leak data or make network calls is disabled by default.
func DefaultConfig() models.CopilotConfig {
	return models.CopilotConfig{
		Enabled:      false,
		AuthMethod:   AuthMethodDevice,
		Model:        DefaultModel,
		AllowRowData: false,
		MaxRows:      DefaultMaxRows,
	}
}

// Normalize fills in sensible values for any zero/invalid fields so callers can
// rely on the config after loading it from disk.
func Normalize(c models.CopilotConfig) models.CopilotConfig {
	if c.AuthMethod != AuthMethodDevice && c.AuthMethod != AuthMethodPAT {
		c.AuthMethod = AuthMethodDevice
	}
	if c.Model == "" {
		c.Model = DefaultModel
	}
	if c.MaxRows <= 0 {
		c.MaxRows = DefaultMaxRows
	}
	return c
}
