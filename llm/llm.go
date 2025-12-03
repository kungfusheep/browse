// Package llm provides an abstraction layer for language model providers.
package llm

import (
	"context"
	"errors"
)

// ErrNoProvider is returned when no LLM provider is configured or available.
var ErrNoProvider = errors.New("no LLM provider available")

// ErrSessionCollision is returned when session IDs collide repeatedly.
var ErrSessionCollision = errors.New("session ID collision")

// Message represents a single message in a conversation.
type Message struct {
	Role    string // "user" or "assistant"
	Content string
}

// Provider defines the interface for language model backends.
type Provider interface {
	// Name returns the provider name for display/logging.
	Name() string

	// Available checks if this provider is ready to use.
	Available() bool

	// Complete sends a prompt and returns the response.
	Complete(ctx context.Context, prompt string) (string, error)

	// CompleteWithSystem sends a prompt with a system message.
	CompleteWithSystem(ctx context.Context, system, prompt string) (string, error)

	// CompleteConversation sends a multi-turn conversation with system message.
	// Messages alternate between user and assistant roles.
	CompleteConversation(ctx context.Context, system string, messages []Message) (string, error)
}

// Client manages LLM providers and selects the best available one.
type Client struct {
	providers []Provider
	preferred Provider
}

// NewClient creates a new LLM client with the given providers.
// Providers are tried in order of preference.
func NewClient(providers ...Provider) *Client {
	return &Client{
		providers: providers,
	}
}

// SetPreferred sets a specific provider to use, bypassing auto-selection.
func (c *Client) SetPreferred(name string) bool {
	for _, p := range c.providers {
		if p.Name() == name && p.Available() {
			c.preferred = p
			return true
		}
	}
	return false
}

// Provider returns the currently active provider, or nil if none available.
func (c *Client) Provider() Provider {
	if c.preferred != nil && c.preferred.Available() {
		return c.preferred
	}

	// Find first available provider
	for _, p := range c.providers {
		if p.Available() {
			return p
		}
	}
	return nil
}

// Available returns true if any provider is available.
func (c *Client) Available() bool {
	return c.Provider() != nil
}

// Complete sends a prompt to the best available provider.
func (c *Client) Complete(ctx context.Context, prompt string) (string, error) {
	p := c.Provider()
	if p == nil {
		return "", ErrNoProvider
	}
	return p.Complete(ctx, prompt)
}

// CompleteWithSystem sends a prompt with system message to the best available provider.
func (c *Client) CompleteWithSystem(ctx context.Context, system, prompt string) (string, error) {
	p := c.Provider()
	if p == nil {
		return "", ErrNoProvider
	}
	return p.CompleteWithSystem(ctx, system, prompt)
}

// CompleteConversation sends a multi-turn conversation to the best available provider.
func (c *Client) CompleteConversation(ctx context.Context, system string, messages []Message) (string, error) {
	p := c.Provider()
	if p == nil {
		return "", ErrNoProvider
	}
	return p.CompleteConversation(ctx, system, messages)
}

// ListProviders returns info about all configured providers.
func (c *Client) ListProviders() []ProviderInfo {
	var infos []ProviderInfo
	for _, p := range c.providers {
		infos = append(infos, ProviderInfo{
			Name:      p.Name(),
			Available: p.Available(),
		})
	}
	return infos
}

// ProviderInfo describes a provider's status.
type ProviderInfo struct {
	Name      string
	Available bool
}
