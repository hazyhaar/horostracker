package e2e

import (
	"net/http"
	"testing"
	"time"
)

func TestAuth(t *testing.T) {
	h, _ := ensureHarness(t)

	t.Run("RegisterSuccess", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		var result struct {
			User  map[string]interface{} `json:"user"`
			Token string                 `json:"token"`
		}
		resp, err := h.JSON("POST", "/api/register", map[string]string{
			"handle":   "auth_test_user",
			"password": "validpassword123",
		}, "", &result)
		if err != nil {
			t.Fatalf("register: %v", err)
		}
		RequireStatus(t, resp, http.StatusCreated)

		if result.Token == "" {
			t.Error("expected non-empty token")
		}
		if result.User["handle"] != "auth_test_user" {
			t.Errorf("handle = %v, want auth_test_user", result.User["handle"])
		}
		if result.User["id"] == nil || result.User["id"] == "" {
			t.Error("expected non-empty user id")
		}
	})

	t.Run("RegisterDuplicateHandle", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		// First register
		h.Register(t, "dup_handle_user", "password1234")

		// Duplicate
		resp, _ := h.Do("POST", "/api/register", map[string]string{
			"handle":   "dup_handle_user",
			"password": "password5678",
		}, "")
		RequireStatus(t, resp, http.StatusConflict)
	})

	t.Run("RegisterHandleTooShort", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		resp, _ := h.Do("POST", "/api/register", map[string]string{
			"handle":   "ab",
			"password": "validpassword",
		}, "")
		RequireStatus(t, resp, http.StatusBadRequest)
	})

	t.Run("RegisterPasswordTooShort", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		resp, _ := h.Do("POST", "/api/register", map[string]string{
			"handle":   "pwd_short_user",
			"password": "short",
		}, "")
		RequireStatus(t, resp, http.StatusBadRequest)
	})

	t.Run("LoginSuccess", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		h.Register(t, "login_user", "mypassword123")
		token, userID := h.Login(t, "login_user", "mypassword123")
		if token == "" {
			t.Error("expected non-empty token")
		}
		if userID == "" {
			t.Error("expected non-empty user ID")
		}
	})

	t.Run("LoginWrongPassword", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		h.Register(t, "wrong_pwd_user", "correctpass123")
		resp, _ := h.Do("POST", "/api/login", map[string]string{
			"handle":   "wrong_pwd_user",
			"password": "wrongpassword",
		}, "")
		RequireStatus(t, resp, http.StatusUnauthorized)
	})

	t.Run("LoginNonexistentUser", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		resp, _ := h.Do("POST", "/api/login", map[string]string{
			"handle":   "nonexistent_user_xyz",
			"password": "doesntmatter",
		}, "")
		RequireStatus(t, resp, http.StatusUnauthorized)
	})

	t.Run("ProtectedEndpointNoToken", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		resp, _ := h.Do("POST", "/api/ask", map[string]string{
			"body": "test question",
		}, "")
		RequireStatus(t, resp, http.StatusUnauthorized)
	})

	t.Run("ProtectedEndpointInvalidToken", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		resp, _ := h.Do("POST", "/api/ask", map[string]string{
			"body": "test question",
		}, "invalid.jwt.token")
		RequireStatus(t, resp, http.StatusUnauthorized)
	})

	// --- Abuse tests ---

	t.Run("Abuse_SQLiInHandle", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		resp, err := h.Do("POST", "/api/register", map[string]string{
			"handle":   SQLiBasic,
			"password": "validpassword123",
		}, "")
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		// Must not crash — 400 (validation) or 201 (accepted) are both OK
		if resp.StatusCode != http.StatusBadRequest && resp.StatusCode != http.StatusCreated {
			t.Logf("SECURITY NOTE: SQLi handle returned status %d", resp.StatusCode)
		}
		t.Logf("SQLi in handle: status %d (server did not crash)", resp.StatusCode)
	})

	t.Run("Abuse_SQLiInPassword", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		resp, err := h.Do("POST", "/api/register", map[string]string{
			"handle":   "abuse_auth_sqlipwd",
			"password": SQLiUnion,
		}, "")
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		if resp.StatusCode != http.StatusBadRequest && resp.StatusCode != http.StatusCreated {
			t.Logf("SECURITY NOTE: SQLi password returned status %d", resp.StatusCode)
		}
		t.Logf("SQLi in password: status %d (server did not crash)", resp.StatusCode)
	})

	t.Run("Abuse_XSSInHandle", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		resp, err := h.Do("POST", "/api/register", map[string]string{
			"handle":   "<script>alert(1)</script>",
			"password": "validpassword123",
		}, "")
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		RequireStatus(t, resp, http.StatusBadRequest)
		t.Log("XSS handle correctly rejected with 400")
	})

	t.Run("Abuse_HomoglyphImpersonation", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		// alice (Latin) was registered in golden_test; register homoglyph variant
		resp, err := h.Do("POST", "/api/register", map[string]string{
			"handle":   HomoglyphHandle,
			"password": "validpassword123",
		}, "")
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		RequireStatus(t, resp, http.StatusBadRequest)
		t.Log("Homoglyph handle correctly rejected with 400")
	})

	t.Run("Abuse_NullByteInHandle", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		resp, err := h.Do("POST", "/api/register", map[string]string{
			"handle":   "abuse_null\x00byte",
			"password": "validpassword123",
		}, "")
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		RequireStatus(t, resp, http.StatusBadRequest)
		t.Log("Null byte handle correctly rejected with 400")
	})

	t.Run("Abuse_JWTAlgNone", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		resp, err := h.Do("POST", "/api/ask", map[string]string{
			"body": "test question with forged token",
		}, JWTAlgNone)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("JWT alg=none: expected 401, got %d", resp.StatusCode)
		}
	})

	t.Run("Abuse_JWTWrongSecret", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		resp, err := h.Do("POST", "/api/ask", map[string]string{
			"body": "test question with wrong secret",
		}, JWTWrongSecret)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("JWT wrong secret: expected 401, got %d", resp.StatusCode)
		}
	})

	t.Run("Abuse_EmptyBody", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		resp, err := h.Do("POST", "/api/register", nil, "")
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		if resp.StatusCode != http.StatusBadRequest {
			t.Logf("SECURITY NOTE: empty body on register returned %d instead of 400", resp.StatusCode)
		}
		t.Logf("Empty body register: status %d (server did not crash)", resp.StatusCode)
	})
}
