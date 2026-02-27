package e2e

import (
	"net/http"
	"testing"
	"time"
)

func TestVisibility(t *testing.T) {
	h, dba := ensureHarness(t)

	t.Run("DefaultVisibilityPublic", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		token, _ := h.Register(t, "vis_default_user", "visdefault1234")
		nodeID := h.AskQuestion(t, token, "What is the default visibility of new nodes?", nil)

		node := h.GetNode(t, nodeID)

		vis, ok := node["visibility"].(string)
		if !ok {
			// Visibility might not be present in response if defaulted at DB level
			// Check via DB assert
			dba.AssertNodeVisibility(t, nodeID, "public")
			return
		}
		if vis != "public" {
			t.Errorf("expected visibility 'public', got %q", vis)
		}
	})

	t.Run("SystematicCloneCreated", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		token, _ := h.Register(t, "vis_clone_user", "visclone1234")

		// Create a question
		nodeID := h.AskQuestion(t, token, "What is the purpose of systematic cloning in horostracker?", nil)

		// Verify that a clone exists in node_clones
		cloneID, exists := dba.QueryCloneExists(t, nodeID)
		if !exists {
			t.Fatal("expected a clone to exist for the created node")
		}

		// Verify the clone has visibility "provider"
		cloneVis := dba.QueryCloneVisibility(t, cloneID)
		if cloneVis != "provider" {
			t.Errorf("expected clone visibility 'provider', got %q", cloneVis)
		}

		t.Logf("Clone %s created for node %s with visibility %q", cloneID, nodeID, cloneVis)
	})

	t.Run("VisibilityStrataTable", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		// Verify the visibility_strata table has the expected seed data
		strata := dba.QueryStrata(t)
		if len(strata) != 4 {
			t.Errorf("expected 4 visibility strata, got %d", len(strata))
		}

		expectedStrata := map[string]string{
			"public":   "anon",
			"research": "researcher",
			"provider": "provider",
			"instance": "operator",
		}

		for stratum, expectedRole := range expectedStrata {
			role, ok := strata[stratum]
			if !ok {
				t.Errorf("missing stratum %q", stratum)
				continue
			}
			if role != expectedRole {
				t.Errorf("stratum %q: expected min_role %q, got %q", stratum, expectedRole, role)
			}
		}
	})

	t.Run("VisibilityFilterQuestions", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		token, _ := h.Register(t, "vis_filter_user", "visfilter1234")

		// Create a normal question
		normalID := h.AskQuestion(t, token, "What are the advantages of functional programming for visibility filter test?", nil)

		// Manually set the question to research visibility via DB
		dba.SetNodeVisibility(t, normalID, "research")

		// Fetch questions as unauthenticated user (role=anon → sees only public)
		var questions []map[string]any
		resp, err := h.JSON("GET", "/api/questions?limit=100", nil, "", &questions)
		if err != nil {
			t.Fatalf("get questions: %v", err)
		}
		RequireStatus(t, resp, http.StatusOK)

		// The research question should not appear for an anonymous/unauthenticated user
		for _, q := range questions {
			if q["id"] == normalID {
				t.Error("research-visibility question should not appear in unauthenticated questions feed")
			}
		}
	})
}
