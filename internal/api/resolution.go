package api

import (
	"database/sql"
	"encoding/json"
	"log"
	"net/http"

	"github.com/hazyhaar/horostracker/internal/db"
	"github.com/hazyhaar/horostracker/internal/llm"
)

// RegisterResolutionRoutes adds Resolution-related API endpoints.
func (a *API) RegisterResolutionRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/resolution/{id}", a.handleGenerateResolution)
	mux.HandleFunc("GET /api/resolution/{id}", a.handleGetResolution)
	mux.HandleFunc("POST /api/render/{id}", a.handleRenderResolution)
	mux.HandleFunc("GET /api/renders/{id}", a.handleGetRenders)
}

// SetResolutionEngine injects the LLM resolution engine.
func (a *API) SetResolutionEngine(engine *llm.ResolutionEngine) {
	a.resEngine = engine
}

// SetChallengeRunner injects the adversarial challenge runner.
func (a *API) SetChallengeRunner(runner *llm.ChallengeRunner) {
	a.challengeRunner = runner
}

func (a *API) handleGenerateResolution(w http.ResponseWriter, r *http.Request) {
	if a.resEngine == nil {
		jsonError(w, "no LLM providers configured — resolution generation unavailable", http.StatusServiceUnavailable)
		return
	}

	claims := a.auth.ExtractClaims(r)
	if claims == nil {
		jsonError(w, "authentication required", http.StatusUnauthorized)
		return
	}

	nodeID := r.PathValue("id")
	if nodeID == "" {
		jsonError(w, "id is required", http.StatusBadRequest)
		return
	}

	var req struct {
		Provider string `json:"provider"`
		Model    string `json:"model"`
	}
	json.NewDecoder(r.Body).Decode(&req)

	// Get the full tree
	tree, err := a.db.GetTree(nodeID, 100)
	if err != nil {
		if err == sql.ErrNoRows {
			jsonError(w, "node not found", http.StatusNotFound)
			return
		}
		log.Printf("error getting tree: %v", err)
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}

	// Generate resolution
	result, err := a.resEngine.GenerateResolution(r.Context(), tree, req.Provider, req.Model)
	if err != nil {
		log.Printf("error generating resolution: %v", err)
		jsonError(w, "resolution generation failed: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Store as a resolution node attached to the tree root
	resNode, err := a.db.CreateNode(db.CreateNodeInput{
		ParentID: &nodeID,
		NodeType: "resolution",
		Body:     result.Content,
		AuthorID: claims.UserID,
		ModelID:  &result.Model,
		Metadata: mustJSON(map[string]interface{}{
			"provider":  result.Provider,
			"tokens_in": result.TokensIn,
			"tokens_out": result.TokensOut,
			"latency_ms": result.LatencyMs,
		}),
	})
	if err != nil {
		log.Printf("error storing resolution: %v", err)
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}

	jsonResp(w, http.StatusCreated, map[string]interface{}{
		"resolution": resNode,
		"generation": result,
	})
}

func (a *API) handleGetResolution(w http.ResponseWriter, r *http.Request) {
	nodeID := r.PathValue("id")
	if nodeID == "" {
		jsonError(w, "id is required", http.StatusBadRequest)
		return
	}

	// Find resolution nodes for this tree
	nodes, err := a.db.GetNodesByRoot(nodeID)
	if err != nil {
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}

	var resolutions []*db.Node
	for _, n := range nodes {
		if n.NodeType == "resolution" {
			resolutions = append(resolutions, n)
		}
	}

	if len(resolutions) == 0 {
		jsonError(w, "no resolution found for this tree", http.StatusNotFound)
		return
	}

	jsonResp(w, http.StatusOK, map[string]interface{}{
		"resolutions": resolutions,
		"count":       len(resolutions),
	})
}

func (a *API) handleRenderResolution(w http.ResponseWriter, r *http.Request) {
	if a.resEngine == nil {
		jsonError(w, "no LLM providers configured", http.StatusServiceUnavailable)
		return
	}

	resolutionID := r.PathValue("id")
	if resolutionID == "" {
		jsonError(w, "id is required", http.StatusBadRequest)
		return
	}

	var req struct {
		Format   string `json:"format"`
		Provider string `json:"provider"`
		Model    string `json:"model"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if req.Format == "" {
		jsonError(w, "format is required (article, faq, thread, summary)", http.StatusBadRequest)
		return
	}

	// Get the resolution node
	resNode, err := a.db.GetNode(resolutionID)
	if err != nil {
		if err == sql.ErrNoRows {
			jsonError(w, "resolution not found", http.StatusNotFound)
			return
		}
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}

	// Render
	result, err := a.resEngine.RenderResolution(r.Context(), resNode.Body, req.Format, req.Provider, req.Model)
	if err != nil {
		log.Printf("error rendering resolution: %v", err)
		jsonError(w, "render failed: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Store in renders table
	renderID := db.NewID()
	a.db.Exec(`INSERT INTO renders (id, resolution_id, format, model_id, content, fidelity_score)
		VALUES (?, ?, ?, ?, ?, NULL)`, renderID, resolutionID, req.Format, result.Model, result.Content)

	jsonResp(w, http.StatusCreated, map[string]interface{}{
		"render_id": renderID,
		"render":    result,
	})
}

func (a *API) handleGetRenders(w http.ResponseWriter, r *http.Request) {
	resolutionID := r.PathValue("id")
	if resolutionID == "" {
		jsonError(w, "id is required", http.StatusBadRequest)
		return
	}

	rows, err := a.db.Query(`
		SELECT id, resolution_id, format, model_id, content, fidelity_score, created_at
		FROM renders WHERE resolution_id = ?
		ORDER BY created_at DESC`, resolutionID)
	if err != nil {
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	type Render struct {
		ID            string  `json:"id"`
		ResolutionID  string  `json:"resolution_id"`
		Format        string  `json:"format"`
		ModelID       *string `json:"model_id,omitempty"`
		Content       *string `json:"content,omitempty"`
		FidelityScore *int    `json:"fidelity_score,omitempty"`
		CreatedAt     string  `json:"created_at"`
	}

	var renders []Render
	for rows.Next() {
		var rd Render
		var modelID, content sql.NullString
		var score sql.NullInt64
		if err := rows.Scan(&rd.ID, &rd.ResolutionID, &rd.Format, &modelID, &content, &score, &rd.CreatedAt); err != nil {
			continue
		}
		if modelID.Valid {
			rd.ModelID = &modelID.String
		}
		if content.Valid {
			rd.Content = &content.String
		}
		if score.Valid {
			s := int(score.Int64)
			rd.FidelityScore = &s
		}
		renders = append(renders, rd)
	}

	jsonResp(w, http.StatusOK, renders)
}

func mustJSON(v interface{}) string {
	b, _ := json.Marshal(v)
	return string(b)
}
