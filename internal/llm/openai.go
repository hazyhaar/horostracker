// CLAUDE:SUMMARY Generic OpenAI-compatible Provider (DeepSeek, Groq, Mistral, OpenRouter, Together, etc.)
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

// OpenAIProvider implements the Provider interface for any OpenAI-compatible API.
// This covers: OpenAI, DeepSeek, Groq, Mistral, OpenRouter, Together,
// Cerebras, SambaNova, HuggingFace Inference, and others.
type OpenAIProvider struct {
	name     string
	baseURL  string
	apiKey   string
	models   []string
	defModel string
	client   *http.Client
}

// OpenAIConfig configures an OpenAI-compatible provider.
type OpenAIConfig struct {
	Name         string
	BaseURL      string // e.g. "https://api.deepseek.com/v1"
	APIKey       string
	Models       []string // available model IDs
	DefaultModel string   // fallback model if none specified
}

func NewOpenAIProvider(cfg OpenAIConfig) *OpenAIProvider {
	defModel := cfg.DefaultModel
	if defModel == "" && len(cfg.Models) > 0 {
		defModel = cfg.Models[0]
	}
	return &OpenAIProvider{
		name:     cfg.Name,
		baseURL:  cfg.BaseURL,
		apiKey:   cfg.APIKey,
		models:   cfg.Models,
		defModel: defModel,
		client:   &http.Client{Timeout: 120 * time.Second},
	}
}

func (p *OpenAIProvider) Name() string     { return p.name }
func (p *OpenAIProvider) Models() []string  { return p.models }

func (p *OpenAIProvider) Complete(ctx context.Context, req Request) (*Response, error) {
	model := req.Model
	if model == "" {
		model = p.defModel
	}
	if model == "" {
		return nil, &ProviderError{Provider: p.name, Err: fmt.Errorf("no model specified")}
	}

	// Build OpenAI-format request body
	body := openAIRequest{
		Model:    model,
		Messages: make([]openAIMessage, len(req.Messages)),
	}
	for i, m := range req.Messages {
		body.Messages[i] = openAIMessage(m)
	}
	if req.Temperature > 0 {
		body.Temperature = &req.Temperature
	}
	if req.MaxTokens > 0 {
		body.MaxTokens = &req.MaxTokens
	}
	if req.TopP > 0 {
		body.TopP = &req.TopP
	}
	if req.Seed != nil {
		body.Seed = req.Seed
	}

	payload, err := json.Marshal(body)
	if err != nil {
		return nil, &ProviderError{Provider: p.name, Model: model, Err: err}
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", p.baseURL+"/chat/completions", bytes.NewReader(payload))
	if err != nil {
		return nil, &ProviderError{Provider: p.name, Model: model, Err: err}
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if p.apiKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)
	}

	start := time.Now()
	httpResp, err := p.client.Do(httpReq)
	latency := time.Since(start)
	if err != nil {
		return nil, &ProviderError{Provider: p.name, Model: model, Err: err}
	}
	defer httpResp.Body.Close()

	respBody, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return nil, &ProviderError{Provider: p.name, Model: model, Err: err}
	}

	if httpResp.StatusCode == 429 {
		return nil, &ProviderError{Provider: p.name, Model: model, Err: ErrRateLimited}
	}
	if httpResp.StatusCode != 200 {
		return nil, &ProviderError{Provider: p.name, Model: model,
			Err: fmt.Errorf("HTTP %d: %s", httpResp.StatusCode, truncate(string(respBody), 200))}
	}

	var oaiResp openAIResponse
	if err := json.Unmarshal(respBody, &oaiResp); err != nil {
		return nil, &ProviderError{Provider: p.name, Model: model, Err: fmt.Errorf("decoding response: %w", err)}
	}

	if len(oaiResp.Choices) == 0 {
		return nil, &ProviderError{Provider: p.name, Model: model, Err: fmt.Errorf("no choices in response")}
	}

	choice := oaiResp.Choices[0]
	return &Response{
		Provider:     p.name,
		Model:        oaiResp.Model,
		Content:      choice.Message.Content,
		TokensIn:     oaiResp.Usage.PromptTokens,
		TokensOut:    oaiResp.Usage.CompletionTokens,
		FinishReason: choice.FinishReason,
		Latency:      latency,
	}, nil
}

// OpenAI API types
type openAIRequest struct {
	Model       string          `json:"model"`
	Messages    []openAIMessage `json:"messages"`
	Temperature *float64        `json:"temperature,omitempty"`
	MaxTokens   *int            `json:"max_tokens,omitempty"`
	TopP        *float64        `json:"top_p,omitempty"`
	Seed        *int            `json:"seed,omitempty"`
}

type openAIMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type openAIResponse struct {
	Model   string `json:"model"`
	Choices []struct {
		Message      openAIMessage `json:"message"`
		FinishReason string        `json:"finish_reason"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
	} `json:"usage"`
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
