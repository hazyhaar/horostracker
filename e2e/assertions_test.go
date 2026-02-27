package e2e

import (
	"net/http"
	"testing"
	"time"
)

func TestAssertions(t *testing.T) {
	h, dba := ensureHarness(t)
	ownerToken, _ := h.Register(t, "assert_owner", "assertowner1234")
	otherToken, _ := h.Register(t, "assert_other", "assertother1234")

	questionID := h.AskQuestion(t, ownerToken, "Mon bailleur peut-il augmenter le loyer de 40% en zone tendue alors que le bail est encore en cours ?", []string{"droit", "immobilier"})

	t.Run("CreateAssertionsManually", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		var result struct {
			Nodes []map[string]interface{} `json:"nodes"`
			Count int                      `json:"count"`
		}
		resp, err := h.JSON("POST", "/api/node/"+questionID+"/assertions", map[string]interface{}{
			"assertions": []string{
				"Le bailleur est soumis à l'encadrement des loyers en zone tendue",
				"Une augmentation de loyer en cours de bail est limitée à l'IRL",
				"Une augmentation de 40% dépasse le plafond légal de révision annuelle",
			},
		}, ownerToken, &result)
		if err != nil {
			t.Fatalf("create assertions: %v", err)
		}
		RequireStatus(t, resp, http.StatusCreated)

		if result.Count != 3 {
			t.Errorf("count = %d, want 3", result.Count)
		}
		for _, n := range result.Nodes {
			if n["node_type"] != "claim" {
				t.Errorf("node_type = %v, want claim", n["node_type"])
			}
			if n["parent_id"] != nil {
				t.Error("assertion should have nil parent_id (root node)")
			}
		}
	})

	t.Run("GetAssertionsForQuestion", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		var result struct {
			Assertions []map[string]interface{} `json:"assertions"`
			Count      int                      `json:"count"`
		}
		resp, err := h.JSON("GET", "/api/node/"+questionID+"/assertions", nil, "", &result)
		if err != nil {
			t.Fatalf("get assertions: %v", err)
		}
		RequireStatus(t, resp, http.StatusOK)

		if result.Count < 3 {
			t.Errorf("count = %d, want >= 3", result.Count)
		}
	})

	t.Run("AssertionNodeIsAutonomousTree", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		// Create an assertion and verify it's a root node
		var result struct {
			Nodes []map[string]interface{} `json:"nodes"`
		}
		h.JSON("POST", "/api/node/"+questionID+"/assertions", map[string]interface{}{
			"assertions": []string{"Assertion autonome pour vérification"},
		}, ownerToken, &result)

		assertionID := result.Nodes[0]["id"].(string)

		// Get tree for the assertion — should work as its own root
		var tree map[string]interface{}
		resp, err := h.JSON("GET", "/api/tree/"+assertionID+"?depth=10", nil, "", &tree)
		if err != nil {
			t.Fatalf("get assertion tree: %v", err)
		}
		RequireStatus(t, resp, http.StatusOK)

		if tree["id"] != assertionID {
			t.Errorf("tree root id = %v, want %s", tree["id"], assertionID)
		}
		if tree["node_type"] != "claim" {
			t.Errorf("tree root type = %v, want claim", tree["node_type"])
		}
	})

	t.Run("AssertionCanReceiveReplies", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		// Create assertion
		var result struct {
			Nodes []map[string]interface{} `json:"nodes"`
		}
		h.JSON("POST", "/api/node/"+questionID+"/assertions", map[string]interface{}{
			"assertions": []string{"Assertion qui reçoit des réponses"},
		}, ownerToken, &result)
		assertionID := result.Nodes[0]["id"].(string)

		// Reply with piece (factual material)
		pieceID := h.AnswerNode(t, ownerToken, assertionID, "Preuve soutenant l'assertion", "piece")
		dba.AssertNodeField(t, pieceID, "node_type", "piece")
		dba.AssertNodeField(t, pieceID, "depth", int64(1))
	})

	t.Run("EmptyAssertionsList", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		resp, _ := h.Do("POST", "/api/node/"+questionID+"/assertions", map[string]interface{}{
			"assertions": []string{},
		}, ownerToken)
		RequireStatus(t, resp, http.StatusBadRequest)
	})

	t.Run("OnlyClaimsCanBeDecomposed", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		pieceID := h.AnswerNode(t, ownerToken, questionID, "Piece to decompose test", "piece")
		resp, _ := h.Do("POST", "/api/node/"+pieceID+"/decompose", nil, ownerToken)
		RequireStatus(t, resp, http.StatusBadRequest)
	})

	t.Run("OnlyClaimsCanHaveAssertions", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		pieceID := h.AnswerNode(t, ownerToken, questionID, "Piece for assertion creation", "piece")
		resp, _ := h.Do("POST", "/api/node/"+pieceID+"/assertions", map[string]interface{}{
			"assertions": []string{"Test assertion"},
		}, ownerToken)
		RequireStatus(t, resp, http.StatusBadRequest)
	})

	t.Run("ForbiddenForNonOwner", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		resp, _ := h.Do("POST", "/api/node/"+questionID+"/assertions", map[string]interface{}{
			"assertions": []string{"Assertion par un non-auteur"},
		}, otherToken)
		RequireStatus(t, resp, http.StatusForbidden)
	})

	t.Run("UnauthenticatedDecompose", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		resp, _ := h.Do("POST", "/api/node/"+questionID+"/decompose", nil, "")
		RequireStatus(t, resp, http.StatusUnauthorized)
	})

	t.Run("UnauthenticatedCreateAssertions", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		resp, _ := h.Do("POST", "/api/node/"+questionID+"/assertions", map[string]interface{}{
			"assertions": []string{"Sans auth"},
		}, "")
		RequireStatus(t, resp, http.StatusUnauthorized)
	})

	t.Run("GetAssertions_EmptyForNewQuestion", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		newQ := h.AskQuestion(t, ownerToken, "Question sans assertions encore", nil)
		var result struct {
			Assertions []interface{} `json:"assertions"`
			Count      int           `json:"count"`
		}
		resp, err := h.JSON("GET", "/api/node/"+newQ+"/assertions", nil, "", &result)
		if err != nil {
			t.Fatalf("get assertions: %v", err)
		}
		RequireStatus(t, resp, http.StatusOK)
		if result.Count != 0 {
			t.Errorf("count = %d, want 0", result.Count)
		}
	})

	t.Run("DeletedAssertionExcluded", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		newQ := h.AskQuestion(t, ownerToken, "Question avec assertion à supprimer", nil)

		var result struct {
			Nodes []map[string]interface{} `json:"nodes"`
		}
		h.JSON("POST", "/api/node/"+newQ+"/assertions", map[string]interface{}{
			"assertions": []string{"Assertion supprimable", "Assertion survivante"},
		}, ownerToken, &result)

		deletedID := result.Nodes[0]["id"].(string)
		h.Do("DELETE", "/api/node/"+deletedID, nil, ownerToken)

		var getResult struct {
			Assertions []map[string]interface{} `json:"assertions"`
			Count      int                      `json:"count"`
		}
		h.JSON("GET", "/api/node/"+newQ+"/assertions", nil, "", &getResult)
		if getResult.Count != 1 {
			t.Errorf("count = %d, want 1 (one should be deleted)", getResult.Count)
		}
	})

	// --- DB assertions ---

	t.Run("DB_DecomposedFrom_Column_Exists", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		db, _ := dba.nodes()
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
			if name == "decomposed_from" {
				found = true
			}
		}
		if !found {
			t.Error("nodes table missing decomposed_from column")
		}
	})

	t.Run("DB_ClaimType_Valid", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		count := dba.QueryScalarInt(t, "SELECT COUNT(*) FROM nodes WHERE node_type = 'claim'")
		if count == 0 {
			t.Error("no claim nodes found in DB")
		}
	})

	t.Run("DB_DecomposedFrom_Links_Correctly", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		count := dba.QueryScalarInt(t, "SELECT COUNT(*) FROM nodes WHERE decomposed_from = ? AND node_type = 'claim' AND deleted_at IS NULL", questionID)
		if count < 3 {
			t.Errorf("expected >= 3 claims linked to parent, got %d", count)
		}
	})

	// --- LLM decompose test (conditional) ---

	t.Run("DecomposeWithLLM", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		if !HasLLM() {
			t.Skip("no LLM API key configured")
		}

		var result struct {
			Assertions []string `json:"assertions"`
		}
		resp, err := h.JSON("POST", "/api/node/"+questionID+"/decompose", nil, ownerToken, &result)
		if err != nil {
			t.Fatalf("decompose: %v", err)
		}
		RequireStatus(t, resp, http.StatusOK)

		if len(result.Assertions) == 0 {
			t.Error("LLM returned empty assertions list")
		}
		for _, a := range result.Assertions {
			if a == "" {
				t.Error("LLM returned empty assertion string")
			}
		}
		t.Logf("LLM decomposed into %d assertions", len(result.Assertions))
	})
}
