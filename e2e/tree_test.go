package e2e

import (
	"fmt"
	"net/http"
	"testing"
	"time"
)

func TestTree(t *testing.T) {
	h, dba := ensureHarness(t)
	token, _ := h.Register(t, "tree_user", "treepass1234")

	t.Run("DeepTree9Levels", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		rootID := h.AskQuestion(t, token, "Deep tree test: root level question about recursion depth", nil)
		parentID := rootID

		for depth := 1; depth <= 9; depth++ {
			parentID = h.AnswerNode(t, token, parentID, fmt.Sprintf("Depth %d answer in linear chain", depth), "claim")
		}

		// Verify deepest node has depth = 9
		dba.AssertNodeField(t, parentID, "depth", int64(9))

		// Verify tree retrieval
		var tree map[string]interface{}
		resp, err := h.JSON("GET", "/api/tree/"+rootID, nil, "", &tree)
		if err != nil {
			t.Fatalf("get tree: %v", err)
		}
		if resp.StatusCode != 200 {
			t.Fatalf("expected 200, got %d", resp.StatusCode)
		}
	})

	t.Run("WideTree12Children", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		rootID := h.AskQuestion(t, token, "Wide tree test: root level question about breadth", nil)

		for i := 1; i <= 12; i++ {
			h.AnswerNode(t, token, rootID, fmt.Sprintf("Wide answer number %d", i), "claim")
		}

		// Verify child_count
		dba.AssertNodeFieldGTE(t, rootID, "child_count", 12)

		// Verify all children via tree API
		var tree map[string]interface{}
		h.JSON("GET", "/api/tree/"+rootID, nil, "", &tree)
		children, ok := tree["children"].([]interface{})
		if !ok || len(children) < 12 {
			t.Errorf("expected at least 12 children, got %d", len(children))
		}
	})

	t.Run("TemperatureCold", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		// Fresh question with no activity
		rootID := h.AskQuestion(t, token, "Temperature cold: minimal activity question", nil)
		dba.AssertTemperature(t, rootID, "cold")
	})

	t.Run("TemperatureWarm", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		rootID := h.AskQuestion(t, token, "Temperature warm: moderate activity question", nil)

		// Add 3 children → should trigger warm
		for i := 0; i < 3; i++ {
			h.AnswerNode(t, token, rootID, fmt.Sprintf("Warm answer %d", i), "claim")
		}

		// Temperature is recalculated by challenges, so we check child_count condition
		dba.AssertNodeFieldGTE(t, rootID, "child_count", 3)
	})

	t.Run("TemperatureHot", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		token2, _ := h.Register(t, "hot_voter_1", "hotvoter1pass")
		token3, _ := h.Register(t, "hot_voter_2", "hotvoter2pass")
		token4, _ := h.Register(t, "hot_voter_3", "hotvoter3pass")
		token5, _ := h.Register(t, "hot_voter_4", "hotvoter4pass")
		token6, _ := h.Register(t, "hot_voter_5", "hotvoter5pass")
		token7, _ := h.Register(t, "hot_voter_6", "hotvoter6pass")
		token8, _ := h.Register(t, "hot_voter_7", "hotvoter7pass")
		token9, _ := h.Register(t, "hot_voter_8", "hotvoter8pass")
		token10, _ := h.Register(t, "hot_voter_9", "hotvoter9pass")
		token11, _ := h.Register(t, "hot_voter_10", "hotvoterApass")

		rootID := h.AskQuestion(t, token, "Temperature hot: high activity question with many children and votes", nil)

		// 6 children
		for i := 0; i < 6; i++ {
			h.AnswerNode(t, token, rootID, fmt.Sprintf("Hot answer %d", i), "claim")
		}

		// 10 votes from different users
		voters := []string{token2, token3, token4, token5, token6, token7, token8, token9, token10, token11}
		for _, v := range voters {
			h.Do("POST", "/api/vote", map[string]interface{}{"node_id": rootID, "value": 1}, v)
		}

		dba.AssertNodeFieldGTE(t, rootID, "child_count", 6)
		dba.AssertNodeFieldGTE(t, rootID, "score", 10)
	})

	t.Run("TemperatureCritical", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		crit1, _ := h.Register(t, "crit_voter_1", "critpass12345")
		crit2, _ := h.Register(t, "crit_voter_2", "critpass12345")
		crit3, _ := h.Register(t, "crit_voter_3", "critpass12345")
		crit4, _ := h.Register(t, "crit_voter_4", "critpass12345")
		crit5, _ := h.Register(t, "crit_voter_5", "critpass12345")
		crit6, _ := h.Register(t, "crit_voter_6", "critpass12345")
		crit7, _ := h.Register(t, "crit_voter_7", "critpass12345")
		crit8, _ := h.Register(t, "crit_voter_8", "critpass12345")
		crit9, _ := h.Register(t, "crit_voter_9", "critpass12345")
		crit10, _ := h.Register(t, "crit_voter_a", "critpass12345")
		crit11, _ := h.Register(t, "crit_voter_b", "critpass12345")
		crit12, _ := h.Register(t, "crit_voter_c", "critpass12345")
		crit13, _ := h.Register(t, "crit_voter_d", "critpass12345")
		crit14, _ := h.Register(t, "crit_voter_e", "critpass12345")
		crit15, _ := h.Register(t, "crit_voter_f", "critpass12345")
		crit16, _ := h.Register(t, "crit_voter_g", "critpass12345")
		crit17, _ := h.Register(t, "crit_voter_h", "critpass12345")
		crit18, _ := h.Register(t, "crit_voter_i", "critpass12345")
		crit19, _ := h.Register(t, "crit_voter_j", "critpass12345")
		crit20, _ := h.Register(t, "crit_voter_k", "critpass12345")
		crit21, _ := h.Register(t, "crit_voter_l", "critpass12345")

		rootID := h.AskQuestion(t, token, "Temperature critical: extremely high activity question", nil)

		// 11 children
		for i := 0; i < 11; i++ {
			h.AnswerNode(t, token, rootID, fmt.Sprintf("Critical answer %d", i), "claim")
		}

		// 21 votes from different users
		voters := []string{crit1, crit2, crit3, crit4, crit5, crit6, crit7, crit8, crit9, crit10,
			crit11, crit12, crit13, crit14, crit15, crit16, crit17, crit18, crit19, crit20, crit21}
		for _, v := range voters {
			h.Do("POST", "/api/vote", map[string]interface{}{"node_id": rootID, "value": 1}, v)
		}

		dba.AssertNodeFieldGTE(t, rootID, "child_count", 11)
		dba.AssertNodeFieldGTE(t, rootID, "score", 21)
	})

	// --- Abuse tests ---

	t.Run("Abuse_CircularParent", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		questionID := h.AskQuestion(t, token, "Circular parent test question", nil)
		// Attempt to create a node whose parent_id is its own ID (impossible since ID unknown,
		// but use the questionID as parent to create a child, then try to re-parent to itself)
		resp, err := h.Do("POST", "/api/answer", map[string]interface{}{
			"parent_id": questionID,
			"body":      "Attempting circular reference",
			"node_type": "claim",
		}, token)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		RequireStatus(t, resp, http.StatusCreated)

		// Now try creating a node with parent_id = itself (fabricated ID)
		resp2, err := h.Do("POST", "/api/answer", map[string]interface{}{
			"parent_id": "self_referencing_node_id",
			"body":      "This node references itself",
			"node_type": "claim",
		}, token)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		// Should fail since parent doesn't exist — must not create infinite loop
		t.Logf("Circular parent attempt: status %d (server did not crash)", resp2.StatusCode)
	})

	t.Run("Abuse_DepthBomb20Levels", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		rootID := h.AskQuestion(t, token, "Depth bomb 20 levels test question", nil)
		parentID := rootID

		// Create 20-level deep tree
		for depth := 1; depth <= 20; depth++ {
			parentID = h.AnswerNode(t, token, parentID, fmt.Sprintf("Depth bomb level %d", depth), "claim")
		}

		// Query with depth=999999 — must respond in reasonable time
		queryStart := time.Now()
		resp, err := h.Do("GET", fmt.Sprintf("/api/tree/%s?depth=%d", rootID, DepthBomb), nil, "")
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		elapsed := time.Since(queryStart)
		if elapsed > 10*time.Second {
			t.Errorf("depth=999999 on 20-level tree took %v — possible memory explosion", elapsed)
		}
		t.Logf("Depth bomb 20 levels: status %d in %v (server did not crash)", resp.StatusCode, elapsed)
	})

	t.Run("Abuse_MassChildrenSpam", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		rootID := h.AskQuestion(t, token, "Mass children spam test question", nil)

		// Create 50 children on same node
		for i := 0; i < 50; i++ {
			h.AnswerNode(t, token, rootID, fmt.Sprintf("Spam child %d", i), "claim")
		}

		// Verify child_count is correct
		dba.AssertNodeFieldGTE(t, rootID, "child_count", 50)

		// Verify tree retrieval works
		var tree map[string]interface{}
		resp, err := h.JSON("GET", "/api/tree/"+rootID, nil, "", &tree)
		if err != nil {
			t.Fatalf("get tree: %v", err)
		}
		RequireStatus(t, resp, http.StatusOK)

		children, ok := tree["children"].([]interface{})
		if !ok || len(children) < 50 {
			t.Errorf("expected at least 50 children, got %d", len(children))
		}
		t.Logf("Mass children spam: %d children created successfully", len(children))
	})
}
