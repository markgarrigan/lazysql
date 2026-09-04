package copilot

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestWithSystemPrompt(t *testing.T) {
	msgs := WithSystemPrompt([]Message{{Role: RoleUser, Content: "hi"}})
	if len(msgs) != 2 {
		t.Fatalf("expected system prompt prepended, got %d messages", len(msgs))
	}
	if msgs[0].Role != RoleSystem {
		t.Fatalf("expected first message to be system, got %q", msgs[0].Role)
	}
	// Should not double-prepend.
	again := WithSystemPrompt(msgs)
	if len(again) != 2 {
		t.Fatalf("system prompt should not be duplicated, got %d", len(again))
	}
}

func TestClientComplete(t *testing.T) {
	// Token exchange server.
	tokenSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"token": "cop", "expires_at": timeFarFuture()})
	}))
	defer tokenSrv.Close()
	oldTokenURL := copilotTokenURL
	copilotTokenURL = tokenSrv.URL
	defer func() { copilotTokenURL = oldTokenURL }()

	// Chat server.
	chatSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); !strings.HasPrefix(got, "Bearer ") {
			t.Errorf("expected bearer token header, got %q", got)
		}
		if r.Header.Get("Copilot-Integration-Id") == "" {
			t.Error("expected Copilot-Integration-Id header")
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{
				{"message": map[string]string{"role": "assistant", "content": "```sql\nSELECT 1;\n```"}},
			},
		})
	}))
	defer chatSrv.Close()
	oldChatURL := chatCompletionsURL
	chatCompletionsURL = chatSrv.URL
	defer func() { chatCompletionsURL = oldChatURL }()

	a := NewAuthenticator(chatSrv.Client(), nil)
	_ = a.SetPAT("gh")
	c := NewClient(chatSrv.Client(), a, "gpt-4o")

	reply, err := c.Complete(context.Background(), WithSystemPrompt([]Message{{Role: RoleUser, Content: "give me sql"}}))
	if err != nil {
		t.Fatalf("Complete failed: %v", err)
	}
	if !strings.Contains(reply, "SELECT 1;") {
		t.Fatalf("unexpected reply: %q", reply)
	}
}

func TestClientCompleteNoAuth(t *testing.T) {
	c := NewClient(http.DefaultClient, nil, "")
	if _, err := c.Complete(context.Background(), nil); err != ErrNoToken {
		t.Fatalf("expected ErrNoToken, got %v", err)
	}
}
