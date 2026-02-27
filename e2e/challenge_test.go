package e2e

import (
	"net/http"
	"testing"
	"time"
)

func TestChallenge(t *testing.T) {
	h, _ := ensureHarness(t)
	token, _ := h.Register(t, "challenge_user", "challengepass1234")

	questionID := h.AskQuestion(t, token, "Challenge test: Is nuclear energy the best solution for climate change?", []string{"energy", "climate"})
	h.AnswerNode(t, token, questionID, "Nuclear energy provides stable baseload power with minimal carbon emissions", "claim")
	h.AnswerNode(t, token, questionID, "Nuclear waste remains dangerous for thousands of years", "claim")

	t.Run("CreateChallenge", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		var challenge map[string]interface{}
		resp, err := h.JSON("POST", "/api/challenge/"+questionID, map[string]interface{}{
			"flow_name": "confrontation",
		}, token, &challenge)
		if err != nil {
			t.Fatalf("create challenge: %v", err)
		}
		RequireStatus(t, resp, http.StatusCreated)

		if challenge["status"] != "pending" {
			t.Errorf("status = %v, want pending", challenge["status"])
		}
		if challenge["flow_name"] != "confrontation" {
			t.Errorf("flow_name = %v, want confrontation", challenge["flow_name"])
		}
		if challenge["node_id"] != questionID {
			t.Errorf("node_id = %v, want %s", challenge["node_id"], questionID)
		}
	})

	t.Run("InvalidFlow", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		resp, _ := h.Do("POST", "/api/challenge/"+questionID, map[string]interface{}{
			"flow_name": "nonexistent_flow",
		}, token)
		RequireStatus(t, resp, http.StatusBadRequest)
	})

	t.Run("NodeNotFound", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		resp, _ := h.Do("POST", "/api/challenge/nonexistent123", map[string]interface{}{
			"flow_name": "confrontation",
		}, token)
		RequireStatus(t, resp, http.StatusNotFound)
	})

	t.Run("ListChallengesForNode", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		var result struct {
			Challenges []map[string]interface{} `json:"challenges"`
			Count      int                      `json:"count"`
		}
		resp, err := h.JSON("GET", "/api/challenges/"+questionID, nil, "", &result)
		if err != nil {
			t.Fatalf("list challenges: %v", err)
		}
		RequireStatus(t, resp, http.StatusOK)

		if result.Count == 0 {
			t.Error("expected at least 1 challenge")
		}
	})

	t.Run("GetChallengeByID", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		// Create a challenge to get its ID
		var created map[string]interface{}
		h.JSON("POST", "/api/challenge/"+questionID, map[string]interface{}{
			"flow_name": "red_team",
		}, token, &created)

		challengeID := created["id"].(string)
		var challenge map[string]interface{}
		resp, err := h.JSON("GET", "/api/challenge/"+challengeID, nil, "", &challenge)
		if err != nil {
			t.Fatalf("get challenge: %v", err)
		}
		RequireStatus(t, resp, http.StatusOK)

		if challenge["id"] != challengeID {
			t.Errorf("id = %v, want %s", challenge["id"], challengeID)
		}
	})

	t.Run("ModerationScores", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		var result struct {
			Scores []interface{} `json:"scores"`
			Count  int           `json:"count"`
		}
		resp, err := h.JSON("GET", "/api/moderation/"+questionID, nil, "", &result)
		if err != nil {
			t.Fatalf("get moderation: %v", err)
		}
		RequireStatus(t, resp, http.StatusOK)
		// Count may be 0 if no challenges have been run yet — that's OK
	})

	t.Run("Leaderboard", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		var result struct {
			Leaderboard []interface{} `json:"leaderboard"`
			Count       int           `json:"count"`
		}
		resp, err := h.JSON("GET", "/api/leaderboard/adversarial", nil, "", &result)
		if err != nil {
			t.Fatalf("leaderboard: %v", err)
		}
		RequireStatus(t, resp, http.StatusOK)
	})

	t.Run("ListFlows", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		var flows []map[string]interface{}
		resp, err := h.JSON("GET", "/api/flows", nil, "", &flows)
		if err != nil {
			t.Fatalf("list flows: %v", err)
		}
		RequireStatus(t, resp, http.StatusOK)

		if len(flows) != 6 {
			t.Errorf("expected 6 flows, got %d", len(flows))
		}

		// Verify expected flow names
		names := make(map[string]bool)
		for _, f := range flows {
			names[f["name"].(string)] = true
		}
		expected := []string{"confrontation", "red_team", "fidelity_benchmark", "adversarial_detection", "deep_dive", "safety_scoring"}
		for _, name := range expected {
			if !names[name] {
				t.Errorf("missing flow: %s", name)
			}
		}
	})

	// --- Abuse tests ---

	t.Run("Abuse_ChallengeSpam", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		spamQuestion := h.AskQuestion(t, token, "Challenge spam test: Can we create many challenges?", nil)
		h.AnswerNode(t, token, spamQuestion, "Yes we can test this", "claim")

		// Create 10 challenges on the same node
		created := 0
		for i := 0; i < 10; i++ {
			resp, _ := h.Do("POST", "/api/challenge/"+spamQuestion, map[string]interface{}{
				"flow_name": "confrontation",
			}, token)
			if resp.StatusCode == http.StatusCreated {
				created++
			}
		}
		t.Logf("Challenge spam: %d/10 challenges created (server did not crash)", created)

		// Verify listing works
		var result struct {
			Challenges []map[string]interface{} `json:"challenges"`
			Count      int                      `json:"count"`
		}
		resp, err := h.JSON("GET", "/api/challenges/"+spamQuestion, nil, "", &result)
		if err != nil {
			t.Fatalf("list challenges: %v", err)
		}
		RequireStatus(t, resp, http.StatusOK)
		t.Logf("Challenge spam: listing returned %d challenges", result.Count)
	})

	t.Run("Abuse_ChallengeUnauthenticated", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		resp, err := h.Do("POST", "/api/challenge/"+questionID, map[string]interface{}{
			"flow_name": "confrontation",
		}, "")
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("unauthenticated challenge: expected 401, got %d", resp.StatusCode)
		}
	})

	t.Run("Abuse_RunChallengeNotOwner", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		// User A creates a challenge
		ownerToken, _ := h.Register(t, "abuse_chal_owner", "ownerpass12345")
		otherToken, _ := h.Register(t, "abuse_chal_other", "otherpass12345")

		ownerQuestion := h.AskQuestion(t, ownerToken, "Challenge ownership test question", nil)
		h.AnswerNode(t, ownerToken, ownerQuestion, "Answer for challenge ownership test", "claim")

		var challenge map[string]interface{}
		h.JSON("POST", "/api/challenge/"+ownerQuestion, map[string]interface{}{
			"flow_name": "confrontation",
		}, ownerToken, &challenge)
		challengeID := challenge["id"].(string)

		// User B tries to run it
		resp, err := h.Do("POST", "/api/challenge/"+challengeID+"/run", nil, otherToken)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		RequireStatus(t, resp, http.StatusForbidden)
		t.Log("Challenge ownership correctly enforced — non-owner rejected")
	})
}
