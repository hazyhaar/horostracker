package api

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"

	"github.com/hazyhaar/horostracker/internal/config"
)

// RegisterFederationRoutes adds federation-related API endpoints.
func (a *API) RegisterFederationRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/federation/identity", a.handleFederationIdentity)
	mux.HandleFunc("GET /api/federation/status", a.handleFederationStatus)
	mux.HandleFunc("GET /api/node/{id}/hash", a.handleNodeHash)
}

// SetFederationConfig injects federation and instance config.
func (a *API) SetFederationConfig(fed config.FederationConfig, inst config.InstanceConfig) {
	a.fedConfig = &fed
	a.instConfig = &inst
}

func (a *API) handleFederationIdentity(w http.ResponseWriter, r *http.Request) {
	if a.instConfig == nil {
		jsonError(w, "instance not configured", http.StatusServiceUnavailable)
		return
	}

	resp := map[string]interface{}{
		"instance_id":   a.instConfig.ID,
		"instance_name": a.instConfig.Name,
		"federation":    a.fedConfig != nil && a.fedConfig.Enabled,
		"version":       "1.0",
		"protocol":      "horostracker-federation-v1",
	}

	if a.fedConfig != nil && a.fedConfig.Enabled {
		resp["instance_url"] = a.fedConfig.InstanceURL
		resp["signature_algorithm"] = a.fedConfig.SignatureAlgo
		resp["public_key_id"] = a.fedConfig.PublicKeyID
		resp["verify_signatures"] = a.fedConfig.VerifySignatures
	}

	jsonResp(w, http.StatusOK, resp)
}

func (a *API) handleFederationStatus(w http.ResponseWriter, r *http.Request) {
	enabled := a.fedConfig != nil && a.fedConfig.Enabled

	var peerCount int
	if a.fedConfig != nil {
		peerCount = len(a.fedConfig.PeerInstances)
	}

	// Count nodes by origin
	var localCount, remoteCount int
	a.db.QueryRow("SELECT COUNT(*) FROM nodes WHERE origin_instance = 'local'").Scan(&localCount)
	a.db.QueryRow("SELECT COUNT(*) FROM nodes WHERE origin_instance != 'local'").Scan(&remoteCount)

	jsonResp(w, http.StatusOK, map[string]interface{}{
		"federation_enabled": enabled,
		"peer_count":         peerCount,
		"local_nodes":        localCount,
		"remote_nodes":       remoteCount,
	})
}

// handleNodeHash returns the content-addressable hash for a node.
// This is used for federation integrity verification.
func (a *API) handleNodeHash(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		jsonError(w, "id is required", http.StatusBadRequest)
		return
	}

	node, err := a.db.GetNode(id)
	if err != nil {
		jsonError(w, "node not found", http.StatusNotFound)
		return
	}

	// Compute content-addressable hash: SHA-256(node_type + body + author_id + created_at)
	h := sha256.New()
	h.Write([]byte(node.NodeType))
	h.Write([]byte(node.Body))
	h.Write([]byte(node.AuthorID))
	h.Write([]byte(node.CreatedAt.UTC().String()))
	hash := hex.EncodeToString(h.Sum(nil))

	// Store if not already set
	if node.BinaryHash == "" {
		a.db.Exec("UPDATE nodes SET binary_hash = ? WHERE id = ? AND binary_hash = ''", hash, id)
	}

	jsonResp(w, http.StatusOK, map[string]interface{}{
		"node_id":     id,
		"binary_hash": hash,
		"algorithm":   "sha256",
		"origin":      node.OriginInstance,
		"signature":   node.Signature,
	})
}
