package components

import (
	"context"
	"net/http"
	"path/filepath"
	"time"

	"github.com/jorgerojas26/lazysql/app"
	"github.com/jorgerojas26/lazysql/internal/copilot"
	"github.com/jorgerojas26/lazysql/models"
)

// copilotService wraps the Copilot authenticator and chat client and exposes the
// current configuration. A single instance is shared across the app.
type copilotService struct {
	auth   *copilot.Authenticator
	client *copilot.Client
}

var sharedCopilotService *copilotService

// getCopilotService lazily builds and returns the shared Copilot service using
// the current application configuration.
func getCopilotService() *copilotService {
	if sharedCopilotService != nil {
		return sharedCopilotService
	}

	cfg := copilotConfig()

	var store *copilot.TokenStore
	if dir, err := app.GetConfigPath(); err == nil {
		store = &copilot.TokenStore{Dir: filepath.Join(dir, "lazysql")}
	}

	httpClient := &http.Client{Timeout: 120 * time.Second}
	auth := copilot.NewAuthenticator(httpClient, store)
	client := copilot.NewClient(httpClient, auth, cfg.Model)

	sharedCopilotService = &copilotService{auth: auth, client: client}
	return sharedCopilotService
}

// copilotConfig returns the current (normalized) Copilot configuration.
func copilotConfig() models.CopilotConfig {
	appCfg := app.App.Config()
	if appCfg == nil {
		return copilot.DefaultConfig()
	}
	return copilot.Normalize(appCfg.Copilot)
}

// Ask sends the conversation to Copilot and returns the assistant reply. The
// safety system prompt is always prepended.
func (s *copilotService) Ask(ctx context.Context, messages []copilot.Message) (string, error) {
	s.client.SetModel(copilotConfig().Model)
	return s.client.Complete(ctx, copilot.WithSystemPrompt(messages))
}

// IsAuthenticated reports whether a GitHub token is available.
func (s *copilotService) IsAuthenticated() bool {
	return s.auth.HasGitHubToken()
}

// RequestDeviceCode starts the GitHub device authorization flow.
func (s *copilotService) RequestDeviceCode(ctx context.Context) (*copilot.DeviceCode, error) {
	return s.auth.RequestDeviceCode(ctx)
}

// WaitForDeviceToken polls until the user authorizes the device code.
func (s *copilotService) WaitForDeviceToken(ctx context.Context, dc *copilot.DeviceCode) error {
	return s.auth.WaitForToken(ctx, dc)
}

// SavePAT stores a user-supplied Personal Access Token.
func (s *copilotService) SavePAT(token string) error {
	return s.auth.SetPAT(token)
}

// Logout clears and removes the stored token.
func (s *copilotService) Logout() error {
	return s.auth.Logout()
}
