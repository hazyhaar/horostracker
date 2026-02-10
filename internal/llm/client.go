// Package llm provides a multi-provider LLM client with fallback chain,
// structured response logging to flows.db, and cost tracking to metrics.db.
package llm

import (
	"context"
	"time"
)

// Message represents a chat message (system/user/assistant).
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// Request is a provider-agnostic LLM completion request.
type Request struct {
	Model       string    `json:"model"`
	Messages    []Message `json:"messages"`
	Temperature float64   `json:"temperature,omitempty"`
	MaxTokens   int       `json:"max_tokens,omitempty"`
	TopP        float64   `json:"top_p,omitempty"`
	Seed        *int      `json:"seed,omitempty"`
}

// Response is a provider-agnostic LLM completion response.
type Response struct {
	Provider     string        `json:"provider"`
	Model        string        `json:"model"`
	Content      string        `json:"content"`
	TokensIn     int           `json:"tokens_in"`
	TokensOut    int           `json:"tokens_out"`
	FinishReason string        `json:"finish_reason"`
	Latency      time.Duration `json:"latency_ms"`
}

// Provider is a single LLM API backend.
type Provider interface {
	// Name returns the provider identifier (e.g. "deepseek", "groq").
	Name() string
	// Models returns the list of model IDs available on this provider.
	Models() []string
	// Complete sends a chat completion request and returns the response.
	Complete(ctx context.Context, req Request) (*Response, error)
}

// Client sends LLM requests with fallback across multiple providers.
type Client struct {
	providers map[string]Provider // keyed by provider name
	fallback  []string            // provider names in priority order
}

// New creates a multi-provider LLM client.
func New(providers []Provider) *Client {
	m := make(map[string]Provider, len(providers))
	order := make([]string, 0, len(providers))
	for _, p := range providers {
		m[p.Name()] = p
		order = append(order, p.Name())
	}
	return &Client{providers: m, fallback: order}
}

// Complete sends a request to the specified provider, or falls back through
// the chain if the provider fails or is unspecified.
func (c *Client) Complete(ctx context.Context, req Request) (*Response, error) {
	// If model contains a provider prefix (e.g. "groq/llama-3-70b"), route directly
	provider, model := splitModel(req.Model)
	if provider != "" {
		req.Model = model
		if p, ok := c.providers[provider]; ok {
			return p.Complete(ctx, req)
		}
	}

	// Try each provider in fallback order
	var lastErr error
	for _, name := range c.fallback {
		p := c.providers[name]
		resp, err := p.Complete(ctx, req)
		if err != nil {
			lastErr = err
			continue
		}
		return resp, nil
	}
	return nil, lastErr
}

// CompleteWith sends a request to a specific named provider.
func (c *Client) CompleteWith(ctx context.Context, providerName string, req Request) (*Response, error) {
	p, ok := c.providers[providerName]
	if !ok {
		return nil, &ProviderError{Provider: providerName, Err: ErrProviderNotFound}
	}
	return p.Complete(ctx, req)
}

// Providers returns the names of all configured providers.
func (c *Client) Providers() []string {
	return c.fallback
}

// HasProvider checks if a named provider is configured.
func (c *Client) HasProvider(name string) bool {
	_, ok := c.providers[name]
	return ok
}

func splitModel(model string) (provider, name string) {
	for i, c := range model {
		if c == '/' {
			return model[:i], model[i+1:]
		}
	}
	return "", model
}
