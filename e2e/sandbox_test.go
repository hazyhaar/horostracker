package e2e

import (
	"net/http"
	"testing"
	"time"
)

func TestSandbox(t *testing.T) {
	h, dba := ensureHarness(t)

	t.Run("TestSystematicCloning", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		token, _ := h.Register(t, "clone_systematic", "clonepass1234")

		// Create a question + answer
		questionID := h.AskQuestion(t, token, "What are the security implications of WebAssembly for systematic cloning?", nil)
		answerID := h.AnswerNode(t, token, questionID, "WebAssembly runs in a sandboxed VM with limited system access.", "claim")

		// Verify both question and answer have clones
		qCloneID, qExists := dba.QueryCloneExists(t, questionID)
		if !qExists {
			t.Fatal("expected clone for question")
		}
		aCloneID, aExists := dba.QueryCloneExists(t, answerID)
		if !aExists {
			t.Fatal("expected clone for answer")
		}

		// Verify clones have visibility "provider"
		qVis := dba.QueryCloneVisibility(t, qCloneID)
		if qVis != "provider" {
			t.Errorf("question clone visibility = %q, want 'provider'", qVis)
		}
		aVis := dba.QueryCloneVisibility(t, aCloneID)
		if aVis != "provider" {
			t.Errorf("answer clone visibility = %q, want 'provider'", aVis)
		}

		t.Logf("Question clone: %s, Answer clone: %s", qCloneID, aCloneID)
	})

	t.Run("TestCloneParentLinkage", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		token, _ := h.Register(t, "clone_linkage", "linkagepass1234")

		// Create question → answer → objection
		questionID := h.AskQuestion(t, token, "How does homomorphic encryption enable computation on encrypted data?", nil)
		answerID := h.AnswerNode(t, token, questionID, "Homomorphic encryption allows mathematical operations on ciphertexts.", "claim")
		objectionID := h.AnswerNode(t, token, answerID, "However, performance overhead makes it impractical for real-time systems.", "claim")

		// Get clone IDs
		answerCloneID, aExists := dba.QueryCloneExists(t, answerID)
		if !aExists {
			t.Fatal("expected clone for answer")
		}
		objCloneID, oExists := dba.QueryCloneExists(t, objectionID)
		if !oExists {
			t.Fatal("expected clone for objection")
		}

		// Verify the objection clone's parent is the answer clone
		objCloneParent := dba.QueryCloneParent(t, objCloneID)
		if objCloneParent != answerCloneID {
			t.Errorf("objection clone parent = %q, want %q (answer clone)", objCloneParent, answerCloneID)
		}

		t.Logf("Objection clone %s correctly linked to answer clone %s", objCloneID, answerCloneID)
	})

	t.Run("TestCloneNotInSearch", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		token, _ := h.Register(t, "clone_search", "searchpass1234")

		// Create a node with a unique keyword
		uniqueKeyword := "QuantumBrainfogTest"
		h.AskQuestion(t, token, "What is "+uniqueKeyword+" and why does it matter?", nil)

		// Search for the keyword
		var result map[string]any
		resp, err := h.JSON("POST", "/api/search", map[string]any{
			"query": uniqueKeyword,
		}, token, &result)
		if err != nil {
			t.Fatalf("search: %v", err)
		}
		RequireStatus(t, resp, http.StatusOK)

		results, ok := result["results"].([]any)
		if !ok {
			t.Log("no results array — search may have returned nil (acceptable)")
			return
		}

		// Only the original node should appear, not the clone (which has visibility "provider")
		count := 0
		for _, r := range results {
			node := r.(map[string]any)
			vis, _ := node["visibility"].(string)
			if vis == "provider" {
				t.Error("clone with visibility 'provider' should not appear in search results")
			}
			count++
		}
		t.Logf("Search returned %d results for %q (clones excluded)", count, uniqueKeyword)
	})

	t.Run("TestCloneNotInFeed", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		token, _ := h.Register(t, "clone_feed", "feedpass1234")
		h.AskQuestion(t, token, "What is the impact of quantum key distribution for clone feed test?", nil)

		// Fetch questions feed as regular user (should not see provider-visibility clones)
		var questions []map[string]any
		resp, err := h.JSON("GET", "/api/questions?limit=100", nil, token, &questions)
		if err != nil {
			t.Fatalf("get questions: %v", err)
		}
		RequireStatus(t, resp, http.StatusOK)

		for _, q := range questions {
			vis, _ := q["visibility"].(string)
			if vis == "provider" {
				t.Error("provider-visibility clone should not appear in questions feed for regular user")
			}
		}
	})

	t.Run("TestAccessGating", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		token, _ := h.Register(t, "access_gate_user", "accesspass1234")

		// Create a question
		questionID := h.AskQuestion(t, token, "What are the limitations of formal verification for access gating test?", nil)

		// Set the question's visibility to "research" so only researchers+ can see it
		dba.SetNodeVisibility(t, questionID, "research")

		// A regular user should not see it in the feed
		var questions []map[string]any
		resp, err := h.JSON("GET", "/api/questions?limit=100", nil, token, &questions)
		if err != nil {
			t.Fatalf("get questions: %v", err)
		}
		RequireStatus(t, resp, http.StatusOK)

		for _, q := range questions {
			if q["id"] == questionID {
				t.Error("research-visibility node should not appear for regular user")
			}
		}
	})

	t.Run("ClonesEndpoint", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		token, _ := h.Register(t, "clone_endpoint", "endpointpass1234")
		questionID := h.AskQuestion(t, token, "Test question for clones endpoint", nil)
		h.AnswerNode(t, token, questionID, "Test answer for clones endpoint", "claim")

		// Get clones via API
		var result map[string]any
		resp, err := h.JSON("GET", "/api/questions/"+questionID+"/clones", nil, "", &result)
		if err != nil {
			t.Fatalf("get clones: %v", err)
		}
		RequireStatus(t, resp, http.StatusOK)

		count, ok := result["count"].(float64)
		if !ok || count < 2 {
			t.Errorf("expected at least 2 clones (question + answer), got %v", result["count"])
		}
	})

	t.Run("AccessRequiresAuth", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		// Try to configure access without auth
		resp, err := h.Do("POST", "/api/questions/some-id/access", map[string]any{
			"visibility": "provider",
		}, "")
		if err != nil {
			t.Fatalf("configure access: %v", err)
		}
		resp.Body.Close()

		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("expected 401 for unauthenticated access config, got %d", resp.StatusCode)
		}
	})
}
