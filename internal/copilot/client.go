package copilot

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"
)

// chatCompletionsURL is the Copilot Chat completions endpoint. It is a package
// variable so tests can override it.
var chatCompletionsURL = "https://api.githubcopilot.com/chat/completions"

// SystemPrompt encodes the safety policy the assistant must follow. It is sent
// as the first message in every conversation and mirrors the UI guardrails.
const SystemPrompt = `You are a SQL assistant embedded in LazySQL, a terminal database client.

Rules you must always follow:
1. You cannot execute SQL yourself. LazySQL runs queries only when the user explicitly chooses to, after a confirmation prompt. Never claim to have executed, run, or modified anything.
2. Only read-only SELECT-style queries may be offered for the user to run. For any query that modifies data or schema (INSERT, UPDATE, DELETE, DROP, ALTER, TRUNCATE, CREATE, GRANT, REVOKE, etc.), provide it strictly as text for the user to copy and review. Never present modifying SQL as something that can be run automatically, and always remind the user to review it before running it manually.
3. Always ask for confirmation before suggesting the user execute anything.
4. Use the provided connection, schema and (when available) result-set context to write correct SQL for the current database provider. If information is missing, ask the user rather than guessing.
5. Put SQL in fenced code blocks (` + "```sql" + `) so it can be extracted.`

// Message is a single chat message.
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

const (
	RoleSystem    = "system"
	RoleUser      = "user"
	RoleAssistant = "assistant"
)

// Client sends chat-completion requests to the Copilot Chat API.
type Client struct {
	httpClient *http.Client
	auth       *Authenticator
	model      string
}

// NewClient creates a chat client. httpClient may be nil (defaults to a client
// with a sane timeout). model defaults to DefaultModel when empty.
func NewClient(httpClient *http.Client, auth *Authenticator, model string) *Client {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 120 * time.Second}
	}
	if model == "" {
		model = DefaultModel
	}
	return &Client{httpClient: httpClient, auth: auth, model: model}
}

// SetModel updates the model used for subsequent requests.
func (c *Client) SetModel(model string) {
	if model != "" {
		c.model = model
	}
}

type chatRequest struct {
	Model    string    `json:"model"`
	Messages []Message `json:"messages"`
	Stream   bool      `json:"stream"`
}

type chatResponse struct {
	Choices []struct {
		Message Message `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

// Complete sends the conversation and returns the assistant's reply. The caller
// is responsible for prepending a system message if desired (see
// WithSystemPrompt).
func (c *Client) Complete(ctx context.Context, messages []Message) (string, error) {
	if c.auth == nil {
		return "", ErrNoToken
	}
	token, err := c.auth.CopilotToken(ctx)
	if err != nil {
		return "", err
	}

	payload, err := json.Marshal(chatRequest{
		Model:    c.model,
		Messages: messages,
		Stream:   false,
	})
	if err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, chatCompletionsURL, bytes.NewReader(payload))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	// Headers editor Copilot integrations are expected to send.
	req.Header.Set("Editor-Version", "LazySQL/1.0")
	req.Header.Set("Editor-Plugin-Version", "lazysql-copilot/1.0")
	req.Header.Set("Copilot-Integration-Id", "vscode-chat")

	resp, err := c.httpClient.Do(req)
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
		var cr chatResponse
		if json.NewDecoder(resp.Body).Decode(&cr) == nil && cr.Error != nil {
			return "", fmt.Errorf("copilot: %s", cr.Error.Message)
		}
		return "", fmt.Errorf("copilot: chat request failed: %s", resp.Status)
	}

	var cr chatResponse
	if err := json.NewDecoder(resp.Body).Decode(&cr); err != nil {
		return "", err
	}
	if cr.Error != nil {
		return "", fmt.Errorf("copilot: %s", cr.Error.Message)
	}
	if len(cr.Choices) == 0 {
		return "", errors.New("copilot: empty response")
	}
	return cr.Choices[0].Message.Content, nil
}

// WithSystemPrompt returns messages with the safety SystemPrompt prepended,
// unless the first message is already a system message.
func WithSystemPrompt(messages []Message) []Message {
	if len(messages) > 0 && messages[0].Role == RoleSystem {
		return messages
	}
	out := make([]Message, 0, len(messages)+1)
	out = append(out, Message{Role: RoleSystem, Content: SystemPrompt})
	return append(out, messages...)
}
