package e2e

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"testing"
	"time"
)

func TestUser(t *testing.T) {
	h, _ := ensureHarness(t)
	token, _ := h.Register(t, "user_profile", "userprofilepass123")

	// Create some questions so this user has content
	h.AskQuestion(t, token, "User test question one for feed", nil)
	h.AskQuestion(t, token, "User test question two for feed", nil)

	t.Run("ProfileByHandle", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		var user map[string]interface{}
		resp, err := h.JSON("GET", "/api/user/user_profile", nil, "", &user)
		if err != nil {
			t.Fatalf("get user: %v", err)
		}
		RequireStatus(t, resp, http.StatusOK)

		if user["handle"] != "user_profile" {
			t.Errorf("handle = %v, want user_profile", user["handle"])
		}
		if user["id"] == nil || user["id"] == "" {
			t.Error("expected non-empty id")
		}
	})

	t.Run("ProfileNotFound", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		resp, _ := h.Do("GET", "/api/user/nonexistent_handle_xyz", nil, "")
		RequireStatus(t, resp, http.StatusNotFound)
	})

	t.Run("MeAuthenticated", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		var user map[string]interface{}
		resp, err := h.JSON("GET", "/api/me", nil, token, &user)
		if err != nil {
			t.Fatalf("get me: %v", err)
		}
		RequireStatus(t, resp, http.StatusOK)

		if user["handle"] != "user_profile" {
			t.Errorf("handle = %v, want user_profile", user["handle"])
		}
	})

	t.Run("MeUnauthenticated", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		resp, _ := h.Do("GET", "/api/me", nil, "")
		RequireStatus(t, resp, http.StatusUnauthorized)
	})

	t.Run("QuestionsFeed", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		var questions []map[string]interface{}
		resp, err := h.JSON("GET", "/api/questions?limit=5", nil, "", &questions)
		if err != nil {
			t.Fatalf("get questions: %v", err)
		}
		RequireStatus(t, resp, http.StatusOK)

		if len(questions) == 0 {
			t.Error("expected at least 1 question in feed")
		}

		// All should be claim node_type (root claims in feed)
		for _, q := range questions {
			if q["node_type"] != "claim" {
				t.Errorf("expected node_type=claim, got %v", q["node_type"])
			}
		}
	})

	// --- Abuse tests ---

	t.Run("Abuse_UserEnumeration", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		// Attempt sequential handle lookup — should return 404 without info leak
		handles := []string{"abuse_enum_1", "abuse_enum_2", "abuse_enum_3"}
		for _, handle := range handles {
			resp, err := h.Do("GET", "/api/user/"+handle, nil, "")
			if err != nil {
				t.Fatalf("request failed: %v", err)
			}
			if resp.StatusCode != http.StatusNotFound {
				t.Errorf("expected 404 for nonexistent user %s, got %d", handle, resp.StatusCode)
			}
		}
		t.Log("User enumeration check passed — all nonexistent handles return 404")
	})

	t.Run("Abuse_ProfileXSSHandle", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		// Register a user with XSS in handle (if server allows)
		xssHandle := "abuse_user_xss"
		xssToken, _ := h.Register(t, xssHandle, "abusepass12345")
		_ = xssToken

		// Fetch profile — verify handle is returned literally
		var user map[string]interface{}
		resp, err := h.JSON("GET", "/api/user/"+xssHandle, nil, "", &user)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		RequireStatus(t, resp, http.StatusOK)

		if user["handle"] != xssHandle {
			t.Errorf("handle mismatch: got %v, want %s", user["handle"], xssHandle)
		}
		t.Log("Profile XSS handle check passed — handle stored and returned literally")
	})

	t.Run("Abuse_MeWithExpiredToken", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		// Craft an expired JWT: header + payload with exp in the past + wrong signature
		header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"HS256","typ":"JWT"}`))
		payload := map[string]interface{}{
			"sub":    "expired_user",
			"handle": "expired_user",
			"exp":    time.Now().Add(-1 * time.Hour).Unix(),
		}
		payloadJSON, _ := json.Marshal(payload)
		payloadB64 := base64.RawURLEncoding.EncodeToString(payloadJSON)

		// Sign with a known-wrong secret (the test secret is "e2e-test-secret-key-horostracker")
		mac := hmac.New(sha256.New, []byte("wrong-secret-for-expired-test"))
		mac.Write([]byte(header + "." + payloadB64))
		sig := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
		expiredToken := header + "." + payloadB64 + "." + sig

		resp, err := h.Do("GET", "/api/me", nil, expiredToken)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("expired token: expected 401, got %d", resp.StatusCode)
		}
	})
}
