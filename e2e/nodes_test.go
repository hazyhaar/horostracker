package e2e

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestNodes(t *testing.T) {
	h, dba := ensureHarness(t)
	token, _ := h.Register(t, "nodes_user", "nodespass1234")

	t.Run("CreateQuestion", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		var result struct {
			Node    map[string]interface{} `json:"node"`
			Similar []interface{}          `json:"similar"`
		}
		resp, err := h.JSON("POST", "/api/ask", map[string]interface{}{
			"body": QuestionSimple,
			"tags": TagsAI,
		}, token, &result)
		if err != nil {
			t.Fatalf("ask: %v", err)
		}
		RequireStatus(t, resp, http.StatusCreated)

		nodeID := result.Node["id"].(string)
		if nodeID == "" {
			t.Fatal("expected non-empty node ID")
		}
		if result.Node["node_type"] != "claim" {
			t.Errorf("node_type = %v, want claim", result.Node["node_type"])
		}
		if result.Node["slug"] == nil || result.Node["slug"] == "" {
			t.Error("expected non-empty slug for question")
		}

		// Direct DB verification
		dba.AssertNodeExists(t, nodeID)
		dba.AssertNodeField(t, nodeID, "node_type", "claim")
		dba.AssertNodeField(t, nodeID, "depth", int64(0))
	})

	t.Run("CreateAnswer", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		questionID := h.AskQuestion(t, token, "Parent question for answer test", nil)
		answerID := h.AnswerNode(t, token, questionID, AnswerSimple, "claim")

		dba.AssertNodeExists(t, answerID)
		dba.AssertNodeField(t, answerID, "node_type", "claim")
		dba.AssertNodeField(t, answerID, "depth", int64(1))

		// Verify parent child_count incremented
		dba.AssertNodeFieldGTE(t, questionID, "child_count", 1)
	})

	t.Run("AllValidNodeTypes", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		questionID := h.AskQuestion(t, token, "Node types validation question", nil)
		validTypes := []string{"claim", "piece"}

		for _, nt := range validTypes {
			childID := h.AnswerNode(t, token, questionID, "Testing "+nt+" type", nt)
			dba.AssertNodeField(t, childID, "node_type", nt)
		}
	})

	t.Run("InvalidNodeType", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		questionID := h.AskQuestion(t, token, "Invalid type test question", nil)
		resp, _ := h.Do("POST", "/api/answer", map[string]interface{}{
			"parent_id": questionID,
			"body":      "Test body",
			"node_type": "invented_type",
		}, token)
		RequireStatus(t, resp, http.StatusBadRequest)
	})

	t.Run("ParentNotFound", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		resp, _ := h.Do("POST", "/api/answer", map[string]interface{}{
			"parent_id": "nonexistent123",
			"body":      "Orphan answer",
			"node_type": "claim",
		}, token)
		RequireStatus(t, resp, http.StatusInternalServerError)
	})

	t.Run("GetNode", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		questionID := h.AskQuestion(t, token, "GetNode test question", nil)
		node := h.GetNode(t, questionID)
		if node["id"] != questionID {
			t.Errorf("id = %v, want %s", node["id"], questionID)
		}
		if node["body"] != "GetNode test question" {
			t.Errorf("body mismatch")
		}
	})

	t.Run("GetTree", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		questionID := h.AskQuestion(t, token, "GetTree test question", nil)
		h.AnswerNode(t, token, questionID, "Answer 1", "claim")
		h.AnswerNode(t, token, questionID, "Answer 2", "claim")

		var tree map[string]interface{}
		resp, err := h.JSON("GET", "/api/tree/"+questionID, nil, "", &tree)
		if err != nil {
			t.Fatalf("get tree: %v", err)
		}
		RequireStatus(t, resp, http.StatusOK)

		children, ok := tree["children"].([]interface{})
		if !ok || len(children) < 2 {
			t.Errorf("expected at least 2 children, got %v", tree["children"])
		}
	})

	t.Run("GetTreeWithDepthLimit", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		questionID := h.AskQuestion(t, token, "Depth limit question", nil)
		child1 := h.AnswerNode(t, token, questionID, "Level 1", "claim")
		h.AnswerNode(t, token, child1, "Level 2", "claim")

		var tree map[string]interface{}
		resp, err := h.JSON("GET", "/api/tree/"+questionID+"?depth=1", nil, "", &tree)
		if err != nil {
			t.Fatalf("get tree: %v", err)
		}
		RequireStatus(t, resp, http.StatusOK)

		children := tree["children"].([]interface{})
		if len(children) > 0 {
			child := children[0].(map[string]interface{})
			grandChildren, _ := child["children"].([]interface{})
			if len(grandChildren) > 0 {
				t.Error("depth=1 should not include grandchildren")
			}
		}
	})

	t.Run("SlugLookup", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		var result struct {
			Node map[string]interface{} `json:"node"`
		}
		h.JSON("POST", "/api/ask", map[string]interface{}{
			"body": "Slug lookup verification question",
		}, token, &result)

		slug := result.Node["slug"].(string)
		var found map[string]interface{}
		resp, err := h.JSON("GET", "/api/q/"+slug, nil, "", &found)
		if err != nil {
			t.Fatalf("slug lookup: %v", err)
		}
		RequireStatus(t, resp, http.StatusOK)
		if found["id"] != result.Node["id"] {
			t.Errorf("slug lookup returned wrong node: %v vs %v", found["id"], result.Node["id"])
		}
	})

	t.Run("ViewCountAsync", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		questionID := h.AskQuestion(t, token, "View count test question", nil)

		// Fetch multiple times to trigger view count
		for i := 0; i < 3; i++ {
			h.GetNode(t, questionID)
		}

		// Wait for async updates to settle
		time.Sleep(1 * time.Second)

		// view_count should be >= 1 (async, exact count depends on timing)
		dba.AssertNodeFieldGTE(t, questionID, "view_count", 1)
	})

	// --- Abuse tests ---

	t.Run("Abuse_XSSInBody", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		var result struct {
			Node map[string]interface{} `json:"node"`
		}
		resp, err := h.JSON("POST", "/api/ask", map[string]interface{}{
			"body": XSSMulti,
		}, token, &result)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		if resp.StatusCode == http.StatusCreated {
			// XSS was accepted — verify literal storage (no server-side execution)
			nodeID := result.Node["id"].(string)
			var storedBody string
			dba.openAndQuery(t, fmt.Sprintf("SELECT body FROM nodes WHERE id = '%s'", nodeID), &storedBody)
			if !strings.Contains(storedBody, "<script>") {
				t.Error("SECURITY NOTE: XSS payload was altered during storage — expected literal storage")
			}
			t.Log("SECURITY NOTE: XSS body accepted and stored literally — sanitization should happen at render time")
		} else {
			t.Logf("XSS body rejected with status %d", resp.StatusCode)
		}
	})

	t.Run("Abuse_SQLiInBody", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		resp, err := h.Do("POST", "/api/ask", map[string]interface{}{
			"body": SQLiUnion,
		}, token)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		// Must not crash or expose SQL errors
		if resp.StatusCode == http.StatusInternalServerError {
			t.Error("SECURITY NOTE: SQLi in body caused 500 — possible unhandled SQL error")
		}
		t.Logf("SQLi in body: status %d (server did not crash)", resp.StatusCode)
	})

	t.Run("Abuse_HugeBody1MB", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		resp, err := h.Do("POST", "/api/ask", map[string]interface{}{
			"body": Body1MB,
		}, token)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		RequireStatus(t, resp, http.StatusRequestEntityTooLarge)
		t.Log("1MB body correctly rejected with 413")
	})

	t.Run("Abuse_NullByteInBody", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		resp, err := h.Do("POST", "/api/ask", map[string]interface{}{
			"body": NullByte,
		}, token)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		// Must not crash or corrupt data
		if resp.StatusCode == http.StatusInternalServerError {
			t.Error("SECURITY NOTE: null byte in body caused 500")
		}
		t.Logf("Null byte in body: status %d (server did not crash)", resp.StatusCode)
	})

	t.Run("Abuse_MetadataInjection", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		resp, err := h.Do("POST", "/api/ask", map[string]interface{}{
			"body":     "Metadata injection test question",
			"metadata": MetadataXSS,
		}, token)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		if resp.StatusCode == http.StatusCreated {
			t.Log("SECURITY NOTE: XSS metadata accepted — verify literal storage at render time")
		}
		t.Logf("Metadata injection: status %d (server did not crash)", resp.StatusCode)
	})

	t.Run("Abuse_TagXSSInjection", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		resp, err := h.Do("POST", "/api/ask", map[string]interface{}{
			"body": "Tag XSS injection test question",
			"tags": []string{TagXSS},
		}, token)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		if resp.StatusCode == http.StatusCreated {
			t.Log("SECURITY NOTE: XSS tag accepted — verify literal storage at render time")
		}
		t.Logf("Tag XSS injection: status %d (server did not crash)", resp.StatusCode)
	})

	t.Run("Abuse_DepthBombQuery", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		questionID := h.AskQuestion(t, token, "Depth bomb query test question", nil)
		h.AnswerNode(t, token, questionID, "Level 1 child", "claim")

		resp, err := h.Do("GET", fmt.Sprintf("/api/tree/%s?depth=%d", questionID, DepthBomb), nil, "")
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		elapsed := time.Since(start)
		if elapsed > 10*time.Second {
			t.Errorf("depth=999999 query took %v — possible stack overflow or infinite recursion", elapsed)
		}
		t.Logf("Depth bomb query: status %d in %v (server did not crash)", resp.StatusCode, elapsed)
	})
}
