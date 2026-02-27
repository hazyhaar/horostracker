package e2e

import (
	"net/http"
	"testing"
	"time"
)

func TestSoftDelete(t *testing.T) {
	h, dba := ensureHarness(t)
	ownerToken, ownerID := h.Register(t, "del_owner", "delowner1234")
	otherToken, _ := h.Register(t, "del_other", "delother1234")
	opToken, _ := h.Register(t, "del_operator", "deloperator1234")

	// Promote del_operator to operator
	db, err := dba.nodes()
	if err != nil {
		t.Fatalf("opening nodes.db: %v", err)
	}
	_, err = db.Exec("UPDATE users SET role = 'operator' WHERE handle = 'del_operator'")
	if err != nil {
		t.Fatalf("promoting to operator: %v", err)
	}
	opToken, _ = h.Login(t, "del_operator", "deloperator1234")
	_ = ownerID

	t.Run("DeleteOwnNode", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		qID := h.AskQuestion(t, ownerToken, "Delete own node test question", nil)
		dba.AssertNodeExists(t, qID)

		resp, err := h.Do("DELETE", "/api/node/"+qID, nil, ownerToken)
		if err != nil {
			t.Fatalf("delete request: %v", err)
		}
		RequireStatus(t, resp, http.StatusOK)

		// Node should still exist in DB with deleted_at set
		var deletedAt interface{}
		db, _ := dba.nodes()
		err = db.QueryRow("SELECT deleted_at FROM nodes WHERE id = ?", qID).Scan(&deletedAt)
		if err != nil {
			t.Fatalf("querying deleted_at: %v", err)
		}
		if deletedAt == nil {
			t.Error("deleted_at should not be NULL after soft-delete")
		}
	})

	t.Run("DeletedNodeNotFoundViaAPI", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		qID := h.AskQuestion(t, ownerToken, "Deleted node invisible test", nil)
		h.Do("DELETE", "/api/node/"+qID, nil, ownerToken)

		// GET /api/node/{id} should return 404
		resp, _ := h.Do("GET", "/api/node/"+qID, nil, "")
		RequireStatus(t, resp, http.StatusNotFound)
	})

	t.Run("DeletedNodeExcludedFromTree", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		qID := h.AskQuestion(t, ownerToken, "Tree exclusion parent question", nil)
		childID := h.AnswerNode(t, ownerToken, qID, "Child that gets deleted", "claim")
		h.AnswerNode(t, ownerToken, qID, "Surviving child", "claim")

		// Delete the child
		h.Do("DELETE", "/api/node/"+childID, nil, ownerToken)

		// Fetch tree — deleted child should be absent
		var tree map[string]interface{}
		resp, err := h.JSON("GET", "/api/tree/"+qID+"?depth=50", nil, "", &tree)
		if err != nil {
			t.Fatalf("get tree: %v", err)
		}
		RequireStatus(t, resp, http.StatusOK)

		children, ok := tree["children"].([]interface{})
		if !ok {
			t.Fatal("tree has no children field")
		}
		for _, c := range children {
			child := c.(map[string]interface{})
			if child["id"] == childID {
				t.Error("deleted child should not appear in tree")
			}
		}
	})

	t.Run("DeletedQuestionExcludedFromFeed", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		qID := h.AskQuestion(t, ownerToken, "Feed exclusion question "+time.Now().Format(time.RFC3339Nano), nil)
		h.Do("DELETE", "/api/node/"+qID, nil, ownerToken)

		var questions []map[string]interface{}
		h.JSON("GET", "/api/questions?limit=100", nil, "", &questions)
		for _, q := range questions {
			if q["id"] == qID {
				t.Error("deleted question should not appear in feed")
			}
		}
	})

	t.Run("DeletedNodeExcludedFromSearch", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		uniqueBody := "Xylophonic quantum marsupial " + time.Now().Format(time.RFC3339Nano)
		qID := h.AskQuestion(t, ownerToken, uniqueBody, nil)
		h.Do("DELETE", "/api/node/"+qID, nil, ownerToken)

		var searchResult struct {
			Results []map[string]interface{} `json:"results"`
			Count   int                      `json:"count"`
		}
		h.JSON("POST", "/api/search", map[string]interface{}{
			"query": "Xylophonic quantum marsupial",
			"limit": 20,
		}, "", &searchResult)

		for _, r := range searchResult.Results {
			if r["id"] == qID {
				t.Error("deleted node should not appear in search results")
			}
		}
	})

	t.Run("DeleteBySlugReturns404AfterDelete", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		var result struct {
			Node map[string]interface{} `json:"node"`
		}
		h.JSON("POST", "/api/ask", map[string]interface{}{
			"body": "Slug delete test question for verification",
		}, ownerToken, &result)
		slug := result.Node["slug"].(string)
		nodeID := result.Node["id"].(string)

		h.Do("DELETE", "/api/node/"+nodeID, nil, ownerToken)

		resp, _ := h.Do("GET", "/api/q/"+slug, nil, "")
		RequireStatus(t, resp, http.StatusNotFound)
	})

	t.Run("ForbiddenForNonOwner", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		qID := h.AskQuestion(t, ownerToken, "Non-owner delete attempt", nil)
		resp, _ := h.Do("DELETE", "/api/node/"+qID, nil, otherToken)
		RequireStatus(t, resp, http.StatusForbidden)
	})

	t.Run("OperatorCanDelete", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		qID := h.AskQuestion(t, ownerToken, "Operator delete test question", nil)
		resp, _ := h.Do("DELETE", "/api/node/"+qID, nil, opToken)
		RequireStatus(t, resp, http.StatusOK)

		// Verify via API
		resp2, _ := h.Do("GET", "/api/node/"+qID, nil, "")
		RequireStatus(t, resp2, http.StatusNotFound)
	})

	t.Run("UnauthenticatedDelete", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		qID := h.AskQuestion(t, ownerToken, "Unauthenticated delete attempt", nil)
		resp, _ := h.Do("DELETE", "/api/node/"+qID, nil, "")
		RequireStatus(t, resp, http.StatusUnauthorized)
	})

	t.Run("DeleteNonexistentNode", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		resp, _ := h.Do("DELETE", "/api/node/nonexistent99999", nil, ownerToken)
		RequireStatus(t, resp, http.StatusNotFound)
	})

	t.Run("DoubleDeleteIdempotent", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		qID := h.AskQuestion(t, ownerToken, "Double delete idempotent test", nil)
		resp1, _ := h.Do("DELETE", "/api/node/"+qID, nil, ownerToken)
		RequireStatus(t, resp1, http.StatusOK)

		// Second delete — node is already deleted, GetNode returns not found
		resp2, _ := h.Do("DELETE", "/api/node/"+qID, nil, ownerToken)
		RequireStatus(t, resp2, http.StatusNotFound)
	})

	// --- DB assertions ---

	t.Run("DB_DeletedAt_Column_Exists", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		db, err := dba.nodes()
		if err != nil {
			t.Fatalf("opening nodes.db: %v", err)
		}
		// Query pragma to verify column exists
		rows, err := db.Query("PRAGMA table_info(nodes)")
		if err != nil {
			t.Fatalf("pragma query: %v", err)
		}
		defer rows.Close()

		found := false
		for rows.Next() {
			var cid int
			var name, ctype string
			var notnull int
			var dflt interface{}
			var pk int
			if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
				t.Fatalf("scanning pragma: %v", err)
			}
			if name == "deleted_at" {
				found = true
			}
		}
		if !found {
			t.Error("nodes table missing deleted_at column")
		}
	})
}
