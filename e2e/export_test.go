package e2e

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestExport(t *testing.T) {
	h, _ := ensureHarness(t)
	token, _ := h.Register(t, "export_user", "exportpass1234")

	// Create a tree for export
	questionID := h.AskQuestion(t, token, "Export test question: What is the meaning of open source?", []string{"opensource", "philosophy"})
	h.AnswerNode(t, token, questionID, "Open source means freedom to inspect and modify code", "claim")
	h.AnswerNode(t, token, questionID, "This ignores the economic implications", "claim")
	h.AnswerNode(t, token, questionID, "Studies show open source has higher code quality on average", "piece")

	t.Run("ExportTree", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		data, resp, err := h.RawBody("GET", "/api/export/tree/"+questionID, nil, "")
		if err != nil {
			t.Fatalf("export tree: %v", err)
		}
		RequireStatus(t, resp, http.StatusOK)

		// Verify content type
		ct := resp.Header.Get("Content-Type")
		if !strings.Contains(ct, "application/jsonl") {
			t.Errorf("content-type = %s, want application/jsonl", ct)
		}

		// Parse as single JSON object
		var export map[string]interface{}
		if err := json.Unmarshal(data, &export); err != nil {
			t.Fatalf("parsing JSONL: %v", err)
		}

		// Check metadata.total_nodes
		metadata := export["metadata"].(map[string]interface{})
		totalNodes := int(metadata["total_nodes"].(float64))
		if totalNodes < 4 {
			t.Errorf("total_nodes = %d, want >= 4", totalNodes)
		}

		// Check tree.author_id starts with "anon_"
		tree := export["tree"].(map[string]interface{})
		authorID := tree["author_id"].(string)
		if !strings.HasPrefix(authorID, "anon_") {
			t.Errorf("author_id = %s, want anon_ prefix", authorID)
		}
	})

	t.Run("ExportAnonymization", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		// Export twice — same author should get same anon_id within export, different between exports
		data1, _, _ := h.RawBody("GET", "/api/export/tree/"+questionID, nil, "")
		data2, _, _ := h.RawBody("GET", "/api/export/tree/"+questionID, nil, "")

		var export1, export2 map[string]interface{}
		json.Unmarshal(data1, &export1)
		json.Unmarshal(data2, &export2)

		tree1 := export1["tree"].(map[string]interface{})
		tree2 := export2["tree"].(map[string]interface{})

		anon1 := tree1["author_id"].(string)
		anon2 := tree2["author_id"].(string)

		if !strings.HasPrefix(anon1, "anon_") {
			t.Errorf("first export author_id = %s, want anon_ prefix", anon1)
		}
		if !strings.HasPrefix(anon2, "anon_") {
			t.Errorf("second export author_id = %s, want anon_ prefix", anon2)
		}

		// Different exports should have different anon IDs (different salt)
		if anon1 == anon2 {
			t.Error("expected different anon IDs between exports (different salt)")
		}

		// Within same export, same author should have same anon ID
		children1 := tree1["children"].([]interface{})
		if len(children1) >= 2 {
			child1Author := children1[0].(map[string]interface{})["author_id"].(string)
			child2Author := children1[1].(map[string]interface{})["author_id"].(string)
			// Both created by same user
			if child1Author != child2Author {
				t.Error("same author should have same anon_id within one export")
			}
		}
	})

	t.Run("ExportGarbageSet", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		data, resp, err := h.RawBody("GET", "/api/export/garbage/"+questionID, nil, "")
		if err != nil {
			t.Fatalf("export garbage: %v", err)
		}
		RequireStatus(t, resp, http.StatusOK)

		var cgs map[string]interface{}
		if err := json.Unmarshal(data, &cgs); err != nil {
			t.Fatalf("parsing garbage set: %v", err)
		}

		if cgs["original_claim"] == nil || cgs["original_claim"] == "" {
			t.Error("expected non-empty original_claim")
		}

		metadata := cgs["metadata"].(map[string]interface{})
		if metadata["objection_count"] == nil {
			t.Error("expected objection_count in metadata")
		}
		if metadata["evidence_count"] == nil {
			t.Error("expected evidence_count in metadata")
		}
	})

	t.Run("ExportAll", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		data, resp, err := h.RawBody("GET", "/api/export/all", nil, "")
		if err != nil {
			t.Fatalf("export all: %v", err)
		}
		RequireStatus(t, resp, http.StatusOK)

		// Each line should be valid JSON
		lines := strings.Split(strings.TrimSpace(string(data)), "\n")
		if len(lines) == 0 {
			t.Error("expected at least 1 line in export all")
		}

		for i, line := range lines {
			if line == "" {
				continue
			}
			var obj map[string]interface{}
			if err := json.Unmarshal([]byte(line), &obj); err != nil {
				t.Errorf("line %d is not valid JSON: %v", i, err)
			}
		}
	})

	// --- Abuse tests ---

	t.Run("Abuse_ExportNonexistentTree", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		resp, err := h.Do("GET", "/api/export/tree/nonexistent_tree_id_xyz", nil, "")
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		// Must not crash — 404 or empty JSON are both acceptable
		if resp.StatusCode == http.StatusInternalServerError {
			t.Error("SECURITY NOTE: export of nonexistent tree caused 500")
		}
		t.Logf("Export nonexistent tree: status %d (server did not crash)", resp.StatusCode)
	})

	t.Run("Abuse_ExportPathTraversal", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		resp, err := h.Do("GET", "/api/export/tree/../../etc/passwd", nil, "")
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		// Must not return filesystem contents
		if resp.StatusCode == http.StatusOK {
			data, _, _ := h.RawBody("GET", "/api/export/tree/../../etc/passwd", nil, "")
			body := string(data)
			if strings.Contains(body, "root:") {
				t.Error("CRITICAL: path traversal exposed /etc/passwd")
			}
		}
		t.Logf("Path traversal export: status %d (server did not crash)", resp.StatusCode)
	})

	t.Run("Abuse_ExportNoPasswordLeak", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		data, resp, err := h.RawBody("GET", "/api/export/all", nil, "")
		if err != nil {
			t.Fatalf("export all: %v", err)
		}
		RequireStatus(t, resp, http.StatusOK)

		body := string(data)
		if strings.Contains(body, "password_hash") {
			t.Error("CRITICAL: export/all leaks password_hash field")
		}
		if strings.Contains(body, "jwt_secret") {
			t.Error("CRITICAL: export/all leaks jwt_secret")
		}
		t.Log("Export password leak check passed — no sensitive fields detected in export/all")
	})
}
