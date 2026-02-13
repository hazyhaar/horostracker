package api

import (
	"encoding/json"
	"log"
	"net/http"
	"strconv"

	"github.com/hazyhaar/horostracker/internal/db"
)

// RegisterEnvelopeRoutes registers envelope routing endpoints.
func (a *API) RegisterEnvelopeRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/envelope", a.handleCreateEnvelope)
	mux.HandleFunc("POST /api/envelope/anon", a.handleCreateAnonEnvelope)
	mux.HandleFunc("GET /api/envelope/{id}", a.handleGetEnvelope)
	mux.HandleFunc("GET /api/envelope/{id}/status", a.handleGetEnvelopeStatus)
	mux.HandleFunc("POST /api/envelope/{id}/claim", a.handleClaimEnvelope)
	mux.HandleFunc("GET /api/envelopes", a.handleListEnvelopes)
	mux.HandleFunc("GET /api/envelopes/batch/{batchID}", a.handleListBatchEnvelopes)
	mux.HandleFunc("POST /api/envelope/{id}/deliver/{targetID}", a.handleDeliverTarget)
	mux.HandleFunc("POST /api/envelope/{id}/fail/{targetID}", a.handleFailTarget)
	mux.HandleFunc("POST /api/envelope/{id}/transition", a.handleUpdateEnvelopeStatus)
	mux.HandleFunc("POST /api/envelopes/expire", a.handleExpireEnvelopes)
}

// handleCreateEnvelope creates a new envelope with delivery targets.
// Auth required — the caller becomes the source_user_id.
// POST /api/envelope
// {
//   "source_type": "horostracker|witheout|api|mcp",
//   "source_node_id": "optional-node-id",
//   "source_callback": "optional-webhook-url",
//   "batch_id": "optional-batch-grouping",
//   "piece_hash": "sha256-of-piece",
//   "ttl_minutes": 15,
//   "targets": [
//     {"target_type": "horostracker", "target_config": "{}"},
//     {"target_type": "googledrive", "target_config": "{\"folder_id\":\"...\"}"}
//   ]
// }
func (a *API) handleCreateEnvelope(w http.ResponseWriter, r *http.Request) {
	claims := a.auth.ExtractClaims(r)
	if claims == nil {
		jsonError(w, "authentication required", http.StatusUnauthorized)
		return
	}

	var req struct {
		SourceType     string                 `json:"source_type"`
		SourceNodeID   *string                `json:"source_node_id"`
		SourceCallback *string                `json:"source_callback"`
		BatchID        *string                `json:"batch_id"`
		PieceHash      string                 `json:"piece_hash"`
		TTLMinutes     int                    `json:"ttl_minutes"`
		Targets        []db.CreateTargetInput `json:"targets"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if req.PieceHash == "" {
		jsonError(w, "piece_hash is required", http.StatusBadRequest)
		return
	}
	if len(req.Targets) == 0 {
		jsonError(w, "at least one target is required", http.StatusBadRequest)
		return
	}

	validSources := map[string]bool{
		"horostracker": true, "witheout": true, "api": true, "mcp": true,
	}
	if !validSources[req.SourceType] {
		jsonError(w, "invalid source_type", http.StatusBadRequest)
		return
	}

	validTargets := map[string]bool{
		"horostracker": true, "googledrive": true, "webhook": true,
		"email": true, "s3": true, "ipfs": true,
	}
	for _, t := range req.Targets {
		if !validTargets[t.TargetType] {
			jsonError(w, "invalid target_type: "+t.TargetType, http.StatusBadRequest)
			return
		}
	}

	envelope, err := a.db.CreateEnvelope(db.CreateEnvelopeInput{
		BatchID:        req.BatchID,
		SourceType:     req.SourceType,
		SourceUserID:   &claims.UserID,
		SourceNodeID:   req.SourceNodeID,
		SourceCallback: req.SourceCallback,
		PieceHash:      req.PieceHash,
		TTLMinutes:     req.TTLMinutes,
		Targets:        req.Targets,
	})
	if err != nil {
		log.Printf("error creating envelope: %v", err)
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}

	jsonResp(w, http.StatusCreated, envelope)
}

// handleGetEnvelope returns an envelope with its targets.
func (a *API) handleGetEnvelope(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		jsonError(w, "id is required", http.StatusBadRequest)
		return
	}

	envelope, err := a.db.GetEnvelope(id)
	if err != nil {
		jsonError(w, "envelope not found", http.StatusNotFound)
		return
	}

	// Only the source user or an admin can see the envelope
	claims := a.auth.ExtractClaims(r)
	if claims == nil {
		jsonError(w, "authentication required", http.StatusUnauthorized)
		return
	}
	if envelope.SourceUserID == nil || *envelope.SourceUserID != claims.UserID {
		jsonError(w, "forbidden", http.StatusForbidden)
		return
	}

	jsonResp(w, http.StatusOK, envelope)
}

// handleListEnvelopes returns the caller's envelopes.
func (a *API) handleListEnvelopes(w http.ResponseWriter, r *http.Request) {
	claims := a.auth.ExtractClaims(r)
	if claims == nil {
		jsonError(w, "authentication required", http.StatusUnauthorized)
		return
	}

	limit := 20
	if l, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && l > 0 {
		limit = l
	}

	envelopes, err := a.db.ListEnvelopesByUser(claims.UserID, limit)
	if err != nil {
		log.Printf("error listing envelopes: %v", err)
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}

	jsonResp(w, http.StatusOK, map[string]interface{}{
		"envelopes": envelopes,
		"count":     len(envelopes),
	})
}

// handleListBatchEnvelopes returns all envelopes in a batch.
func (a *API) handleListBatchEnvelopes(w http.ResponseWriter, r *http.Request) {
	claims := a.auth.ExtractClaims(r)
	if claims == nil {
		jsonError(w, "authentication required", http.StatusUnauthorized)
		return
	}

	batchID := r.PathValue("batchID")
	if batchID == "" {
		jsonError(w, "batchID is required", http.StatusBadRequest)
		return
	}

	envelopes, err := a.db.ListEnvelopesByBatch(batchID)
	if err != nil {
		log.Printf("error listing batch envelopes: %v", err)
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}

	jsonResp(w, http.StatusOK, map[string]interface{}{
		"batch_id":  batchID,
		"envelopes": envelopes,
		"count":     len(envelopes),
	})
}

// handleDeliverTarget marks a target as delivered (called by edge on return).
// POST /api/envelope/{id}/deliver/{targetID}
func (a *API) handleDeliverTarget(w http.ResponseWriter, r *http.Request) {
	envID := r.PathValue("id")
	targetID := r.PathValue("targetID")
	if envID == "" || targetID == "" {
		jsonError(w, "envelope id and target id required", http.StatusBadRequest)
		return
	}

	if err := a.db.DeliverTarget(envID, targetID); err != nil {
		log.Printf("error delivering target: %v", err)
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}

	jsonResp(w, http.StatusOK, map[string]string{"status": "delivered"})
}

// handleFailTarget marks a target as failed.
// POST /api/envelope/{id}/fail/{targetID}  {"error": "reason"}
func (a *API) handleFailTarget(w http.ResponseWriter, r *http.Request) {
	envID := r.PathValue("id")
	targetID := r.PathValue("targetID")
	if envID == "" || targetID == "" {
		jsonError(w, "envelope id and target id required", http.StatusBadRequest)
		return
	}

	var req struct {
		Error string `json:"error"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		req.Error = "unknown"
	}

	if err := a.db.FailTarget(envID, targetID, req.Error); err != nil {
		log.Printf("error failing target: %v", err)
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}

	jsonResp(w, http.StatusOK, map[string]string{"status": "failed"})
}

// handleUpdateEnvelopeStatus transitions the envelope status.
// POST /api/envelope/{id}/status  {"status": "dispatched", "error": "optional"}
func (a *API) handleUpdateEnvelopeStatus(w http.ResponseWriter, r *http.Request) {
	envID := r.PathValue("id")
	if envID == "" {
		jsonError(w, "id is required", http.StatusBadRequest)
		return
	}

	var req struct {
		Status string  `json:"status"`
		Error  *string `json:"error"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, "invalid request body", http.StatusBadRequest)
		return
	}

	validStatuses := map[string]bool{
		"pending": true, "dispatched": true, "processing": true,
		"delivered": true, "partial": true, "failed": true, "expired": true,
	}
	if !validStatuses[req.Status] {
		jsonError(w, "invalid status", http.StatusBadRequest)
		return
	}

	if err := a.db.UpdateEnvelopeStatus(envID, req.Status, req.Error); err != nil {
		log.Printf("error updating envelope status: %v", err)
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}

	jsonResp(w, http.StatusOK, map[string]string{"status": req.Status})
}

// handleCreateAnonEnvelope creates an envelope WITHOUT authentication.
// source_user_id stays NULL. The caller receives the envelope_id as a claim ticket.
// POST /api/envelope/anon
func (a *API) handleCreateAnonEnvelope(w http.ResponseWriter, r *http.Request) {
	var req struct {
		SourceType     string                 `json:"source_type"`
		SourceCallback *string                `json:"source_callback"`
		BatchID        *string                `json:"batch_id"`
		PieceHash      string                 `json:"piece_hash"`
		TTLMinutes     int                    `json:"ttl_minutes"`
		Targets        []db.CreateTargetInput `json:"targets"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if req.PieceHash == "" {
		jsonError(w, "piece_hash is required", http.StatusBadRequest)
		return
	}
	if len(req.Targets) == 0 {
		jsonError(w, "at least one target is required", http.StatusBadRequest)
		return
	}

	validSources := map[string]bool{
		"witheout": true, "api": true,
	}
	if !validSources[req.SourceType] {
		jsonError(w, "anon envelopes only allowed from witheout or api", http.StatusBadRequest)
		return
	}

	validTargets := map[string]bool{
		"horostracker": true, "googledrive": true, "webhook": true,
		"email": true, "s3": true, "ipfs": true,
	}
	for _, t := range req.Targets {
		if !validTargets[t.TargetType] {
			jsonError(w, "invalid target_type: "+t.TargetType, http.StatusBadRequest)
			return
		}
	}

	envelope, err := a.db.CreateEnvelope(db.CreateEnvelopeInput{
		BatchID:        req.BatchID,
		SourceType:     req.SourceType,
		SourceUserID:   nil, // anonymous — no user
		SourceNodeID:   nil, // no target node yet
		SourceCallback: req.SourceCallback,
		PieceHash:      req.PieceHash,
		TTLMinutes:     req.TTLMinutes,
		Targets:        req.Targets,
	})
	if err != nil {
		log.Printf("error creating anon envelope: %v", err)
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}

	jsonResp(w, http.StatusCreated, map[string]interface{}{
		"envelope_id": envelope.ID,
		"status":      envelope.Status,
		"expires_at":  envelope.ExpiresAt,
		"targets":     len(envelope.Targets),
	})
}

// handleGetEnvelopeStatus returns only the delivery status of an envelope.
// NO authentication required — knowing the envelope_id is the proof.
// Returns minimal data: status + delivery progress. No user info, no piece data.
// GET /api/envelope/{id}/status
func (a *API) handleGetEnvelopeStatus(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		jsonError(w, "id is required", http.StatusBadRequest)
		return
	}

	status, targetCount, deliveredCount, err := a.db.GetEnvelopeStatus(id)
	if err != nil {
		jsonError(w, "envelope not found", http.StatusNotFound)
		return
	}

	jsonResp(w, http.StatusOK, map[string]interface{}{
		"envelope_id":     id,
		"status":          status,
		"target_count":    targetCount,
		"delivered_count": deliveredCount,
	})
}

// handleClaimEnvelope assigns an unclaimed anonymous envelope to the authenticated user.
// The user must present the envelope_id (their claim ticket) + a valid JWT.
// POST /api/envelope/{id}/claim  {"node_id": "optional — where to attach"}
func (a *API) handleClaimEnvelope(w http.ResponseWriter, r *http.Request) {
	claims := a.auth.ExtractClaims(r)
	if claims == nil {
		jsonError(w, "authentication required", http.StatusUnauthorized)
		return
	}

	envID := r.PathValue("id")
	if envID == "" {
		jsonError(w, "id is required", http.StatusBadRequest)
		return
	}

	if err := a.db.ClaimEnvelope(envID, claims.UserID); err != nil {
		jsonError(w, err.Error(), http.StatusConflict)
		return
	}

	envelope, err := a.db.GetEnvelope(envID)
	if err != nil {
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}

	jsonResp(w, http.StatusOK, envelope)
}

// handleExpireEnvelopes is an admin/cron endpoint to expire overdue envelopes.
func (a *API) handleExpireEnvelopes(w http.ResponseWriter, r *http.Request) {
	count, err := a.db.ExpireEnvelopes()
	if err != nil {
		log.Printf("error expiring envelopes: %v", err)
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}

	jsonResp(w, http.StatusOK, map[string]interface{}{
		"expired_count": count,
	})
}
