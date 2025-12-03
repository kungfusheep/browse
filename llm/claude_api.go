package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
)

const (
	anthropicAPIURL     = "https://api.anthropic.com/v1/messages"
	anthropicAPIVersion = "2023-06-01"
	defaultModel        = "claude-sonnet-4-20250514"
)

// ClaudeAPI implements Provider using the Anthropic API directly.
type ClaudeAPI struct {
	apiKey string
	model  string
	client *http.Client
}

// NewClaudeAPI creates a new Claude API provider.
// If apiKey is empty, it reads from ANTHROPIC_API_KEY environment variable.
func NewClaudeAPI(apiKey string) *ClaudeAPI {
	if apiKey == "" {
		apiKey = os.Getenv("ANTHROPIC_API_KEY")
	}
	return &ClaudeAPI{
		apiKey: apiKey,
		model:  defaultModel,
		client: &http.Client{},
	}
}

// WithModel sets a specific model to use.
func (c *ClaudeAPI) WithModel(model string) *ClaudeAPI {
	c.model = model
	return c
}

// Name returns the provider name.
func (c *ClaudeAPI) Name() string {
	return "claude-api"
}

// Available checks if an API key is configured.
func (c *ClaudeAPI) Available() bool {
	return c.apiKey != ""
}

// Complete sends a prompt to the Anthropic API.
func (c *ClaudeAPI) Complete(ctx context.Context, prompt string) (string, error) {
	return c.complete(ctx, "", []apiMessage{{Role: "user", Content: prompt}})
}

// CompleteWithSystem sends a prompt with system message to the Anthropic API.
func (c *ClaudeAPI) CompleteWithSystem(ctx context.Context, system, prompt string) (string, error) {
	return c.complete(ctx, system, []apiMessage{{Role: "user", Content: prompt}})
}

// CompleteConversation sends a multi-turn conversation to the Anthropic API.
func (c *ClaudeAPI) CompleteConversation(ctx context.Context, system string, messages []Message) (string, error) {
	apiMsgs := make([]apiMessage, len(messages))
	for i, msg := range messages {
		apiMsgs[i] = apiMessage{Role: msg.Role, Content: msg.Content}
	}
	return c.complete(ctx, system, apiMsgs)
}

func (c *ClaudeAPI) complete(ctx context.Context, system string, messages []apiMessage) (string, error) {
	reqBody := apiRequest{
		Model:     c.model,
		MaxTokens: 4096,
		Messages:  messages,
	}

	if system != "" {
		reqBody.System = system
	}

	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("marshaling request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", anthropicAPIURL, bytes.NewReader(jsonBody))
	if err != nil {
		return "", fmt.Errorf("creating request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", c.apiKey)
	req.Header.Set("anthropic-version", anthropicAPIVersion)

	resp, err := c.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("API request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("reading response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		var apiErr apiErrorResponse
		if json.Unmarshal(body, &apiErr) == nil && apiErr.Error.Message != "" {
			return "", fmt.Errorf("API error (%d): %s", resp.StatusCode, apiErr.Error.Message)
		}
		return "", fmt.Errorf("API error (%d): %s", resp.StatusCode, string(body))
	}

	var apiResp apiResponse
	if err := json.Unmarshal(body, &apiResp); err != nil {
		return "", fmt.Errorf("parsing response: %w", err)
	}

	// Extract text from content blocks
	var result string
	for _, block := range apiResp.Content {
		if block.Type == "text" {
			result += block.Text
		}
	}

	return result, nil
}

// API request/response types

type apiRequest struct {
	Model     string       `json:"model"`
	MaxTokens int          `json:"max_tokens"`
	System    string       `json:"system,omitempty"`
	Messages  []apiMessage `json:"messages"`
}

type apiMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type apiResponse struct {
	Content []apiContentBlock `json:"content"`
}

type apiContentBlock struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

type apiErrorResponse struct {
	Error struct {
		Message string `json:"message"`
	} `json:"error"`
}
