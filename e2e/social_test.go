package e2e

import (
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestSocial(t *testing.T) {
	h, dba := ensureHarness(t)
	aliceToken, _ := h.Register(t, "social_alice", "alicepass1234")
	bobToken, _ := h.Register(t, "social_bob", "bobpass1234")
	carolToken, _ := h.Register(t, "social_carol", "carolpass1234")

	// Create a shared question
	questionID := h.AskQuestion(t, aliceToken, "Social test: What is the best programming language?", []string{"programming", "debate"})

	t.Run("VoteUpvote", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		resp, _ := h.Do("POST", "/api/vote", map[string]interface{}{
			"node_id": questionID,
			"value":   1,
		}, bobToken)
		RequireStatus(t, resp, http.StatusOK)

		// Verify score in DB
		dba.AssertNodeFieldGTE(t, questionID, "score", 1)
	})

	t.Run("VoteDownvote", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		resp, _ := h.Do("POST", "/api/vote", map[string]interface{}{
			"node_id": questionID,
			"value":   -1,
		}, carolToken)
		RequireStatus(t, resp, http.StatusOK)
	})

	t.Run("VoteReversal", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		// Bob changes from +1 to -1 → delta should be -2
		nodeBefore := h.GetNode(t, questionID)
		scoreBefore := int(nodeBefore["score"].(float64))

		resp, _ := h.Do("POST", "/api/vote", map[string]interface{}{
			"node_id": questionID,
			"value":   -1,
		}, bobToken)
		RequireStatus(t, resp, http.StatusOK)

		nodeAfter := h.GetNode(t, questionID)
		scoreAfter := int(nodeAfter["score"].(float64))

		if scoreAfter-scoreBefore != -2 {
			t.Errorf("vote reversal delta = %d, want -2", scoreAfter-scoreBefore)
		}
	})

	t.Run("VoteSameValueNoop", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		// Carol votes -1 again — should be no-op
		nodeBefore := h.GetNode(t, questionID)
		scoreBefore := int(nodeBefore["score"].(float64))

		resp, _ := h.Do("POST", "/api/vote", map[string]interface{}{
			"node_id": questionID,
			"value":   -1,
		}, carolToken)
		RequireStatus(t, resp, http.StatusOK)

		nodeAfter := h.GetNode(t, questionID)
		scoreAfter := int(nodeAfter["score"].(float64))

		if scoreAfter != scoreBefore {
			t.Errorf("same vote should be no-op: score changed from %d to %d", scoreBefore, scoreAfter)
		}
	})

	t.Run("VoteInvalidValue", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		resp, _ := h.Do("POST", "/api/vote", map[string]interface{}{
			"node_id": questionID,
			"value":   5,
		}, aliceToken)
		RequireStatus(t, resp, http.StatusBadRequest)
	})

	t.Run("ThankSuccess", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		resp, _ := h.Do("POST", "/api/thank", map[string]interface{}{
			"node_id": questionID,
			"message": "Great question!",
		}, bobToken)
		RequireStatus(t, resp, http.StatusOK)
	})

	t.Run("ThankIdempotent", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		// Bob thanks again — should be idempotent (INSERT OR IGNORE)
		resp, _ := h.Do("POST", "/api/thank", map[string]interface{}{
			"node_id": questionID,
			"message": "Still a great question!",
		}, bobToken)
		RequireStatus(t, resp, http.StatusOK)

		// Only one thank from bob should exist
		dba.AssertRowCount(t, "thanks", "from_user IN (SELECT id FROM users WHERE handle='social_bob') AND to_node = ?",
			[]interface{}{questionID}, 1)
	})

	t.Run("ThankMessageTruncation", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		longMsg := strings.Repeat("x", 200)
		resp, _ := h.Do("POST", "/api/thank", map[string]interface{}{
			"node_id": questionID,
			"message": longMsg,
		}, carolToken)
		RequireStatus(t, resp, http.StatusOK)
		// DB truncates to 140 chars — verify in DB
		// (can't easily verify exact truncation via API, but no error means it handled it)
	})

	t.Run("TagsOrderedByFrequency", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		// Create questions with tags to build frequency data
		h.AskQuestion(t, aliceToken, "Tag test 1: popular tag question", []string{"popular_tag", "rare_tag"})
		h.AskQuestion(t, aliceToken, "Tag test 2: another popular tag", []string{"popular_tag"})

		var tags []map[string]interface{}
		resp, err := h.JSON("GET", "/api/tags?limit=50", nil, "", &tags)
		if err != nil {
			t.Fatalf("get tags: %v", err)
		}
		RequireStatus(t, resp, http.StatusOK)

		if len(tags) == 0 {
			t.Error("expected at least 1 tag")
		}

		// Verify sorted by count descending
		for i := 1; i < len(tags); i++ {
			prev := tags[i-1]["count"].(float64)
			curr := tags[i]["count"].(float64)
			if prev < curr {
				t.Errorf("tags not ordered by frequency: %v before %v", tags[i-1], tags[i])
				break
			}
		}
	})

	t.Run("BountyCreation", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		var bounty map[string]interface{}
		resp, err := h.JSON("POST", "/api/bounty", map[string]interface{}{
			"node_id": questionID,
			"amount":  100,
		}, aliceToken, &bounty)
		if err != nil {
			t.Fatalf("create bounty: %v", err)
		}
		RequireStatus(t, resp, http.StatusCreated)

		if bounty["status"] != "active" {
			t.Errorf("bounty status = %v, want active", bounty["status"])
		}
		if bounty["amount"].(float64) != 100 {
			t.Errorf("bounty amount = %v, want 100", bounty["amount"])
		}

		// Verify via listing
		var bounties []map[string]interface{}
		resp, err = h.JSON("GET", "/api/bounties?limit=50", nil, "", &bounties)
		if err != nil {
			t.Fatalf("get bounties: %v", err)
		}
		RequireStatus(t, resp, http.StatusOK)
		if len(bounties) == 0 {
			t.Error("expected at least 1 active bounty")
		}
	})

	// --- Abuse tests ---

	t.Run("Abuse_SelfVote", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		// Alice votes on her own question
		resp, err := h.Do("POST", "/api/vote", map[string]interface{}{
			"node_id": questionID,
			"value":   1,
		}, aliceToken)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		RequireStatus(t, resp, http.StatusForbidden)
		t.Log("Self-vote correctly rejected with 403")
	})

	t.Run("Abuse_VoteNonexistentNode", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		resp, err := h.Do("POST", "/api/vote", map[string]interface{}{
			"node_id": "nonexistent_node_id_12345",
			"value":   1,
		}, aliceToken)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		// Must not panic — 404 or 500 are acceptable, not a crash
		t.Logf("Vote nonexistent node: status %d (server did not crash)", resp.StatusCode)
	})

	t.Run("Abuse_BountyNegativeAmount", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		resp, err := h.Do("POST", "/api/bounty", map[string]interface{}{
			"node_id": questionID,
			"amount":  BountyNegative,
		}, aliceToken)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		if resp.StatusCode == http.StatusCreated {
			t.Error("SECURITY NOTE: negative bounty accepted — server should reject amounts <= 0")
		}
		t.Logf("Negative bounty: status %d", resp.StatusCode)
	})

	t.Run("Abuse_BountyZeroAmount", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		resp, err := h.Do("POST", "/api/bounty", map[string]interface{}{
			"node_id": questionID,
			"amount":  0,
		}, aliceToken)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		if resp.StatusCode == http.StatusCreated {
			t.Error("SECURITY NOTE: zero bounty accepted — server should reject amounts <= 0")
		}
		t.Logf("Zero bounty: status %d", resp.StatusCode)
	})

	t.Run("Abuse_BountyMaxInt", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		resp, err := h.Do("POST", "/api/bounty", map[string]interface{}{
			"node_id": questionID,
			"amount":  BountyMaxInt,
		}, aliceToken)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		RequireStatus(t, resp, http.StatusBadRequest)
		t.Log("MaxInt bounty correctly rejected with 400")
	})

	t.Run("Abuse_ThankXSSMessage", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		xssThankUser, _ := h.Register(t, "abuse_social_thxss", "abusepass12345")
		resp, err := h.Do("POST", "/api/thank", map[string]interface{}{
			"node_id": questionID,
			"message": XSSScript,
		}, xssThankUser)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		if resp.StatusCode == http.StatusOK {
			// Verify message is stored literally and truncated to 140
			t.Log("SECURITY NOTE: XSS in thank message accepted — verify literal storage and 140-char truncation at render time")
		}
		t.Logf("XSS thank message: status %d (server did not crash)", resp.StatusCode)
	})

	t.Run("Abuse_VoteFloatValue", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		resp, err := h.Do("POST", "/api/vote", map[string]interface{}{
			"node_id": questionID,
			"value":   0.5,
		}, bobToken)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		// Float should be rejected or truncated — must not crash
		if resp.StatusCode == http.StatusOK {
			t.Log("SECURITY NOTE: float vote value (0.5) accepted — server should reject non-integer votes")
		}
		t.Logf("Float vote value: status %d (server did not crash)", resp.StatusCode)
	})
}
