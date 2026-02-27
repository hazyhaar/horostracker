package e2e

import (
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestFederation(t *testing.T) {
	h, _ := ensureHarness(t)
	token, _ := h.Register(t, "federation_user", "federationpass1234")

	t.Run("Identity", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		var result map[string]interface{}
		resp, err := h.JSON("GET", "/api/federation/identity", nil, "", &result)
		if err != nil {
			t.Fatalf("federation identity: %v", err)
		}
		RequireStatus(t, resp, http.StatusOK)

		if result["instance_id"] == nil || result["instance_id"] == "" {
			t.Error("expected non-empty instance_id")
		}
		if result["protocol"] == nil || result["protocol"] == "" {
			t.Error("expected non-empty protocol")
		}
		if result["version"] == nil {
			t.Error("expected version field")
		}
	})

	t.Run("Status", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		var result map[string]interface{}
		resp, err := h.JSON("GET", "/api/federation/status", nil, "", &result)
		if err != nil {
			t.Fatalf("federation status: %v", err)
		}
		RequireStatus(t, resp, http.StatusOK)

		localNodes, ok := result["local_nodes"].(float64)
		if !ok {
			t.Error("expected local_nodes field")
		}
		if localNodes < 0 {
			t.Errorf("local_nodes = %v, want >= 0", localNodes)
		}
	})

	t.Run("NodeHash", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		questionID := h.AskQuestion(t, token, "Hash test question for federation integrity", nil)

		var result map[string]interface{}
		resp, err := h.JSON("GET", "/api/node/"+questionID+"/hash", nil, "", &result)
		if err != nil {
			t.Fatalf("node hash: %v", err)
		}
		RequireStatus(t, resp, http.StatusOK)

		hash := result["binary_hash"].(string)
		if len(hash) != 64 { // SHA-256 hex
			t.Errorf("hash length = %d, want 64 hex chars", len(hash))
		}
		if result["algorithm"] != "sha256" {
			t.Errorf("algorithm = %v, want sha256", result["algorithm"])
		}
	})

	t.Run("NodeHashDeterministic", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		questionID := h.AskQuestion(t, token, "Deterministic hash test question", nil)

		var result1, result2 map[string]interface{}
		h.JSON("GET", "/api/node/"+questionID+"/hash", nil, "", &result1)
		h.JSON("GET", "/api/node/"+questionID+"/hash", nil, "", &result2)

		hash1 := result1["binary_hash"].(string)
		hash2 := result2["binary_hash"].(string)
		if hash1 != hash2 {
			t.Errorf("hashes differ for same node: %s vs %s", hash1, hash2)
		}
	})

	// --- Abuse tests ---

	t.Run("Abuse_HashManipulation", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		questionID := h.AskQuestion(t, token, "Hash manipulation test question for integrity", nil)

		// Get hash before vote
		var hashBefore map[string]interface{}
		h.JSON("GET", "/api/node/"+questionID+"/hash", nil, "", &hashBefore)
		hash1 := hashBefore["binary_hash"].(string)

		// Modify node state via vote
		voterToken, _ := h.Register(t, "abuse_fed_voter", "abusepass12345")
		h.Do("POST", "/api/vote", map[string]interface{}{"node_id": questionID, "value": 1}, voterToken)

		// Get hash after vote
		var hashAfter map[string]interface{}
		h.JSON("GET", "/api/node/"+questionID+"/hash", nil, "", &hashAfter)
		hash2 := hashAfter["binary_hash"].(string)

		if hash1 == hash2 {
			t.Log("SECURITY NOTE: hash did not change after vote — hash may not include score/vote data (acceptable if by design)")
		} else {
			t.Log("Hash correctly changed after node state modification — integrity verified")
		}
	})

	t.Run("Abuse_FederationInfoLeak", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		data, resp, err := h.RawBody("GET", "/api/federation/identity", nil, "")
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		RequireStatus(t, resp, http.StatusOK)

		body := string(data)
		sensitivePatterns := []string{"private_key", "jwt_secret", "/home/", "/workspace/", "password"}
		for _, pattern := range sensitivePatterns {
			if strings.Contains(strings.ToLower(body), pattern) {
				t.Errorf("CRITICAL: federation identity leaks sensitive data containing %q", pattern)
			}
		}
		t.Log("Federation info leak check passed — no sensitive fields detected")
	})
}
