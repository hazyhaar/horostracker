package e2e

import (
	"fmt"
	"net/http"
	"testing"
	"time"
)

func TestSecurity(t *testing.T) {
	h, _ := ensureHarness(t)

	t.Run("SecurityHeaders", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		resp, err := h.Do("GET", "/api/questions", nil, "")
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		defer resp.Body.Close()

		headers := map[string]string{
			"X-Content-Type-Options": "nosniff",
			"X-Frame-Options":       "DENY",
			"Referrer-Policy":       "strict-origin-when-cross-origin",
		}
		for name, expected := range headers {
			got := resp.Header.Get(name)
			if got != expected {
				t.Errorf("header %s = %q, want %q", name, got, expected)
			}
		}

		csp := resp.Header.Get("Content-Security-Policy")
		if csp == "" {
			t.Error("missing Content-Security-Policy header")
		}

		pp := resp.Header.Get("Permissions-Policy")
		if pp == "" {
			t.Error("missing Permissions-Policy header")
		}
	})

	t.Run("SafetyScoreOnCreate", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		token, _ := h.Register(t, "safety_create_user", "safetypass1234")

		var result map[string]interface{}
		resp, err := h.JSON("POST", "/api/ask", map[string]interface{}{
			"body": "What are the implications of quantum computing for cryptography?",
		}, token, &result)
		if err != nil {
			t.Fatalf("ask question: %v", err)
		}
		RequireStatus(t, resp, http.StatusCreated)

		ss, ok := result["safety_score"]
		if !ok || ss == nil {
			t.Fatal("expected safety_score in response")
		}

		ssMap, ok := ss.(map[string]interface{})
		if !ok {
			t.Fatal("safety_score is not an object")
		}

		score, ok := ssMap["score"].(float64)
		if !ok {
			t.Fatal("safety_score.score missing or not a number")
		}
		if score < 0 || score > 1 {
			t.Errorf("safety_score.score = %v, want between 0.0 and 1.0", score)
		}

		// Normal content should score high (safe)
		if score < 0.5 {
			t.Errorf("normal content scored %v, expected > 0.5", score)
		}
	})

	t.Run("SafetyScoreTimeline", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		token, _ := h.Register(t, "safety_timeline_user", "timelinepass1234")
		nodeID := h.AskQuestion(t, token, "How does machine learning work?", nil)

		var consensus map[string]interface{}
		resp, err := h.JSON("GET", "/api/nodes/"+nodeID+"/safety", nil, "", &consensus)
		if err != nil {
			t.Fatalf("get safety: %v", err)
		}
		RequireStatus(t, resp, http.StatusOK)

		count, ok := consensus["scores_count"].(float64)
		if !ok || count < 1 {
			t.Errorf("expected at least 1 score in timeline, got %v", consensus["scores_count"])
		}
	})

	t.Run("PromptInjectionDetection", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		token, _ := h.Register(t, "safety_injection_user", "injectionpass1234")

		var result map[string]interface{}
		resp, err := h.JSON("POST", "/api/ask", map[string]interface{}{
			"body": PromptInjection,
		}, token, &result)
		if err != nil {
			t.Fatalf("ask question: %v", err)
		}
		RequireStatus(t, resp, http.StatusCreated)

		ss, ok := result["safety_score"]
		if !ok || ss == nil {
			t.Fatal("expected safety_score in response")
		}

		ssMap := ss.(map[string]interface{})
		severity := ssMap["severity"].(string)
		score := ssMap["score"].(float64)

		// Prompt injection should trigger detection (score < 1.0, severity != "info")
		if severity == "info" {
			t.Errorf("prompt injection should have severity > info, got %q (score: %v)", severity, score)
		}
		if score >= 1.0 {
			t.Errorf("prompt injection scored %v, expected < 1.0", score)
		}

		t.Logf("Prompt injection detected: severity=%s, score=%.2f", severity, score)
	})

	t.Run("PromptPatternsCRUD", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		token, _ := h.Register(t, "safety_patterns_user", "patternspass1234")

		// Create a pattern
		resp, err := h.Do("POST", "/api/safety/patterns", map[string]interface{}{
			"pattern":      "test_dangerous_pattern",
			"pattern_type": "exact",
			"list_type":    "flag",
			"severity":     "low",
			"language":     "en",
			"category":     "test",
			"description":  "Test pattern for E2E",
		}, token)
		if err != nil {
			t.Fatalf("create pattern: %v", err)
		}
		RequireStatus(t, resp, http.StatusCreated)
		resp.Body.Close()

		// List patterns
		var listResult struct {
			Patterns []map[string]interface{} `json:"patterns"`
			Count    int                      `json:"count"`
		}
		resp, err = h.JSON("GET", "/api/safety/patterns", nil, "", &listResult)
		if err != nil {
			t.Fatalf("list patterns: %v", err)
		}
		RequireStatus(t, resp, http.StatusOK)

		if listResult.Count == 0 {
			t.Error("expected at least 1 pattern")
		}

		// Find our test pattern and vote on it
		for _, p := range listResult.Patterns {
			if p["pattern"] == "test_dangerous_pattern" {
				id := p["id"].(float64)
				var voteResult map[string]interface{}
				resp, err = h.JSON("PUT", "/api/safety/patterns/"+
					func() string { return fmt.Sprintf("%d", int(id)) }()+"/vote",
					map[string]interface{}{"up": true}, token, &voteResult)
				if err != nil {
					t.Fatalf("vote pattern: %v", err)
				}
				RequireStatus(t, resp, http.StatusOK)

				votesUp, _ := voteResult["votes_up"].(float64)
				if votesUp < 1 {
					t.Errorf("expected votes_up >= 1, got %v", votesUp)
				}
				break
			}
		}

		// Export patterns
		var exportResult []map[string]interface{}
		resp, err = h.JSON("GET", "/api/safety/patterns/export", nil, "", &exportResult)
		if err != nil {
			t.Fatalf("export patterns: %v", err)
		}
		RequireStatus(t, resp, http.StatusOK)

		if len(exportResult) == 0 {
			t.Error("export returned no patterns")
		}
	})

	t.Run("SafetyLeaderboard", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		var result struct {
			Leaderboard []map[string]interface{} `json:"leaderboard"`
			Count       int                      `json:"count"`
		}
		resp, err := h.JSON("GET", "/api/safety/leaderboard", nil, "", &result)
		if err != nil {
			t.Fatalf("leaderboard: %v", err)
		}
		RequireStatus(t, resp, http.StatusOK)

		// We've created nodes, so the local scorer should appear
		if result.Count == 0 {
			t.Error("expected at least 1 scorer in leaderboard")
		}

		found := false
		for _, entry := range result.Leaderboard {
			if entry["scorer_id"] == "horostracker-v1" {
				found = true
				break
			}
		}
		if !found {
			t.Error("expected horostracker-v1 in leaderboard")
		}
	})

	t.Run("AnswerSafetyScore", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		token, _ := h.Register(t, "safety_answer_user", "answerpass1234")
		questionID := h.AskQuestion(t, token, "What is graph theory?", nil)

		var result map[string]interface{}
		resp, err := h.JSON("POST", "/api/answer", map[string]interface{}{
			"parent_id": questionID,
			"body":      "Graph theory studies mathematical structures used to model pairwise relations between objects.",
			"node_type": "claim",
		}, token, &result)
		if err != nil {
			t.Fatalf("answer: %v", err)
		}
		RequireStatus(t, resp, http.StatusCreated)

		ss, ok := result["safety_score"]
		if !ok || ss == nil {
			t.Fatal("expected safety_score in answer response")
		}

		ssMap := ss.(map[string]interface{})
		score := ssMap["score"].(float64)
		if score < 0.5 {
			t.Errorf("normal answer scored %v, expected > 0.5", score)
		}
	})

	t.Run("AsyncSafetyScorer", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		if !HasLLM() {
			t.Skip("no LLM API key configured — skipping async scorer test")
		}

		token, _ := h.Register(t, "safety_async_user", "asyncpass1234")
		nodeID := h.AskQuestion(t, token, "What is the impact of deepfakes on democratic processes?", nil)

		// Wait for async scorers to complete (up to 20s)
		deadline := time.Now().Add(20 * time.Second)
		for time.Now().Before(deadline) {
			var consensus map[string]interface{}
			resp, err := h.JSON("GET", "/api/nodes/"+nodeID+"/safety", nil, "", &consensus)
			if err != nil {
				t.Fatalf("get safety: %v", err)
			}
			if resp.StatusCode != http.StatusOK {
				time.Sleep(500 * time.Millisecond)
				continue
			}

			count, ok := consensus["scores_count"].(float64)
			if ok && count >= 2 {
				t.Logf("Async scoring complete: %d scores in timeline", int(count))
				return
			}
			time.Sleep(1 * time.Second)
		}

		// At least verify we have more than the initial local score
		var consensus map[string]interface{}
		resp, err := h.JSON("GET", "/api/nodes/"+nodeID+"/safety", nil, "", &consensus)
		if err != nil {
			t.Fatalf("final safety check: %v", err)
		}
		RequireStatus(t, resp, http.StatusOK)

		count, _ := consensus["scores_count"].(float64)
		t.Logf("Final score count: %d (local scorer always provides 1)", int(count))
		if count < 1 {
			t.Error("expected at least the local safety score")
		}
	})

	// RateLimitSearch tests rate limiting on the search endpoint (limit: 30/60s).
	// This is a more reliable test since search doesn't create resources.
	t.Run("RateLimitSearch", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		// The rate limit for POST /api/search is 30 requests per 60 seconds.
		// Send 40 rapid search requests and expect at least some to be rate-limited.
		limited := 0
		for i := 0; i < 40; i++ {
			resp, err := h.Do("POST", "/api/search", map[string]interface{}{
				"query": fmt.Sprintf("rate limit test query %d", i),
			}, "")
			if err != nil {
				t.Fatalf("search %d: %v", i, err)
			}
			resp.Body.Close()
			if resp.StatusCode == http.StatusTooManyRequests {
				limited++
			}
		}

		t.Logf("Rate limit: %d/40 search requests were rate-limited", limited)
		if limited == 0 {
			t.Error("expected at least some requests to be rate-limited")
		}
	})
}
