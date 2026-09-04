package copilot

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSetPATAndLogout(t *testing.T) {
	store := &TokenStore{Dir: t.TempDir()}
	a := NewAuthenticator(http.DefaultClient, store)

	if a.HasGitHubToken() {
		t.Fatal("should not have token initially")
	}
	if err := a.SetPAT("pat-123"); err != nil {
		t.Fatalf("SetPAT failed: %v", err)
	}
	if !a.HasGitHubToken() {
		t.Fatal("should have token after SetPAT")
	}
	// Token should be persisted.
	if got, _ := store.Load(); got != "pat-123" {
		t.Fatalf("expected persisted PAT, got %q", got)
	}
	if err := a.SetPAT("  "); err == nil {
		t.Fatal("empty PAT should error")
	}
	if err := a.Logout(); err != nil {
		t.Fatalf("Logout failed: %v", err)
	}
	if a.HasGitHubToken() {
		t.Fatal("should not have token after logout")
	}
}

func TestCopilotTokenNoGitHubToken(t *testing.T) {
	a := NewAuthenticator(http.DefaultClient, nil)
	if _, err := a.CopilotToken(context.Background()); err != ErrNoToken {
		t.Fatalf("expected ErrNoToken, got %v", err)
	}
}

func TestCopilotTokenExchangeAndCache(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"token":      "copilot-abc",
			"expires_at": timeFarFuture(),
		})
	}))
	defer srv.Close()

	oldURL := copilotTokenURL
	copilotTokenURL = srv.URL
	defer func() { copilotTokenURL = oldURL }()

	a := NewAuthenticator(srv.Client(), nil)
	_ = a.SetPAT("gh-token")

	tok, err := a.CopilotToken(context.Background())
	if err != nil {
		t.Fatalf("CopilotToken failed: %v", err)
	}
	if tok != "copilot-abc" {
		t.Fatalf("unexpected token: %q", tok)
	}
	// Second call should use cache, not hit the server again.
	if _, err := a.CopilotToken(context.Background()); err != nil {
		t.Fatalf("second CopilotToken failed: %v", err)
	}
	if calls != 1 {
		t.Fatalf("expected token to be cached (1 call), got %d", calls)
	}
}

func TestCopilotTokenNoSubscription(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()

	oldURL := copilotTokenURL
	copilotTokenURL = srv.URL
	defer func() { copilotTokenURL = oldURL }()

	a := NewAuthenticator(srv.Client(), nil)
	_ = a.SetPAT("gh-token")

	if _, err := a.CopilotToken(context.Background()); err != ErrNoCopilotSubscription {
		t.Fatalf("expected ErrNoCopilotSubscription, got %v", err)
	}
}

func TestPollForTokenPending(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "authorization_pending"})
	}))
	defer srv.Close()

	oldURL := accessTokenURL
	accessTokenURL = srv.URL
	defer func() { accessTokenURL = oldURL }()

	a := NewAuthenticator(srv.Client(), nil)
	if err := a.PollForToken(context.Background(), "dc"); err != ErrAuthPending {
		t.Fatalf("expected ErrAuthPending, got %v", err)
	}
}

func TestPollForTokenSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"access_token": "gh-xyz"})
	}))
	defer srv.Close()

	oldURL := accessTokenURL
	accessTokenURL = srv.URL
	defer func() { accessTokenURL = oldURL }()

	store := &TokenStore{Dir: t.TempDir()}
	a := NewAuthenticator(srv.Client(), store)
	if err := a.PollForToken(context.Background(), "dc"); err != nil {
		t.Fatalf("PollForToken failed: %v", err)
	}
	if !a.HasGitHubToken() {
		t.Fatal("expected token after successful poll")
	}
	if got, _ := store.Load(); got != "gh-xyz" {
		t.Fatalf("expected persisted github token, got %q", got)
	}
}

func timeFarFuture() int64 {
	return 1<<62 - 1
}
