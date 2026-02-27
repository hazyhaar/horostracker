// CLAUDE:SUMMARY LLM Provider implementation for Google Gemini API (Flash, Pro models)
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

// GeminiProvider implements the Provider interface for Google's Gemini API.
type GeminiProvider struct {
	apiKey string
	models []string
	client *http.Client
}

func NewGeminiProvider(apiKey string) *GeminiProvider {
	return &GeminiProvider{
		apiKey: apiKey,
		models: []string{"gemini-2.0-flash", "gemini-2.0-flash-lite", "gemini-1.5-pro"},
		client: &http.Client{Timeout: 120 * time.Second},
	}
}

func (p *GeminiProvider) Name() string     { return "gemini" }
func (p *GeminiProvider) Models() []string  { return p.models }

func (p *GeminiProvider) Complete(ctx context.Context, req Request) (*Response, error) {
	model := req.Model
	if model == "" {
		model = p.models[0]
	}

	// Build Gemini request
	var systemInstruction *geminiContent
	var contents []geminiContent
	for _, m := range req.Messages {
		if m.Role == "system" {
			systemInstruction = &geminiContent{
				Parts: []geminiPart{{Text: m.Content}},
			}
			continue
		}
		role := m.Role
		if role == "assistant" {
			role = "model"
		}
		contents = append(contents, geminiContent{
			Role:  role,
			Parts: []geminiPart{{Text: m.Content}},
		})
	}

	body := geminiRequest{
		Contents: contents,
	}
	if systemInstruction != nil {
		body.SystemInstruction = systemInstruction
	}
	if req.Temperature > 0 || req.MaxTokens > 0 || req.TopP > 0 {
		gc := &geminiGenerationConfig{}
		if req.Temperature > 0 {
			gc.Temperature = &req.Temperature
		}
		if req.MaxTokens > 0 {
			gc.MaxOutputTokens = &req.MaxTokens
		}
		if req.TopP > 0 {
			gc.TopP = &req.TopP
		}
		body.GenerationConfig = gc
	}

	payload, err := json.Marshal(body)
	if err != nil {
		return nil, &ProviderError{Provider: "gemini", Model: model, Err: err}
	}

	url := fmt.Sprintf("https://generativelanguage.googleapis.com/v1beta/models/%s:generateContent?key=%s",
		model, p.apiKey)
	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(payload))
	if err != nil {
		return nil, &ProviderError{Provider: "gemini", Model: model, Err: err}
	}
	httpReq.Header.Set("Content-Type", "application/json")

	start := time.Now()
	httpResp, err := p.client.Do(httpReq)
	latency := time.Since(start)
	if err != nil {
		return nil, &ProviderError{Provider: "gemini", Model: model, Err: err}
	}
	defer httpResp.Body.Close()

	respBody, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return nil, &ProviderError{Provider: "gemini", Model: model, Err: err}
	}

	if httpResp.StatusCode == 429 {
		return nil, &ProviderError{Provider: "gemini", Model: model, Err: ErrRateLimited}
	}
	if httpResp.StatusCode != 200 {
		return nil, &ProviderError{Provider: "gemini", Model: model,
			Err: fmt.Errorf("HTTP %d: %s", httpResp.StatusCode, truncate(string(respBody), 200))}
	}

	var gemResp geminiResponse
	if err := json.Unmarshal(respBody, &gemResp); err != nil {
		return nil, &ProviderError{Provider: "gemini", Model: model, Err: fmt.Errorf("decoding response: %w", err)}
	}

	if len(gemResp.Candidates) == 0 {
		return nil, &ProviderError{Provider: "gemini", Model: model, Err: fmt.Errorf("no candidates in response")}
	}

	var content string
	for _, part := range gemResp.Candidates[0].Content.Parts {
		content += part.Text
	}

	var tokensIn, tokensOut int
	if gemResp.UsageMetadata != nil {
		tokensIn = gemResp.UsageMetadata.PromptTokenCount
		tokensOut = gemResp.UsageMetadata.CandidatesTokenCount
	}

	return &Response{
		Provider:     "gemini",
		Model:        model,
		Content:      content,
		TokensIn:     tokensIn,
		TokensOut:    tokensOut,
		FinishReason: gemResp.Candidates[0].FinishReason,
		Latency:      latency,
	}, nil
}

// Gemini API types
type geminiRequest struct {
	Contents          []geminiContent         `json:"contents"`
	SystemInstruction *geminiContent          `json:"systemInstruction,omitempty"`
	GenerationConfig  *geminiGenerationConfig `json:"generationConfig,omitempty"`
}

type geminiContent struct {
	Role  string       `json:"role,omitempty"`
	Parts []geminiPart `json:"parts"`
}

type geminiPart struct {
	Text string `json:"text"`
}

type geminiGenerationConfig struct {
	Temperature    *float64 `json:"temperature,omitempty"`
	MaxOutputTokens *int    `json:"maxOutputTokens,omitempty"`
	TopP           *float64 `json:"topP,omitempty"`
}

type geminiResponse struct {
	Candidates []struct {
		Content      geminiContent `json:"content"`
		FinishReason string        `json:"finishReason"`
	} `json:"candidates"`
	UsageMetadata *struct {
		PromptTokenCount     int `json:"promptTokenCount"`
		CandidatesTokenCount int `json:"candidatesTokenCount"`
	} `json:"usageMetadata"`
}
