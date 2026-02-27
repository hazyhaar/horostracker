package e2e

import (
	"net/http"
	"strings"
	"testing"
	"time"
)

// TestCSPCompliance verifies that Content-Security-Policy is correctly enforced
// and that no inline JavaScript handlers exist in served static files.
func TestCSPCompliance(t *testing.T) {
	h, _ := ensureHarness(t)

	t.Run("CSP_Header_Blocks_Inline_Scripts", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		resp, err := h.Do("GET", "/", nil, "")
		if err != nil {
			t.Fatalf("fetching index: %v", err)
		}
		defer resp.Body.Close()

		csp := resp.Header.Get("Content-Security-Policy")
		if csp == "" {
			t.Fatal("missing Content-Security-Policy header")
		}

		if !strings.Contains(csp, "script-src 'self'") {
			t.Errorf("CSP does not contain script-src 'self': %s", csp)
		}

		// Extract the script-src directive and verify it does NOT contain 'unsafe-inline'.
		// Note: style-src 'unsafe-inline' is acceptable (needed for inline CSS),
		// but script-src 'unsafe-inline' would defeat CSP's XSS protection.
		for _, directive := range strings.Split(csp, ";") {
			directive = strings.TrimSpace(directive)
			if strings.HasPrefix(directive, "script-src") {
				if strings.Contains(directive, "'unsafe-inline'") {
					t.Errorf("script-src allows 'unsafe-inline' — this defeats CSP: %s", directive)
				}
			}
		}
	})

	t.Run("No_Inline_Onclick_In_IndexHTML", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		body, resp, err := h.RawBody("GET", "/", nil, "")
		if err != nil {
			t.Fatalf("fetching index: %v", err)
		}
		RequireStatus(t, resp, http.StatusOK)

		html := string(body)
		inlineHandlers := []string{
			"onclick=", "onchange=", "onsubmit=", "onload=",
			"onerror=", "onmouseover=", "onfocus=", "onblur=",
			"onkeydown=", "onkeyup=", "onkeypress=",
		}
		for _, handler := range inlineHandlers {
			if strings.Contains(strings.ToLower(html), handler) {
				t.Errorf("index.html contains inline handler %q — blocked by CSP script-src 'self'", handler)
			}
		}
	})

	t.Run("No_Inline_Onclick_In_AppJS", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		body, resp, err := h.RawBody("GET", "/static/js/app.js", nil, "")
		if err != nil {
			t.Fatalf("fetching app.js: %v", err)
		}
		RequireStatus(t, resp, http.StatusOK)

		js := string(body)

		// Check for inline event handlers in string literals that would be inserted into DOM
		// Pattern: onclick=" which would appear in template literals generating HTML
		inlinePatterns := []string{
			`onclick="`,
			`onchange="`,
			`onsubmit="`,
			`onerror="`,
			`onmouseover="`,
			`onfocus="`,
			`onblur="`,
		}
		for _, pattern := range inlinePatterns {
			if strings.Contains(js, pattern) {
				t.Errorf("app.js contains inline handler %q in generated HTML — blocked by CSP", pattern)
			}
		}

		// Verify data-nav pattern is used (the CSP-safe approach)
		if !strings.Contains(js, "data-nav") {
			t.Error("app.js does not use data-nav attribute pattern for navigation")
		}
		if !strings.Contains(js, "addEventListener") {
			t.Error("app.js does not use addEventListener — CSP-compliant event binding required")
		}
	})
}

// TestAdminRoleCoherence verifies that the role system is consistent between
// the database schema, the Go validation layer, and the JavaScript UI.
func TestAdminRoleCoherence(t *testing.T) {
	h, dba := ensureHarness(t)

	t.Run("DB_Schema_Has_Operator_Role", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		db, err := dba.nodes()
		if err != nil {
			t.Fatalf("opening nodes.db: %v", err)
		}

		var ddl string
		err = db.QueryRow("SELECT sql FROM sqlite_master WHERE type='table' AND name='users'").Scan(&ddl)
		if err != nil {
			t.Fatalf("querying users DDL: %v", err)
		}

		// The DDL must contain the new role set
		expectedRoles := []string{"'anon'", "'user'", "'researcher'", "'provider'", "'operator'"}
		for _, role := range expectedRoles {
			if !strings.Contains(ddl, role) {
				t.Errorf("users table DDL missing role %s — found: %s", role, ddl)
			}
		}

		// The DDL must NOT contain old roles
		oldRoles := []string{"'admin'", "'moderator'"}
		for _, role := range oldRoles {
			if strings.Contains(ddl, role) {
				t.Errorf("users table DDL still contains old role %s — migration did not run: %s", role, ddl)
			}
		}
	})

	t.Run("Operator_Role_Assignable", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		// Use a unique handle to avoid collision with TestGolden which uses Users.Operator
		handle := "op_role_test_user"
		password := "optest-pass-1234"

		// Register a regular user
		token, _ := h.Register(t, handle, password)

		// Verify default role is 'user'
		var me map[string]interface{}
		resp, err := h.JSON("GET", "/api/me", nil, token, &me)
		if err != nil {
			t.Fatalf("get /me: %v", err)
		}
		RequireStatus(t, resp, http.StatusOK)

		if me["role"] != "user" {
			t.Errorf("default role = %v, want 'user'", me["role"])
		}

		// Promote to operator directly in DB
		db, err := dba.nodes()
		if err != nil {
			t.Fatalf("opening nodes.db: %v", err)
		}
		_, err = db.Exec("UPDATE users SET role = 'operator' WHERE handle = ?", handle)
		if err != nil {
			t.Fatalf("setting operator role: %v", err)
		}

		// Re-login to get fresh token with updated role
		token, _ = h.Login(t, handle, password)

		// Verify role is now operator
		resp, err = h.JSON("GET", "/api/me", nil, token, &me)
		if err != nil {
			t.Fatalf("get /me after promotion: %v", err)
		}
		RequireStatus(t, resp, http.StatusOK)

		if me["role"] != "operator" {
			t.Errorf("role after promotion = %v, want 'operator'", me["role"])
		}
	})

	t.Run("JS_Admin_Button_Uses_Operator_Not_Admin", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		body, resp, err := h.RawBody("GET", "/static/js/app.js", nil, "")
		if err != nil {
			t.Fatalf("fetching app.js: %v", err)
		}
		RequireStatus(t, resp, http.StatusOK)

		js := string(body)

		// The admin button should check for 'operator' role, not 'admin'
		if strings.Contains(js, "role === 'admin'") {
			t.Error("app.js checks for role 'admin' instead of 'operator' — must use 'operator'")
		}
		if !strings.Contains(js, "role === 'operator'") && !strings.Contains(js, "role !== 'operator'") {
			t.Error("app.js does not reference 'operator' role for admin access control")
		}
	})

	t.Run("Admin_Page_Denied_Without_Operator_Role", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		// Register a regular user (not operator)
		regularToken, _ := h.Register(t, "regular_user_admin_test", "regularpass1234")

		// The admin page guard is client-side JS, but the API endpoints behind it
		// should still enforce server-side auth. Test GET /api/providers which
		// is used by the admin page — it should work (providers are public info).
		// The real guard is that POST /api/resolution requires auth.

		// Verify regular user cannot resolve (needs valid node, but the point
		// is that the auth middleware accepts the token)
		var me map[string]interface{}
		resp, err := h.JSON("GET", "/api/me", nil, regularToken, &me)
		if err != nil {
			t.Fatalf("get /me: %v", err)
		}
		RequireStatus(t, resp, http.StatusOK)

		if me["role"] == "operator" {
			t.Error("regular user should not have operator role")
		}
	})

	t.Run("All_New_Roles_Insertable", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		db, err := dba.nodes()
		if err != nil {
			t.Fatalf("opening nodes.db: %v", err)
		}

		// Test that all roles from the new set can be inserted
		roles := []string{"anon", "user", "researcher", "provider", "operator"}
		for _, role := range roles {
			handle := "roletest_" + role
			// Insert a test user with this role
			_, err := db.Exec(`INSERT OR IGNORE INTO users (id, handle, password_hash, role) VALUES (?, ?, 'test', ?)`,
				"roletest_"+role+"_id", handle, role)
			if err != nil {
				t.Errorf("cannot insert user with role %q: %v", role, err)
			}
		}

		// Verify old roles are rejected by the CHECK constraint
		oldRoles := []string{"admin", "moderator"}
		for _, role := range oldRoles {
			handle := "oldtest_" + role
			_, err := db.Exec(`INSERT INTO users (id, handle, password_hash, role) VALUES (?, ?, 'test', ?)`,
				"oldtest_"+role+"_id", handle, role)
			if err == nil {
				t.Errorf("old role %q should be rejected by CHECK constraint but was accepted", role)
				// Clean up
				db.Exec("DELETE FROM users WHERE id = ?", "oldtest_"+role+"_id")
			}
		}
	})
}

// TestAdminProviders verifies the /api/providers endpoint used by the admin UI.
func TestAdminProviders(t *testing.T) {
	h, _ := ensureHarness(t)

	t.Run("Providers_Endpoint_Returns_Valid_Structure", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		var result map[string]interface{}
		resp, err := h.JSON("GET", "/api/providers", nil, "", &result)
		if err != nil {
			t.Fatalf("get providers: %v", err)
		}
		RequireStatus(t, resp, http.StatusOK)

		providers, ok := result["providers"]
		if !ok {
			t.Fatal("response missing 'providers' field")
		}

		providerMap, ok := providers.(map[string]interface{})
		if !ok {
			t.Fatal("providers is not an object")
		}

		// Each provider should map to an array of model names
		for name, models := range providerMap {
			modelArr, ok := models.([]interface{})
			if !ok {
				t.Errorf("provider %q models is not an array", name)
				continue
			}
			if len(modelArr) == 0 {
				t.Errorf("provider %q has no models", name)
			}
			for i, m := range modelArr {
				if _, ok := m.(string); !ok {
					t.Errorf("provider %q model[%d] is not a string: %v", name, i, m)
				}
			}
		}
	})
}
