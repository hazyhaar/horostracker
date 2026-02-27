package e2e

import (
	"net/http"
	"testing"
	"time"
)

func TestFiveW1H(t *testing.T) {
	h, dba := ensureHarness(t)
	ownerToken, _ := h.Register(t, "w5h1_owner", "w5h1owner1234")

	questionID := h.AskQuestion(t, ownerToken, "Question pour tester l'extraction 5W1H", nil)

	var assertResult struct {
		Nodes []map[string]interface{} `json:"nodes"`
	}
	h.JSON("POST", "/api/node/"+questionID+"/assertions", map[string]interface{}{
		"assertions": []string{"Assertion pour 5W1H"},
	}, ownerToken, &assertResult)
	assertionID := assertResult.Nodes[0]["id"].(string)

	// Create a source with text content (5W1H extraction is async)
	var source map[string]interface{}
	h.JSON("POST", "/api/node/"+assertionID+"/source", map[string]interface{}{
		"content_text": "Le 15 janvier 2024, le Ministère de la Santé a publié un rapport indiquant que les concentrations de bisphénol A dans l'eau potable de la région Île-de-France dépassent les seuils recommandés par l'OMS, en raison de l'utilisation de tuyaux en PVC contenant des plastifiants.",
		"title":        "Rapport Ministère de la Santé",
	}, ownerToken, &source)
	sourceID := source["id"].(string)

	t.Run("Get5W1HEndpoint", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		// Wait briefly for async extraction if LLM is configured
		if HasLLM() {
			time.Sleep(5 * time.Second)
		}

		var result struct {
			SourceID   string              `json:"source_id"`
			Dimensions map[string][]string `json:"dimensions"`
			Raw        []interface{}       `json:"raw"`
		}
		resp, err := h.JSON("GET", "/api/source/"+sourceID+"/5w1h", nil, "", &result)
		if err != nil {
			t.Fatalf("get 5w1h: %v", err)
		}
		RequireStatus(t, resp, http.StatusOK)

		if result.SourceID != sourceID {
			t.Errorf("source_id = %v, want %s", result.SourceID, sourceID)
		}

		// If LLM is configured, verify dimensions are populated
		if HasLLM() {
			if len(result.Dimensions) == 0 {
				t.Error("LLM configured but no 5W1H dimensions extracted")
			}
			validDims := map[string]bool{"who": true, "what": true, "when": true, "where": true, "why": true, "how": true}
			for dim := range result.Dimensions {
				if !validDims[dim] {
					t.Errorf("invalid dimension: %q", dim)
				}
			}
			if len(result.Dimensions) > 0 {
				t.Logf("5W1H extracted %d dimensions", len(result.Dimensions))
				for dim, entries := range result.Dimensions {
					t.Logf("  %s: %v", dim, entries)
				}
			}
		}
	})

	t.Run("5W1HEmptyForNewSource", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		// Create a source via URL (no content_text → no 5W1H extraction)
		var urlSource map[string]interface{}
		h.JSON("POST", "/api/node/"+assertionID+"/source", map[string]interface{}{
			"url":   "https://example.com/empty-5w1h",
			"title": "URL source sans texte",
		}, ownerToken, &urlSource)
		urlSourceID := urlSource["id"].(string)

		var result struct {
			Dimensions map[string][]string `json:"dimensions"`
		}
		resp, err := h.JSON("GET", "/api/source/"+urlSourceID+"/5w1h", nil, "", &result)
		if err != nil {
			t.Fatalf("get 5w1h: %v", err)
		}
		RequireStatus(t, resp, http.StatusOK)

		// URL-only sources have no text to extract from, so dimensions should be empty
		if len(result.Dimensions) != 0 {
			t.Errorf("expected empty dimensions for URL-only source, got %d", len(result.Dimensions))
		}
	})

	t.Run("5W1HNonexistentSource", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		var result struct {
			Dimensions map[string][]string `json:"dimensions"`
		}
		resp, err := h.JSON("GET", "/api/source/nonexistent999/5w1h", nil, "", &result)
		if err != nil {
			t.Fatalf("get 5w1h: %v", err)
		}
		// Should return 200 with empty dimensions, not an error
		RequireStatus(t, resp, http.StatusOK)
	})

	// --- DB assertions ---

	t.Run("DB_Source5W1H_Table_Exists", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		db, _ := dba.nodes()
		var count int
		err := db.QueryRow("SELECT COUNT(*) FROM source_5w1h WHERE 1=0").Scan(&count)
		if err != nil {
			t.Fatalf("source_5w1h table does not exist: %v", err)
		}
	})

	t.Run("DB_Source5W1H_Indexes_Exist", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		db, _ := dba.nodes()
		var count int
		err := db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type='index' AND name IN ('idx_source_5w1h_source', 'idx_source_5w1h_dim')").Scan(&count)
		if err != nil {
			t.Fatalf("querying indexes: %v", err)
		}
		if count < 2 {
			t.Errorf("expected 2 source_5w1h indexes, found %d", count)
		}
	})

	t.Run("DB_Source5W1H_Dimension_Check_Constraint", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		db, _ := dba.nodes()
		// Attempt to insert invalid dimension — should fail
		_, err := db.Exec("INSERT INTO source_5w1h (id, source_id, dimension, content) VALUES ('test_invalid', ?, 'invalid_dim', 'test')", sourceID)
		if err == nil {
			t.Error("inserting invalid dimension should fail (CHECK constraint)")
			db.Exec("DELETE FROM source_5w1h WHERE id = 'test_invalid'")
		}
	})
}
