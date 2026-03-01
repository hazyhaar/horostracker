// CLAUDE:SUMMARY Multi-model inference dispatch API — fan-out prompts to multiple LLM models in parallel with optional persistence
package api

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"github.com/hazyhaar/horostracker/internal/db"
	"github.com/hazyhaar/horostracker/internal/llm"
)

func (a *API) RegisterDispatchRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/inference/dispatch", a.handleDispatch)
	mux.HandleFunc("GET /api/inference/results/{dispatchID}", a.handleDispatchResults)
}

func (a *API) handleDispatch(w http.ResponseWriter, r *http.Request) {
	if a.llmClient == nil {
		jsonError(w, "LLM client not configured", http.StatusServiceUnavailable)
		return
	}

	var req struct {
		Prompt    string   `json:"prompt"`
		System    string   `json:"system"`
		Models    []string `json:"models"`
		TimeoutMs int      `json:"timeout_ms"`
		Persist   bool     `json:"persist"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if req.Prompt == "" || len(req.Models) == 0 {
		jsonError(w, "prompt and models are required", http.StatusBadRequest)
		return
	}

	timeout := 30 * time.Second
	if req.TimeoutMs > 0 {
		timeout = time.Duration(req.TimeoutMs) * time.Millisecond
	}

	dispatchID := db.NewID()

	// Persist dispatch record
	if req.Persist && a.flowsDB != nil {
		modelsJSON, _ := json.Marshal(req.Models)
		_, _ = a.flowsDB.Exec(`INSERT INTO dispatches (id, prompt_hash, models, status) VALUES (?, ?, ?, 'running')`,
			dispatchID, hashPrompt(req.Prompt), string(modelsJSON))
	}

	ctx, cancel := context.WithTimeout(r.Context(), timeout)
	defer cancel()

	type modelResult struct {
		Model     string `json:"model"`
		Provider  string `json:"provider"`
		Content   string `json:"content"`
		TokensIn  int    `json:"tokens_in"`
		TokensOut int    `json:"tokens_out"`
		LatencyMs int    `json:"latency_ms"`
		Error     string `json:"error,omitempty"`
	}

	results := make([]modelResult, len(req.Models))
	var wg sync.WaitGroup

	for i, model := range req.Models {
		wg.Add(1)
		go func(i int, model string) {
			defer wg.Done()

			var messages []llm.Message
			if req.System != "" {
				messages = append(messages, llm.Message{Role: "system", Content: req.System})
			}
			messages = append(messages, llm.Message{Role: "user", Content: req.Prompt})

			llmReq := llm.Request{
				Model:    model,
				Messages: messages,
			}

			start := time.Now()
			resp, err := a.llmClient.Complete(ctx, llmReq)
			latency := time.Since(start)

			mr := modelResult{
				Model:     model,
				LatencyMs: int(latency.Milliseconds()),
			}

			if err != nil {
				mr.Error = err.Error()
			} else {
				mr.Content = resp.Content
				mr.Provider = resp.Provider
				mr.TokensIn = resp.TokensIn
				mr.TokensOut = resp.TokensOut
				mr.Model = resp.Model
			}

			results[i] = mr

			// Persist step if requested
			if req.Persist && a.flowsDB != nil {
				stepID := db.NewID()
				errStr := ""
				if err != nil {
					errStr = err.Error()
				}
				content := ""
				if resp != nil {
					content = resp.Content
				}
				_, _ = a.flowsDB.Exec(`INSERT INTO flow_steps (id, flow_id, step_index, model_id, provider,
					prompt, system_prompt, response_raw, response_parsed,
					tokens_in, tokens_out, latency_ms, dispatch_id, error)
					VALUES (?, ?, 0, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
					stepID, dispatchID, model, mr.Provider,
					req.Prompt, req.System, content, content,
					mr.TokensIn, mr.TokensOut, mr.LatencyMs,
					dispatchID, nilStr(errStr))
			}
		}(i, model)
	}

	wg.Wait()

	// Update dispatch status
	if req.Persist && a.flowsDB != nil {
		_, _ = a.flowsDB.Exec(`UPDATE dispatches SET status = 'completed', completed_at = datetime('now') WHERE id = ?`, dispatchID)
	}

	jsonResp(w, http.StatusOK, map[string]interface{}{
		"dispatch_id": dispatchID,
		"results":     results,
	})
}

func (a *API) handleDispatchResults(w http.ResponseWriter, r *http.Request) {
	if a.flowsDB == nil {
		jsonError(w, "flows database not configured", http.StatusServiceUnavailable)
		return
	}

	dispatchID := r.PathValue("dispatchID")

	// Get dispatch info
	var promptHash, models, status string
	var completedAt *string
	row := a.flowsDB.QueryRow(`SELECT prompt_hash, models, status, completed_at FROM dispatches WHERE id = ?`, dispatchID)
	if err := row.Scan(&promptHash, &models, &status, &completedAt); err != nil {
		jsonError(w, "dispatch not found", http.StatusNotFound)
		return
	}

	// Get steps
	rows, err := a.flowsDB.Query(`
		SELECT id, model_id, provider, COALESCE(response_raw,''), COALESCE(tokens_in,0),
			COALESCE(tokens_out,0), COALESCE(latency_ms,0), COALESCE(error,'')
		FROM flow_steps WHERE dispatch_id = ? ORDER BY model_id`, dispatchID)
	if err != nil {
		jsonError(w, "query failed", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	type stepResult struct {
		ID        string `json:"id"`
		ModelID   string `json:"model_id"`
		Provider  string `json:"provider"`
		Content   string `json:"content"`
		TokensIn  int    `json:"tokens_in"`
		TokensOut int    `json:"tokens_out"`
		LatencyMs int    `json:"latency_ms"`
		Error     string `json:"error,omitempty"`
	}

	var steps []stepResult
	for rows.Next() {
		var s stepResult
		if rows.Scan(&s.ID, &s.ModelID, &s.Provider, &s.Content, &s.TokensIn, &s.TokensOut, &s.LatencyMs, &s.Error) == nil {
			steps = append(steps, s)
		}
	}

	jsonResp(w, http.StatusOK, map[string]interface{}{
		"dispatch_id":  dispatchID,
		"prompt_hash":  promptHash,
		"models":       models,
		"status":       status,
		"completed_at": completedAt,
		"results":      steps,
	})
}

func nilStr(s string) interface{} {
	if s == "" {
		return nil
	}
	return s
}

func hashPrompt(s string) string {
	// Simple fast hash for grouping
	h := uint64(0)
	for _, c := range s {
		h = h*31 + uint64(c)
	}
	return db.NewID() // Use nanoid for uniqueness; prompt hash is just for grouping
}
