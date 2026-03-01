// CLAUDE:SUMMARY Dynamic discovery of available LLM models across all configured providers
package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/hazyhaar/horostracker/internal/db"
)

// ModelDiscovery handles dynamic discovery of available LLM models.
type ModelDiscovery struct {
	flowsDB *db.FlowsDB
	client  *Client
	httpCl  *http.Client
	logger  *slog.Logger
}

// NewModelDiscovery creates a model discovery service.
func NewModelDiscovery(flowsDB *db.FlowsDB, client *Client, logger *slog.Logger) *ModelDiscovery {
	return &ModelDiscovery{
		flowsDB: flowsDB,
		client:  client,
		httpCl:  &http.Client{Timeout: 15 * time.Second},
		logger:  logger,
	}
}

// providerEndpoint maps provider names to their models listing endpoint and API style.
type providerEndpoint struct {
	name    string
	baseURL string
	apiKey  string
	style   string // "openai", "gemini", "anthropic"
}

// DiscoverAll runs discovery for all configured providers.
func (md *ModelDiscovery) DiscoverAll(ctx context.Context) {
	endpoints := md.buildEndpoints()
	if len(endpoints) == 0 {
		md.logger.Info("no providers configured for model discovery")
		return
	}

	totalDiscovered := 0
	for _, ep := range endpoints {
		models, err := md.discoverProvider(ctx, ep)
		if err != nil {
			md.logger.Warn("model discovery failed",
				"provider", ep.name,
				"error", err,
			)
			_ = md.flowsDB.MarkAllUnavailableForProvider(ep.name)
			continue
		}

		for _, m := range models {
			_ = md.flowsDB.UpsertModel(m)
		}
		totalDiscovered += len(models)

		md.logger.Info("models discovered",
			"provider", ep.name,
			"count", len(models),
		)
	}

	_ = md.flowsDB.InsertAuditLog("", "", "model_discovered", map[string]interface{}{
		"total_models": totalDiscovered,
	})
}

// buildEndpoints creates the list of provider endpoints from the configured LLM client.
func (md *ModelDiscovery) buildEndpoints() []providerEndpoint {
	var endpoints []providerEndpoint

	for _, name := range md.client.Providers() {
		p := md.client.providers[name]
		switch prov := p.(type) {
		case *OpenAIProvider:
			endpoints = append(endpoints, providerEndpoint{
				name:    name,
				baseURL: prov.baseURL,
				apiKey:  prov.apiKey,
				style:   "openai",
			})
		case *GeminiProvider:
			endpoints = append(endpoints, providerEndpoint{
				name:    name,
				baseURL: "https://generativelanguage.googleapis.com/v1beta",
				apiKey:  prov.apiKey,
				style:   "gemini",
			})
		case *AnthropicProvider:
			// Anthropic has no models listing endpoint; use hardcoded list
			endpoints = append(endpoints, providerEndpoint{
				name:    name,
				baseURL: "",
				apiKey:  prov.apiKey,
				style:   "anthropic",
			})
		}
	}

	return endpoints
}

// discoverProvider queries a single provider for available models.
func (md *ModelDiscovery) discoverProvider(ctx context.Context, ep providerEndpoint) ([]*db.AvailableModel, error) {
	switch ep.style {
	case "openai":
		return md.discoverOpenAI(ctx, ep)
	case "gemini":
		return md.discoverGemini(ctx, ep)
	case "anthropic":
		return md.discoverAnthropic(ep)
	default:
		return nil, fmt.Errorf("unknown API style: %s", ep.style)
	}
}

// discoverOpenAI queries GET /v1/models for OpenAI-compatible APIs.
func (md *ModelDiscovery) discoverOpenAI(ctx context.Context, ep providerEndpoint) ([]*db.AvailableModel, error) {
	url := strings.TrimSuffix(ep.baseURL, "/") + "/models"
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+ep.apiKey)

	resp, err := md.httpCl.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}

	var result struct {
		Data []struct {
			ID      string `json:"id"`
			Created int64  `json:"created"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}

	models := make([]*db.AvailableModel, 0, len(result.Data))
	for _, m := range result.Data {
		modelID := ep.name + "/" + m.ID
		models = append(models, &db.AvailableModel{
			ModelID:          modelID,
			Provider:         ep.name,
			ModelName:        m.ID,
			IsAvailable:      true,
			CapabilitiesJSON: "{}",
		})
	}
	return models, nil
}

// discoverGemini queries the Gemini API for available models.
func (md *ModelDiscovery) discoverGemini(ctx context.Context, ep providerEndpoint) ([]*db.AvailableModel, error) {
	url := ep.baseURL + "/models?key=" + ep.apiKey
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}

	resp, err := md.httpCl.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}

	var result struct {
		Models []struct {
			Name        string `json:"name"`
			DisplayName string `json:"displayName"`
		} `json:"models"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}

	models := make([]*db.AvailableModel, 0, len(result.Models))
	for _, m := range result.Models {
		// Gemini model names look like "models/gemini-2.0-flash"
		modelName := strings.TrimPrefix(m.Name, "models/")
		modelID := "gemini/" + modelName
		displayName := m.DisplayName
		models = append(models, &db.AvailableModel{
			ModelID:          modelID,
			Provider:         "gemini",
			ModelName:        modelName,
			DisplayName:      &displayName,
			IsAvailable:      true,
			CapabilitiesJSON: "{}",
		})
	}
	return models, nil
}

// discoverAnthropic returns a hardcoded list (no API endpoint for listing).
//
//nolint:unparam // ep kept for interface consistency with discoverOpenAI/discoverGemini
func (md *ModelDiscovery) discoverAnthropic(ep providerEndpoint) ([]*db.AvailableModel, error) {
	knownModels := []struct {
		id      string
		display string
		ctx     int
	}{
		{"claude-sonnet-4-5-20250929", "Claude Sonnet 4.5", 200000},
		{"claude-haiku-4-5-20251001", "Claude Haiku 4.5", 200000},
		{"claude-opus-4-6", "Claude Opus 4.6", 200000},
	}

	models := make([]*db.AvailableModel, 0, len(knownModels))
	for _, m := range knownModels {
		modelID := "anthropic/" + m.id
		display := m.display
		ctx := m.ctx
		models = append(models, &db.AvailableModel{
			ModelID:          modelID,
			Provider:         "anthropic",
			ModelName:        m.id,
			DisplayName:      &display,
			ContextWindow:    &ctx,
			IsAvailable:      true,
			CapabilitiesJSON: "{}",
		})
	}
	return models, nil
}
