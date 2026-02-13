package api

import (
	"encoding/json"
	"net/http"

	"github.com/hazyhaar/horostracker/internal/db"
)

func (a *API) RegisterProviderRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/providers/register", a.handleProviderRegister)
	mux.HandleFunc("GET /api/providers", a.handleProviderList)
	mux.HandleFunc("GET /api/providers/{id}", a.handleProviderGet)
	mux.HandleFunc("POST /api/providers/{id}/heartbeat", a.handleProviderHeartbeat)
}

func (a *API) handleProviderRegister(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name              string   `json:"name"`
		Endpoint          string   `json:"endpoint"`
		APIStyle          string   `json:"api_style"`
		Models            []string `json:"models"`
		ResolutionSpace   bool     `json:"resolution_space"`
		ResolutionCriteria json.RawMessage `json:"resolution_criteria"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if req.Name == "" {
		jsonError(w, "name is required", http.StatusBadRequest)
		return
	}
	if req.APIStyle == "" {
		req.APIStyle = "openai"
	}

	modelsJSON, _ := json.Marshal(req.Models)
	criteriaStr := "{}"
	if req.ResolutionCriteria != nil {
		criteriaStr = string(req.ResolutionCriteria)
	}

	id := db.NewID()
	resSpace := 0
	if req.ResolutionSpace {
		resSpace = 1
	}

	_, err := a.db.Exec(`INSERT INTO providers (id, name, endpoint, api_style, models, resolution_space, resolution_criteria)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		id, req.Name, req.Endpoint, req.APIStyle, string(modelsJSON), resSpace, criteriaStr)
	if err != nil {
		jsonError(w, "registration failed: "+err.Error(), http.StatusInternalServerError)
		return
	}

	jsonResp(w, http.StatusCreated, map[string]interface{}{
		"id":       id,
		"name":     req.Name,
		"endpoint": req.Endpoint,
		"models":   req.Models,
	})
}

func (a *API) handleProviderList(w http.ResponseWriter, r *http.Request) {
	rows, err := a.db.Query(`SELECT id, name, endpoint, api_style, models, is_active, COALESCE(last_seen_at,''), created_at
		FROM providers ORDER BY created_at DESC`)
	if err != nil {
		jsonError(w, "query failed", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	type providerInfo struct {
		ID         string `json:"id"`
		Name       string `json:"name"`
		Endpoint   string `json:"endpoint"`
		APIStyle   string `json:"api_style"`
		Models     string `json:"models"`
		IsActive   bool   `json:"is_active"`
		LastSeenAt string `json:"last_seen_at,omitempty"`
		CreatedAt  string `json:"created_at"`
	}

	var providers []providerInfo
	for rows.Next() {
		var p providerInfo
		var active int
		if rows.Scan(&p.ID, &p.Name, &p.Endpoint, &p.APIStyle, &p.Models, &active, &p.LastSeenAt, &p.CreatedAt) == nil {
			p.IsActive = active == 1
			providers = append(providers, p)
		}
	}

	jsonResp(w, http.StatusOK, map[string]interface{}{
		"providers": providers,
		"count":     len(providers),
	})
}

func (a *API) handleProviderGet(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	row := a.db.QueryRow(`SELECT id, name, endpoint, api_style, models, capabilities, is_active,
		resolution_space, resolution_criteria, COALESCE(last_seen_at,''), created_at
		FROM providers WHERE id = ?`, id)

	var name, endpoint, apiStyle, models, capabilities, criteria, lastSeen, createdAt string
	var active, resSpace int
	if err := row.Scan(&name, &name, &endpoint, &apiStyle, &models, &capabilities, &active,
		&resSpace, &criteria, &lastSeen, &createdAt); err != nil {
		jsonError(w, "provider not found", http.StatusNotFound)
		return
	}

	jsonResp(w, http.StatusOK, map[string]interface{}{
		"id":                 id,
		"name":               name,
		"endpoint":           endpoint,
		"api_style":          apiStyle,
		"models":             models,
		"capabilities":       capabilities,
		"is_active":          active == 1,
		"resolution_space":   resSpace == 1,
		"resolution_criteria": criteria,
		"last_seen_at":       lastSeen,
		"created_at":         createdAt,
	})
}

func (a *API) handleProviderHeartbeat(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	res, err := a.db.Exec(`UPDATE providers SET last_seen_at = datetime('now') WHERE id = ?`, id)
	if err != nil {
		jsonError(w, "heartbeat failed", http.StatusInternalServerError)
		return
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		jsonError(w, "provider not found", http.StatusNotFound)
		return
	}

	jsonResp(w, http.StatusOK, map[string]string{"status": "ok"})
}
