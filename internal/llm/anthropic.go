package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// AnthropicProvider implements the Provider interface for Anthropic's Messages API.
type AnthropicProvider struct {
	apiKey string
	models []string
	client *http.Client
}

func NewAnthropicProvider(apiKey string) *AnthropicProvider {
	return &AnthropicProvider{
		apiKey: apiKey,
		models: []string{"claude-sonnet-4-5-20250929", "claude-haiku-4-5-20251001"},
		client: &http.Client{Timeout: 120 * time.Second},
	}
}

func (p *AnthropicProvider) Name() string     { return "anthropic" }
func (p *AnthropicProvider) Models() []string  { return p.models }

func (p *AnthropicProvider) Complete(ctx context.Context, req Request) (*Response, error) {
	model := req.Model
	if model == "" {
		model = p.models[0]
	}

	// Extract system message if present
	var system string
	var messages []anthropicMessage
	for _, m := range req.Messages {
		if m.Role == "system" {
			system = m.Content
			continue
		}
		messages = append(messages, anthropicMessage{Role: m.Role, Content: m.Content})
	}

	body := anthropicRequest{
		Model:     model,
		Messages:  messages,
		MaxTokens: 4096,
	}
	if system != "" {
		body.System = system
	}
	if req.MaxTokens > 0 {
		body.MaxTokens = req.MaxTokens
	}
	if req.Temperature > 0 {
		body.Temperature = &req.Temperature
	}
	if req.TopP > 0 {
		body.TopP = &req.TopP
	}

	payload, err := json.Marshal(body)
	if err != nil {
		return nil, &ProviderError{Provider: "anthropic", Model: model, Err: err}
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", "https://api.anthropic.com/v1/messages", bytes.NewReader(payload))
	if err != nil {
		return nil, &ProviderError{Provider: "anthropic", Model: model, Err: err}
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", p.apiKey)
	httpReq.Header.Set("anthropic-version", "2023-06-01")

	start := time.Now()
	httpResp, err := p.client.Do(httpReq)
	latency := time.Since(start)
	if err != nil {
		return nil, &ProviderError{Provider: "anthropic", Model: model, Err: err}
	}
	defer httpResp.Body.Close()

	respBody, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return nil, &ProviderError{Provider: "anthropic", Model: model, Err: err}
	}

	if httpResp.StatusCode == 429 {
		return nil, &ProviderError{Provider: "anthropic", Model: model, Err: ErrRateLimited}
	}
	if httpResp.StatusCode != 200 {
		return nil, &ProviderError{Provider: "anthropic", Model: model,
			Err: fmt.Errorf("HTTP %d: %s", httpResp.StatusCode, truncate(string(respBody), 200))}
	}

	var anthResp anthropicResponse
	if err := json.Unmarshal(respBody, &anthResp); err != nil {
		return nil, &ProviderError{Provider: "anthropic", Model: model, Err: fmt.Errorf("decoding response: %w", err)}
	}

	var content string
	for _, block := range anthResp.Content {
		if block.Type == "text" {
			content += block.Text
		}
	}

	return &Response{
		Provider:     "anthropic",
		Model:        anthResp.Model,
		Content:      content,
		TokensIn:     anthResp.Usage.InputTokens,
		TokensOut:    anthResp.Usage.OutputTokens,
		FinishReason: anthResp.StopReason,
		Latency:      latency,
	}, nil
}

// Anthropic API types
type anthropicRequest struct {
	Model       string             `json:"model"`
	Messages    []anthropicMessage `json:"messages"`
	System      string             `json:"system,omitempty"`
	MaxTokens   int                `json:"max_tokens"`
	Temperature *float64           `json:"temperature,omitempty"`
	TopP        *float64           `json:"top_p,omitempty"`
}

type anthropicMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type anthropicResponse struct {
	Model      string `json:"model"`
	Content    []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	StopReason string `json:"stop_reason"`
	Usage      struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
}
