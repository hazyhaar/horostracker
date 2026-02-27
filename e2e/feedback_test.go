package e2e

import (
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestFeedback(t *testing.T) {
	h, dba := ensureHarness(t)

	var submittedID string

	t.Run("SubmitAnonymous", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		var result map[string]string
		resp, err := h.JSON("POST", "/feedback/submit", map[string]string{
			"text":     "Super produit, bravo !",
			"page_url": "http://localhost/#/home",
		}, "", &result)
		if err != nil {
			t.Fatalf("POST /feedback/submit: %v", err)
		}
		RequireStatus(t, resp, http.StatusOK)

		if result["status"] != "ok" {
			t.Errorf("status = %q, want ok", result["status"])
		}
		if result["id"] == "" {
			t.Error("expected non-empty id")
		}
		submittedID = result["id"]
	})

	t.Run("SubmitAuthenticated", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		token, userID := h.Register(t, "fb_auth_user", "password1234")

		var result map[string]string
		resp, err := h.JSON("POST", "/feedback/submit", map[string]string{
			"text":     "Commentaire authentifié",
			"page_url": "http://localhost/#/profile",
		}, token, &result)
		if err != nil {
			t.Fatalf("POST /feedback/submit: %v", err)
		}
		RequireStatus(t, resp, http.StatusOK)

		if result["status"] != "ok" {
			t.Errorf("status = %q, want ok", result["status"])
		}

		// Verify the user_id was recorded in the database
		var dbUserID string
		db, err := dba.nodes()
		if err != nil {
			t.Fatalf("opening nodes.db: %v", err)
		}
		err = db.QueryRow(`SELECT COALESCE(user_id,'') FROM feedback_comments WHERE id = ?`, result["id"]).Scan(&dbUserID)
		if err != nil {
			t.Fatalf("querying feedback comment: %v", err)
		}
		if dbUserID != userID {
			t.Errorf("user_id in DB = %q, want %q", dbUserID, userID)
		}
	})

	t.Run("SubmitEmptyText", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		resp, _ := h.Do("POST", "/feedback/submit", map[string]string{
			"text":     "",
			"page_url": "http://localhost/",
		}, "")
		RequireStatus(t, resp, http.StatusBadRequest)
	})

	t.Run("SubmitInvalidJSON", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		resp, _ := h.Do("POST", "/feedback/submit", nil, "")
		RequireStatus(t, resp, http.StatusBadRequest)
	})

	t.Run("ListJSON", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		var comments []map[string]interface{}
		resp, err := h.JSON("GET", "/feedback/comments", nil, "", &comments)
		if err != nil {
			t.Fatalf("GET /feedback/comments: %v", err)
		}
		RequireStatus(t, resp, http.StatusOK)

		ct := resp.Header.Get("Content-Type")
		if !strings.HasPrefix(ct, "application/json") {
			t.Errorf("Content-Type = %q, want application/json", ct)
		}

		if len(comments) < 2 {
			t.Fatalf("expected at least 2 comments, got %d", len(comments))
		}

		for _, c := range comments {
			if c["app_name"] != "horostracker" {
				t.Errorf("app_name = %v, want horostracker", c["app_name"])
			}
		}
	})

	t.Run("ListJSON_AuthUserVisible", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		var comments []map[string]interface{}
		resp, err := h.JSON("GET", "/feedback/comments", nil, "", &comments)
		if err != nil {
			t.Fatalf("GET /feedback/comments: %v", err)
		}
		RequireStatus(t, resp, http.StatusOK)

		// At least one comment should have a non-empty user_id (from SubmitAuthenticated)
		found := false
		for _, c := range comments {
			uid, _ := c["user_id"].(string)
			if uid != "" {
				found = true
				break
			}
		}
		if !found {
			t.Error("expected at least one comment with a non-empty user_id")
		}
	})

	t.Run("ListPagination", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		var comments []map[string]interface{}
		resp, err := h.JSON("GET", "/feedback/comments?limit=1&offset=0", nil, "", &comments)
		if err != nil {
			t.Fatalf("GET: %v", err)
		}
		RequireStatus(t, resp, http.StatusOK)

		if len(comments) != 1 {
			t.Errorf("expected 1 comment with limit=1, got %d", len(comments))
		}
	})

	t.Run("ListPaginationOffset", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		// Get first page
		var page1 []map[string]interface{}
		resp, err := h.JSON("GET", "/feedback/comments?limit=1&offset=0", nil, "", &page1)
		if err != nil {
			t.Fatalf("GET page 1: %v", err)
		}
		RequireStatus(t, resp, http.StatusOK)

		// Get second page
		var page2 []map[string]interface{}
		resp, err = h.JSON("GET", "/feedback/comments?limit=1&offset=1", nil, "", &page2)
		if err != nil {
			t.Fatalf("GET page 2: %v", err)
		}
		RequireStatus(t, resp, http.StatusOK)

		if len(page1) == 0 || len(page2) == 0 {
			t.Skip("not enough comments to test offset")
		}

		// The two pages should return different comments
		if page1[0]["id"] == page2[0]["id"] {
			t.Error("offset=0 and offset=1 returned the same comment")
		}
	})

	t.Run("ListHTML", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		resp, err := h.Do("GET", "/feedback/comments.html", nil, "")
		if err != nil {
			t.Fatalf("GET /feedback/comments.html: %v", err)
		}
		RequireStatus(t, resp, http.StatusOK)

		ct := resp.Header.Get("Content-Type")
		if !strings.HasPrefix(ct, "text/html") {
			t.Errorf("Content-Type = %q, want text/html", ct)
		}

		body := bodyString(resp)
		if !strings.Contains(body, "Super produit") {
			t.Error("HTML should contain submitted comment text")
		}
		if !strings.Contains(body, "Commentaires") {
			t.Error("HTML should contain title 'Commentaires'")
		}
	})

	t.Run("WidgetJS", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		body, resp, err := h.RawBody("GET", "/feedback/widget.js", nil, "")
		if err != nil {
			t.Fatalf("GET /feedback/widget.js: %v", err)
		}
		RequireStatus(t, resp, http.StatusOK)

		ct := resp.Header.Get("Content-Type")
		if !strings.Contains(ct, "javascript") {
			t.Errorf("Content-Type = %q, want javascript", ct)
		}

		cc := resp.Header.Get("Cache-Control")
		if !strings.Contains(cc, "max-age") {
			t.Errorf("widget.js missing Cache-Control max-age, got %q", cc)
		}

		js := string(body)
		required := []string{"hfb-btn", "hfb-overlay", "/submit", "data-base"}
		for _, elem := range required {
			if !strings.Contains(js, elem) {
				t.Errorf("widget.js missing required element: %q", elem)
			}
		}
	})

	t.Run("WidgetCSS", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		body, resp, err := h.RawBody("GET", "/feedback/widget.css", nil, "")
		if err != nil {
			t.Fatalf("GET /feedback/widget.css: %v", err)
		}
		RequireStatus(t, resp, http.StatusOK)

		ct := resp.Header.Get("Content-Type")
		if !strings.Contains(ct, "css") {
			t.Errorf("Content-Type = %q, want css", ct)
		}

		cc := resp.Header.Get("Cache-Control")
		if !strings.Contains(cc, "max-age") {
			t.Errorf("widget.css missing Cache-Control max-age, got %q", cc)
		}

		css := string(body)
		required := []string{".hfb-btn", ".hfb-overlay", "z-index", "position"}
		for _, elem := range required {
			if !strings.Contains(css, elem) {
				t.Errorf("widget.css missing required style: %q", elem)
			}
		}
	})

	t.Run("XSSPrevention", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		// Submit XSS payloads
		h.Do("POST", "/feedback/submit", map[string]string{
			"text":     XSSScript,
			"page_url": "http://localhost/",
		}, "")

		resp, _ := h.Do("GET", "/feedback/comments.html", nil, "")
		body := bodyString(resp)

		if strings.Contains(body, "<script>alert") {
			t.Error("HTML contains unescaped <script> tag — XSS vulnerability")
		}
	})

	t.Run("XSSMultiVector", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		h.Do("POST", "/feedback/submit", map[string]string{
			"text":     XSSMulti,
			"page_url": "http://localhost/",
		}, "")

		resp, _ := h.Do("GET", "/feedback/comments.html", nil, "")
		body := bodyString(resp)

		// Check for unescaped HTML tags (the attribute names may appear inside escaped text)
		if strings.Contains(body, "<img ") {
			t.Error("HTML contains unescaped <img> tag — XSS vulnerability")
		}
		if strings.Contains(body, "<div onmouseover") {
			t.Error("HTML contains unescaped <div> with event handler — XSS vulnerability")
		}
	})

	t.Run("DBRowExists", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		if submittedID == "" {
			t.Skip("no submitted ID from SubmitAnonymous")
		}
		dba.AssertRowCountGTE(t, "feedback_comments", "id = ?", []interface{}{submittedID}, 1)
	})

	t.Run("NotFoundUnknownPath", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		resp, err := h.Do("GET", "/feedback/nonexistent", nil, "")
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		resp.Body.Close()

		// SPA catch-all (GET /) may return 200 with index.html — that's acceptable.
		// The important thing is the server doesn't crash and doesn't return feedback data.
		if resp.StatusCode == http.StatusInternalServerError {
			t.Error("unknown feedback path caused internal server error")
		}
		t.Logf("Unknown feedback path: status %d (server did not crash)", resp.StatusCode)
	})

	// --- Abuse tests ---

	t.Run("Abuse_SQLiInText", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		var result map[string]string
		resp, err := h.JSON("POST", "/feedback/submit", map[string]string{
			"text":     SQLiBasic,
			"page_url": "http://localhost/",
		}, "", &result)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}

		// Must not crash — 200 (stored as text) is acceptable
		if resp.StatusCode != http.StatusOK {
			t.Errorf("SQLi in text: expected 200, got %d", resp.StatusCode)
		}
		if result["status"] != "ok" {
			t.Errorf("status = %q, want ok", result["status"])
		}
		t.Logf("SQLi in text: status %d (server did not crash, payload stored safely)", resp.StatusCode)
	})

	t.Run("Abuse_SQLiUnionInText", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		var result map[string]string
		resp, err := h.JSON("POST", "/feedback/submit", map[string]string{
			"text":     SQLiUnion,
			"page_url": "http://localhost/",
		}, "", &result)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		if resp.StatusCode != http.StatusOK {
			t.Errorf("SQLi union in text: expected 200, got %d", resp.StatusCode)
		}
		t.Logf("SQLi union in text: status %d (server did not crash)", resp.StatusCode)
	})

	t.Run("Abuse_SQLiInPageURL", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		resp, err := h.Do("POST", "/feedback/submit", map[string]string{
			"text":     "normal text",
			"page_url": SQLiBasic,
		}, "")
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("SQLi in page_url: expected 200, got %d", resp.StatusCode)
		}
		t.Logf("SQLi in page_url: status %d (server did not crash)", resp.StatusCode)
	})

	t.Run("Abuse_NullByteInText", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		resp, err := h.Do("POST", "/feedback/submit", map[string]string{
			"text":     NullByte,
			"page_url": "http://localhost/",
		}, "")
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		resp.Body.Close()

		// Should accept without crashing
		if resp.StatusCode == http.StatusInternalServerError {
			t.Error("null byte in text caused internal server error")
		}
		t.Logf("Null byte in text: status %d (server did not crash)", resp.StatusCode)
	})

	t.Run("Abuse_ControlCharsInText", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		resp, err := h.Do("POST", "/feedback/submit", map[string]string{
			"text":     ControlChars,
			"page_url": "http://localhost/",
		}, "")
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		resp.Body.Close()

		if resp.StatusCode == http.StatusInternalServerError {
			t.Error("control chars in text caused internal server error")
		}
		t.Logf("Control chars in text: status %d (server did not crash)", resp.StatusCode)
	})

	t.Run("Abuse_LargePayload", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		resp, err := h.Do("POST", "/feedback/submit", map[string]string{
			"text":     Body1MB,
			"page_url": "http://localhost/",
		}, "")
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		resp.Body.Close()

		// Should either truncate to 10000 chars or reject, but not crash
		if resp.StatusCode == http.StatusInternalServerError {
			t.Error("1 MB payload caused internal server error")
		}
		t.Logf("1 MB payload: status %d (server handled gracefully)", resp.StatusCode)

		// Verify text was truncated in DB if stored
		if resp.StatusCode == http.StatusOK {
			db, err := dba.nodes()
			if err != nil {
				t.Fatalf("opening nodes.db: %v", err)
			}
			var textLen int
			row := db.QueryRow(`SELECT LENGTH(text) FROM feedback_comments ORDER BY created_at DESC LIMIT 1`)
			if err := row.Scan(&textLen); err != nil {
				t.Fatalf("querying text length: %v", err)
			}
			if textLen > 10001 {
				t.Errorf("text length = %d, expected <= 10000 (truncation)", textLen)
			}
		}
	})

	t.Run("Abuse_XSSInPageURL", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		h.Do("POST", "/feedback/submit", map[string]string{
			"text":     "feedback with XSS page_url",
			"page_url": `javascript:alert('xss')`,
		}, "")

		resp, _ := h.Do("GET", "/feedback/comments.html", nil, "")
		body := bodyString(resp)

		// The page_url should NOT be rendered as a clickable link (no href with javascript:)
		if strings.Contains(body, `href="javascript:`) {
			t.Error("HTML contains javascript: URI in href attribute — XSS vulnerability")
		}
	})

	t.Run("Abuse_EmptyBody", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		resp, err := h.Do("POST", "/feedback/submit", nil, "")
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		resp.Body.Close()

		if resp.StatusCode != http.StatusBadRequest {
			t.Logf("SECURITY NOTE: empty body returned %d instead of 400", resp.StatusCode)
		}
		t.Logf("Empty body: status %d (server did not crash)", resp.StatusCode)
	})

	t.Run("Abuse_WrongMethod", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		// PUT on submit should not work
		resp, err := h.Do("PUT", "/feedback/submit", map[string]string{
			"text":     "test",
			"page_url": "http://localhost/",
		}, "")
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		resp.Body.Close()

		if resp.StatusCode == http.StatusOK {
			t.Error("PUT on /feedback/submit should not return 200")
		}
	})
}
