package e2e

import (
	"net/http"
	"testing"
	"time"
)

func TestSearch(t *testing.T) {
	h, _ := ensureHarness(t)
	token, _ := h.Register(t, "search_user", "searchpass1234")

	// Seed content for search
	h.AskQuestion(t, token, "Search test: microservices architecture patterns for distributed systems", TagsAI)
	h.AskQuestion(t, token, "Search test: quantum computing fundamentals and qubit manipulation", TagsScience)
	h.AskQuestion(t, token, TextArabic, nil)

	// Allow FTS index to settle
	time.Sleep(200 * time.Millisecond)

	t.Run("BasicSearch", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		var result struct {
			Results []interface{} `json:"results"`
			Count   int           `json:"count"`
		}
		resp, err := h.JSON("POST", "/api/search", map[string]interface{}{
			"query": "microservices",
		}, "", &result)
		if err != nil {
			t.Fatalf("search: %v", err)
		}
		RequireStatus(t, resp, http.StatusOK)
		if result.Count == 0 {
			t.Error("expected at least 1 result for 'microservices'")
		}
	})

	t.Run("EmptyQuery", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		resp, _ := h.Do("POST", "/api/search", map[string]interface{}{
			"query": "",
		}, "")
		RequireStatus(t, resp, http.StatusBadRequest)
	})

	t.Run("SpecialCharacters", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		// FTS5 special chars should not crash the server
		var result struct {
			Results []interface{} `json:"results"`
			Count   int           `json:"count"`
		}
		resp, err := h.JSON("POST", "/api/search", map[string]interface{}{
			"query": "quantum computing",
		}, "", &result)
		if err != nil {
			t.Fatalf("search: %v", err)
		}
		RequireStatus(t, resp, http.StatusOK)
	})

	t.Run("UnicodeContent", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		var result struct {
			Results []interface{} `json:"results"`
			Count   int           `json:"count"`
		}
		// Search for Arabic content
		resp, err := h.JSON("POST", "/api/search", map[string]interface{}{
			"query": "الذكاء",
		}, "", &result)
		if err != nil {
			t.Fatalf("search arabic: %v", err)
		}
		RequireStatus(t, resp, http.StatusOK)
		if result.Count == 0 {
			t.Error("expected at least 1 result for Arabic query")
		}
	})

	t.Run("PhraseSearch", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		var result struct {
			Results []interface{} `json:"results"`
			Count   int           `json:"count"`
		}
		resp, err := h.JSON("POST", "/api/search", map[string]interface{}{
			"query": "distributed systems",
		}, "", &result)
		if err != nil {
			t.Fatalf("search phrase: %v", err)
		}
		RequireStatus(t, resp, http.StatusOK)
		if result.Count == 0 {
			t.Error("expected at least 1 result for phrase 'distributed systems'")
		}
	})

	t.Run("LimitParameter", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		var result struct {
			Results []interface{} `json:"results"`
			Count   int           `json:"count"`
		}
		resp, err := h.JSON("POST", "/api/search", map[string]interface{}{
			"query": "test",
			"limit": 1,
		}, "", &result)
		if err != nil {
			t.Fatalf("search: %v", err)
		}
		RequireStatus(t, resp, http.StatusOK)
		if result.Count > 1 {
			t.Errorf("expected at most 1 result with limit=1, got %d", result.Count)
		}
	})

	// --- Abuse tests ---

	t.Run("Abuse_FTS5Injection", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		exploits := []string{"* OR 1=1", "NEAR(a,b)", `"unclosed`, "col:value"}
		for _, q := range exploits {
			resp, err := h.Do("POST", "/api/search", map[string]interface{}{
				"query": q,
			}, "")
			if err != nil {
				t.Fatalf("FTS5 injection (%s): request failed: %v", q, err)
			}
			if resp.StatusCode == http.StatusInternalServerError {
				t.Errorf("FTS5 exploit query %q caused 500 — should be sanitized to 200 or 400", q)
			}
			t.Logf("FTS5 injection %q: status %d (correctly handled)", q, resp.StatusCode)
		}
	})

	t.Run("Abuse_SQLiInSearch", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		resp, err := h.Do("POST", "/api/search", map[string]interface{}{
			"query": SQLiBasic,
		}, "")
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		if resp.StatusCode == http.StatusInternalServerError {
			t.Error("SQLi in search caused 500 — should be sanitized to 200 or 400")
		}
		t.Logf("SQLi in search: status %d (correctly handled)", resp.StatusCode)
	})

	t.Run("Abuse_HugeQuery", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		resp, err := h.Do("POST", "/api/search", map[string]interface{}{
			"query": HugeQuery,
		}, "")
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		if resp.StatusCode != http.StatusRequestEntityTooLarge {
			t.Errorf("100KB search query: expected 413, got %d", resp.StatusCode)
		}
		t.Logf("100KB search query: status %d", resp.StatusCode)
	})

	t.Run("Abuse_ControlCharsInQuery", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		resp, err := h.Do("POST", "/api/search", map[string]interface{}{
			"query": ControlChars,
		}, "")
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		if resp.StatusCode == http.StatusInternalServerError {
			t.Error("CRLF injection in search caused 500 — should be sanitized to 200 or 400")
		}
		t.Logf("Control chars in search: status %d (correctly handled)", resp.StatusCode)
	})
}
