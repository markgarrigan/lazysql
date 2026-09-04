package copilot

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Endpoints and client ID are package variables so tests can override them.
// These mirror the values used by editor Copilot integrations. They are not a
// documented public REST surface and may change upstream.
var (
	// GitHubClientID is the OAuth client ID used for the device flow.
	GitHubClientID = "Iv1.b507a08c87ecfe98"

	deviceCodeURL   = "https://github.com/login/device/code"
	accessTokenURL  = "https://github.com/login/oauth/access_token"
	copilotTokenURL = "https://api.github.com/copilot_internal/v2/token"
)

// Common auth errors surfaced to the UI so it can prompt re-auth or explain the
// missing subscription.
var (
	// ErrNoToken indicates that no GitHub token is available yet.
	ErrNoToken = errors.New("copilot: no GitHub token available; please log in")
	// ErrNoCopilotSubscription indicates the account lacks Copilot access.
	ErrNoCopilotSubscription = errors.New("copilot: account does not have an active GitHub Copilot subscription")
	// ErrAuthPending indicates the user has not yet authorized the device code.
	ErrAuthPending = errors.New("copilot: authorization pending")
	// ErrSlowDown indicates the device-flow poll interval should increase.
	ErrSlowDown = errors.New("copilot: slow_down")
)

// DeviceCode holds the response of a device authorization request.
type DeviceCode struct {
	DeviceCode      string `json:"device_code"`
	UserCode        string `json:"user_code"`
	VerificationURI string `json:"verification_uri"`
	ExpiresIn       int    `json:"expires_in"`
	Interval        int    `json:"interval"`
}

// copilotToken is the short-lived token returned by the token exchange.
type copilotToken struct {
	Token     string `json:"token"`
	ExpiresAt int64  `json:"expires_at"`
}

// Authenticator manages GitHub Copilot authentication: it holds a long-lived
// GitHub token (from the device flow or a PAT) and exchanges it for a
// short-lived Copilot token, refreshing on expiry.
type Authenticator struct {
	httpClient *http.Client
	store      *TokenStore

	mu          sync.Mutex
	githubToken string
	copilot     copilotToken
}

// NewAuthenticator creates an Authenticator. httpClient may be nil (defaults to
// a client with a sane timeout). store may be nil to disable persistence.
func NewAuthenticator(httpClient *http.Client, store *TokenStore) *Authenticator {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	a := &Authenticator{httpClient: httpClient, store: store}
	if store != nil {
		if tok, err := store.Load(); err == nil {
			a.githubToken = tok
		}
	}
	return a
}

// HasGitHubToken reports whether a long-lived GitHub token is loaded.
func (a *Authenticator) HasGitHubToken() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.githubToken != ""
}

// SetPAT stores a user-supplied Personal Access Token as the GitHub token and
// persists it (if a store is configured).
func (a *Authenticator) SetPAT(token string) error {
	token = strings.TrimSpace(token)
	if token == "" {
		return errors.New("copilot: empty token")
	}
	a.mu.Lock()
	a.githubToken = token
	a.copilot = copilotToken{} // invalidate any cached Copilot token
	a.mu.Unlock()
	if a.store != nil {
		return a.store.Save(token)
	}
	return nil
}

// Logout clears the in-memory tokens and removes the persisted token.
func (a *Authenticator) Logout() error {
	a.mu.Lock()
	a.githubToken = ""
	a.copilot = copilotToken{}
	a.mu.Unlock()
	if a.store != nil {
		return a.store.Delete()
	}
	return nil
}

// RequestDeviceCode starts the device authorization flow.
func (a *Authenticator) RequestDeviceCode(ctx context.Context) (*DeviceCode, error) {
	form := url.Values{}
	form.Set("client_id", GitHubClientID)
	form.Set("scope", "read:user")

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, deviceCodeURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("copilot: device code request failed: %s", resp.Status)
	}

	var dc DeviceCode
	if err := json.NewDecoder(resp.Body).Decode(&dc); err != nil {
		return nil, err
	}
	if dc.Interval <= 0 {
		dc.Interval = 5
	}
	return &dc, nil
}

// PollForToken polls the access token endpoint once. It returns ErrAuthPending
// or ErrSlowDown while waiting for the user to authorize. On success the GitHub
// token is stored and persisted.
func (a *Authenticator) PollForToken(ctx context.Context, deviceCode string) error {
	form := url.Values{}
	form.Set("client_id", GitHubClientID)
	form.Set("device_code", deviceCode)
	form.Set("grant_type", "urn:ietf:params:oauth:grant-type:device_code")

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, accessTokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	var body struct {
		AccessToken string `json:"access_token"`
		Error       string `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return err
	}

	if body.AccessToken != "" {
		a.mu.Lock()
		a.githubToken = body.AccessToken
		a.copilot = copilotToken{}
		a.mu.Unlock()
		if a.store != nil {
			return a.store.Save(body.AccessToken)
		}
		return nil
	}

	switch body.Error {
	case "authorization_pending":
		return ErrAuthPending
	case "slow_down":
		return ErrSlowDown
	case "":
		return errors.New("copilot: empty access token response")
	default:
		return fmt.Errorf("copilot: device flow error: %s", body.Error)
	}
}

// WaitForToken repeatedly polls until the user authorizes, the device code
// expires, or the context is cancelled.
func (a *Authenticator) WaitForToken(ctx context.Context, dc *DeviceCode) error {
	interval := time.Duration(dc.Interval) * time.Second
	if interval <= 0 {
		interval = 5 * time.Second
	}
	deadline := time.Now().Add(time.Duration(dc.ExpiresIn) * time.Second)

	for {
		if !deadline.IsZero() && time.Now().After(deadline) {
			return errors.New("copilot: device code expired; please try again")
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(interval):
		}

		err := a.PollForToken(ctx, dc.DeviceCode)
		switch {
		case err == nil:
			return nil
		case errors.Is(err, ErrAuthPending):
			// keep waiting
		case errors.Is(err, ErrSlowDown):
			interval += 5 * time.Second
		default:
			return err
		}
	}
}

// CopilotToken returns a valid short-lived Copilot token, exchanging or
// refreshing it as needed.
func (a *Authenticator) CopilotToken(ctx context.Context) (string, error) {
	a.mu.Lock()
	githubToken := a.githubToken
	if githubToken == "" {
		a.mu.Unlock()
		return "", ErrNoToken
	}
	// Reuse cached token if it is still valid (with a small safety margin).
	if a.copilot.Token != "" && time.Now().Unix() < a.copilot.ExpiresAt-60 {
		cached := a.copilot.Token
		a.mu.Unlock()
		return cached, nil
	}
	a.mu.Unlock()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, copilotTokenURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "token "+githubToken)
	req.Header.Set("Accept", "application/json")

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK:
		// handled below
	case http.StatusUnauthorized, http.StatusForbidden:
		return "", ErrNoCopilotSubscription
	default:
		return "", fmt.Errorf("copilot: token exchange failed: %s", resp.Status)
	}

	var tok copilotToken
	if err := json.NewDecoder(resp.Body).Decode(&tok); err != nil {
		return "", err
	}
	if tok.Token == "" {
		return "", ErrNoCopilotSubscription
	}
	a.mu.Lock()
	a.copilot = tok
	a.mu.Unlock()
	return tok.Token, nil
}

// TokenStore persists the long-lived GitHub token to a permission-restricted
// file (0o600) under a base directory. It never stores the short-lived Copilot
// token.
type TokenStore struct {
	// Dir is the base directory (typically the app config dir).
	Dir string
	// FileName overrides the default token file name (used in tests).
	FileName string
}

const defaultTokenFileName = "copilot_token"

func (s *TokenStore) path() string {
	name := s.FileName
	if name == "" {
		name = defaultTokenFileName
	}
	return filepath.Join(s.Dir, name)
}

// Load returns the stored token. It supports "${env:VAR}" sourcing so the token
// can come from the environment instead of disk.
func (s *TokenStore) Load() (string, error) {
	data, err := os.ReadFile(s.path())
	if err != nil {
		return "", err
	}
	return ResolveTokenValue(strings.TrimSpace(string(data))), nil
}

// Save writes the token to disk with 0o600 permissions, creating the directory
// if needed.
func (s *TokenStore) Save(token string) error {
	if err := os.MkdirAll(s.Dir, 0o700); err != nil {
		return err
	}
	return os.WriteFile(s.path(), []byte(strings.TrimSpace(token)), 0o600)
}

// Delete removes the persisted token file. A missing file is not an error.
func (s *TokenStore) Delete() error {
	err := os.Remove(s.path())
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// ResolveTokenValue expands a "${env:VAR}" reference to the value of the
// environment variable VAR. Any other value is returned unchanged.
func ResolveTokenValue(v string) string {
	inner, ok := strings.CutPrefix(v, "${env:")
	if !ok {
		return v
	}
	name, ok := strings.CutSuffix(inner, "}")
	if !ok {
		return v
	}
	return os.Getenv(name)
}
