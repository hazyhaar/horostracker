// CLAUDE:SUMMARY Deduplication API — check content similarity (hash + fuzzy), list and inspect duplicate clusters
package api

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"unicode"

	"github.com/hazyhaar/horostracker/internal/db"
)

func (a *API) RegisterDedupRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/dedup/check", a.handleDedupCheck)
	mux.HandleFunc("GET /api/dedup/clusters", a.handleDedupClusters)
	mux.HandleFunc("GET /api/dedup/cluster/{id}", a.handleDedupCluster)
}

func (a *API) handleDedupCheck(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Body       string  `json:"body"`
		Threshold  float64 `json:"threshold"`
		Method     string  `json:"method"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if req.Body == "" {
		jsonError(w, "body is required", http.StatusBadRequest)
		return
	}
	if req.Threshold <= 0 {
		req.Threshold = 0.8
	}
	if req.Method == "" {
		req.Method = "fuzzy"
	}

	normalized := normalizeText(req.Body)
	bodyHash := fmt.Sprintf("%x", sha256.Sum256([]byte(normalized)))

	// Level 1: Exact match (hash)
	rows, err := a.db.Query(`SELECT id, body FROM nodes WHERE node_type = 'question'`)
	if err != nil {
		jsonError(w, "query failed", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	type match struct {
		NodeID     string  `json:"node_id"`
		Similarity float64 `json:"similarity"`
		Method     string  `json:"method"`
	}

	var matches []match
	for rows.Next() {
		var nodeID, nodeBody string
		if rows.Scan(&nodeID, &nodeBody) != nil {
			continue
		}

		existingNorm := normalizeText(nodeBody)
		existingHash := fmt.Sprintf("%x", sha256.Sum256([]byte(existingNorm)))

		// Exact
		if bodyHash == existingHash {
			matches = append(matches, match{NodeID: nodeID, Similarity: 1.0, Method: "exact"})
			continue
		}

		// Fuzzy (trigram Jaccard)
		if req.Method == "fuzzy" || req.Method == "all" {
			sim := trigramSimilarity(normalized, existingNorm)
			if sim >= req.Threshold {
				matches = append(matches, match{NodeID: nodeID, Similarity: sim, Method: "fuzzy"})
			}
		}
	}

	// Check if there's an existing cluster for exact matches
	var clusterID string
	if len(matches) > 0 {
		_ = a.db.QueryRow(`SELECT cluster_id FROM dedup_members WHERE node_id = ? LIMIT 1`, matches[0].NodeID).Scan(&clusterID)
	}

	jsonResp(w, http.StatusOK, map[string]interface{}{
		"matches":    matches,
		"count":      len(matches),
		"cluster_id": clusterID,
		"body_hash":  bodyHash,
	})
}

func (a *API) handleDedupClusters(w http.ResponseWriter, r *http.Request) {
	rows, err := a.db.Query(`
		SELECT dc.id, dc.canonical_id, dc.method, dc.created_at,
			(SELECT COUNT(*) FROM dedup_members dm WHERE dm.cluster_id = dc.id) as member_count
		FROM dedup_clusters dc ORDER BY dc.created_at DESC LIMIT 100`)
	if err != nil {
		jsonError(w, "query failed", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	type cluster struct {
		ID          string `json:"id"`
		CanonicalID string `json:"canonical_id"`
		Method      string `json:"method"`
		CreatedAt   string `json:"created_at"`
		MemberCount int    `json:"member_count"`
	}

	var clusters []cluster
	for rows.Next() {
		var c cluster
		if rows.Scan(&c.ID, &c.CanonicalID, &c.Method, &c.CreatedAt, &c.MemberCount) == nil {
			clusters = append(clusters, c)
		}
	}

	jsonResp(w, http.StatusOK, map[string]interface{}{"clusters": clusters, "count": len(clusters)})
}

func (a *API) handleDedupCluster(w http.ResponseWriter, r *http.Request) {
	clusterID := r.PathValue("id")

	var canonicalID, method, createdAt string
	if err := a.db.QueryRow(`SELECT canonical_id, method, created_at FROM dedup_clusters WHERE id = ?`, clusterID).
		Scan(&canonicalID, &method, &createdAt); err != nil {
		jsonError(w, "cluster not found", http.StatusNotFound)
		return
	}

	rows, err := a.db.Query(`SELECT dm.node_id, dm.similarity, n.body, n.created_at
		FROM dedup_members dm JOIN nodes n ON n.id = dm.node_id
		WHERE dm.cluster_id = ? ORDER BY dm.similarity DESC`, clusterID)
	if err != nil {
		jsonError(w, "query failed", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	type member struct {
		NodeID     string  `json:"node_id"`
		Similarity float64 `json:"similarity"`
		Body       string  `json:"body"`
		CreatedAt  string  `json:"created_at"`
	}

	var members []member
	for rows.Next() {
		var m member
		if rows.Scan(&m.NodeID, &m.Similarity, &m.Body, &m.CreatedAt) == nil {
			members = append(members, m)
		}
	}

	jsonResp(w, http.StatusOK, map[string]interface{}{
		"id": clusterID, "canonical_id": canonicalID, "method": method,
		"created_at": createdAt, "members": members, "count": len(members),
	})
}

// --- Pure Go dedup helpers ---

func normalizeText(s string) string {
	s = strings.ToLower(s)
	s = strings.TrimSpace(s)
	// Collapse whitespace
	var b strings.Builder
	prevSpace := false
	for _, r := range s {
		if unicode.IsSpace(r) {
			if !prevSpace {
				b.WriteRune(' ')
			}
			prevSpace = true
		} else {
			b.WriteRune(r)
			prevSpace = false
		}
	}
	return b.String()
}

func trigrams(s string) map[string]bool {
	set := make(map[string]bool)
	runes := []rune(s)
	for i := 0; i+3 <= len(runes); i++ {
		set[string(runes[i:i+3])] = true
	}
	return set
}

func trigramSimilarity(a, b string) float64 {
	ta := trigrams(a)
	tb := trigrams(b)

	if len(ta) == 0 && len(tb) == 0 {
		return 1.0
	}

	intersection := 0
	for k := range ta {
		if tb[k] {
			intersection++
		}
	}

	union := len(ta) + len(tb) - intersection
	if union == 0 {
		return 0
	}
	return float64(intersection) / float64(union)
}

// createDedupCluster creates a dedup cluster for a set of nodes.
// nolint:unused // planned for dedup v2
func (a *API) createDedupCluster(canonicalID, method string, members []struct {
	nodeID     string
	similarity float64
}) string {
	clusterID := db.NewID()
	_, _ = a.db.Exec(`INSERT INTO dedup_clusters (id, canonical_id, method) VALUES (?, ?, ?)`, clusterID, canonicalID, method)

	for _, m := range members {
		_, _ = a.db.Exec(`INSERT OR IGNORE INTO dedup_members (cluster_id, node_id, similarity) VALUES (?, ?, ?)`,
			clusterID, m.nodeID, m.similarity)
	}

	return clusterID
}
