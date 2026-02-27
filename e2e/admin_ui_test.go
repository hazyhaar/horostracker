package e2e

import (
	"net/http"
	"strings"
	"testing"
	"time"
)

// TestAdminUIFlow verifies the admin page workflow end-to-end:
// 1. The served app.js contains all required admin DOM elements
// 2. The tree API returns author_handle for display
// 3. Resolutions are accessible via public API
// 4. The admin tree section uses loadAdminTree with scrollIntoView
func TestAdminUIFlow(t *testing.T) {
	h, _ := ensureHarness(t)

	// Create test data: operator + question + answer + resolution
	opToken, _ := h.Register(t, "admin_ui_operator", "operator-pass-1234")

	// Promote to operator
	db, err := dba.nodes()
	if err != nil {
		t.Fatalf("opening nodes.db: %v", err)
	}
	_, err = db.Exec("UPDATE users SET role = 'operator' WHERE handle = 'admin_ui_operator'")
	if err != nil {
		t.Fatalf("promoting to operator: %v", err)
	}
	opToken, _ = h.Login(t, "admin_ui_operator", "operator-pass-1234")

	questionID := h.AskQuestion(t, opToken, "Admin UI test question", nil)
	answerID := h.AnswerNode(t, opToken, questionID, "Test answer for admin UI flow", "claim")
	_ = answerID

	t.Run("AppJS_Admin_Tree_Wrap_Present", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		body, resp, err := h.RawBody("GET", "/static/js/app.js", nil, "")
		if err != nil {
			t.Fatalf("fetching app.js: %v", err)
		}
		RequireStatus(t, resp, http.StatusOK)
		js := string(body)

		required := []string{
			"admin-tree-wrap",
			"admin-tree",
			"admin-bootstrap-btn",
			"loadAdminTree",
			"scrollIntoView",
			"renderAdminNodeTree",
			"checkResolutionStatus",
		}
		for _, elem := range required {
			if !strings.Contains(js, elem) {
				t.Errorf("app.js missing required element: %q", elem)
			}
		}
	})

	t.Run("AppJS_Has_Resolution_Display", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		body, resp, err := h.RawBody("GET", "/static/js/app.js", nil, "")
		if err != nil {
			t.Fatalf("fetching app.js: %v", err)
		}
		RequireStatus(t, resp, http.StatusOK)
		js := string(body)

		required := []string{
			"loadNodeResolutions",
			"resolutions-wrap",
			"resolution-card",
			"res-provider",
			"formatBody",
			"res-compact",
			"res-expanded",
			"extractResolutionTitle",
		}
		for _, elem := range required {
			if !strings.Contains(js, elem) {
				t.Errorf("app.js missing resolution display element: %q", elem)
			}
		}
	})

	t.Run("Tree_API_Returns_Author_Handle", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		var tree map[string]interface{}
		resp, err := h.JSON("GET", "/api/tree/"+questionID+"?depth=5", nil, "", &tree)
		if err != nil {
			t.Fatalf("get tree: %v", err)
		}
		RequireStatus(t, resp, http.StatusOK)

		authorHandle, ok := tree["author_handle"]
		if !ok {
			t.Fatal("tree root missing author_handle field")
		}
		if authorHandle != "admin_ui_operator" {
			t.Errorf("author_handle = %v, want 'admin_ui_operator'", authorHandle)
		}

		// Check children also have author_handle
		children, ok := tree["children"].([]interface{})
		if !ok || len(children) == 0 {
			t.Fatal("tree has no children")
		}
		child := children[0].(map[string]interface{})
		childHandle, ok := child["author_handle"]
		if !ok {
			t.Error("child node missing author_handle field")
		}
		if childHandle != "admin_ui_operator" {
			t.Errorf("child author_handle = %v, want 'admin_ui_operator'", childHandle)
		}
	})

	t.Run("Resolution_API_Public_Access", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		// Resolution models endpoint should work without auth
		var result map[string]interface{}
		resp, err := h.JSON("GET", "/api/resolution/"+questionID+"/models", nil, "", &result)
		if err != nil {
			t.Fatalf("get resolution models: %v", err)
		}
		RequireStatus(t, resp, http.StatusOK)

		// count should be a number (0 is OK for test data without LLM)
		if _, ok := result["count"]; !ok {
			t.Error("response missing 'count' field")
		}
		if _, ok := result["models"]; !ok {
			t.Error("response missing 'models' field")
		}
	})

	t.Run("Admin_Question_Cards_Use_DataQid", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		body, resp, err := h.RawBody("GET", "/static/js/app.js", nil, "")
		if err != nil {
			t.Fatalf("fetching app.js: %v", err)
		}
		RequireStatus(t, resp, http.StatusOK)
		js := string(body)

		// Admin question cards must use data-qid for click handling
		if !strings.Contains(js, `data-qid="${q.id}"`) && !strings.Contains(js, "data-qid=") {
			t.Error("admin question cards missing data-qid attribute")
		}

		// The onclick must call loadAdminTree, not navigate
		if !strings.Contains(js, "loadAdminTree(card.dataset.qid)") {
			t.Error("admin card click does not call loadAdminTree")
		}
	})

	t.Run("NoCacheHeaders_On_Static_Files", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		resp, err := h.Do("GET", "/static/js/app.js", nil, "")
		if err != nil {
			t.Fatalf("fetching app.js: %v", err)
		}
		defer resp.Body.Close()

		cc := resp.Header.Get("Cache-Control")
		if !strings.Contains(cc, "no-cache") {
			t.Errorf("static files missing no-cache header, got: %q", cc)
		}
	})

	t.Run("AppJS_Has_ViewMode_Switcher", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		body, resp, err := h.RawBody("GET", "/static/js/app.js", nil, "")
		if err != nil {
			t.Fatalf("fetching app.js: %v", err)
		}
		RequireStatus(t, resp, http.StatusOK)
		js := string(body)

		required := []string{
			"view-mode-switcher",
			"view-mode-btn",
			"currentViewMode",
			"renderFishbone",
			"renderCurrentMode",
		}
		for _, elem := range required {
			if !strings.Contains(js, elem) {
				t.Errorf("app.js missing view mode element: %q", elem)
			}
		}
	})

	t.Run("AppJS_Has_Collapsible_Nodes", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		body, resp, err := h.RawBody("GET", "/static/js/app.js", nil, "")
		if err != nil {
			t.Fatalf("fetching app.js: %v", err)
		}
		RequireStatus(t, resp, http.StatusOK)
		js := string(body)

		required := []string{
			"node-header-compact",
			"node-expand-btn",
			"node-collapse-btn",
			"node-expanded",
			"extractNodeTitle",
			"countDescendants",
			"node-title-text",
			"node-child-count",
		}
		for _, elem := range required {
			if !strings.Contains(js, elem) {
				t.Errorf("app.js missing collapsible node element: %q", elem)
			}
		}
	})

	t.Run("AppJS_Has_Fishbone_Components", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		body, resp, err := h.RawBody("GET", "/static/js/app.js", nil, "")
		if err != nil {
			t.Fatalf("fetching app.js: %v", err)
		}
		RequireStatus(t, resp, http.StatusOK)
		js := string(body)

		required := []string{
			"fb-head",
			"fb-spine",
			"fb-pair",
			"fb-branch",
			"fb-card",
			"fb-connector",
			"fb-dot",
			"buildFishboneBranch",
		}
		for _, elem := range required {
			if !strings.Contains(js, elem) {
				t.Errorf("app.js missing fishbone element: %q", elem)
			}
		}
	})

	t.Run("CSS_Has_Fishbone_Styles", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		body, resp, err := h.RawBody("GET", "/static/css/style.css", nil, "")
		if err != nil {
			t.Fatalf("fetching style.css: %v", err)
		}
		RequireStatus(t, resp, http.StatusOK)
		css := string(body)

		required := []string{
			".view-mode-switcher",
			".node-header-compact",
			".node-expand-btn",
			".fishbone",
			".fb-spine",
			".fb-card",
			".fb-connector",
		}
		for _, elem := range required {
			if !strings.Contains(css, elem) {
				t.Errorf("style.css missing required style: %q", elem)
			}
		}
	})

	t.Run("AppJS_Has_Ombilical_Components", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		body, resp, err := h.RawBody("GET", "/static/js/app.js", nil, "")
		if err != nil {
			t.Fatalf("fetching app.js: %v", err)
		}
		RequireStatus(t, resp, http.StatusOK)
		js := string(body)

		required := []string{
			"classifyChildren",
			"renderOmbilical",
			"omb-bubble",
			"expandBubble",
			"omb-parent-card",
			"omb-columns",
			"wireOmbilicalActions",
		}
		for _, elem := range required {
			if !strings.Contains(js, elem) {
				t.Errorf("app.js missing ombilical component: %q", elem)
			}
		}
	})

	t.Run("CSS_Has_Ombilical_Styles", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		body, resp, err := h.RawBody("GET", "/static/css/style.css", nil, "")
		if err != nil {
			t.Fatalf("fetching style.css: %v", err)
		}
		RequireStatus(t, resp, http.StatusOK)
		css := string(body)

		required := []string{
			".omb-parent-card",
			".omb-columns",
			".omb-bubble",
			".omb-column-header",
			".omb-bubble-card",
			".omb-bubble-expanded",
			".omb-subtree",
		}
		for _, elem := range required {
			if !strings.Contains(css, elem) {
				t.Errorf("style.css missing ombilical style: %q", elem)
			}
		}
	})

	t.Run("AppJS_Has_Proof_Tree_Mode", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		body, resp, err := h.RawBody("GET", "/static/js/app.js", nil, "")
		if err != nil {
			t.Fatalf("fetching app.js: %v", err)
		}
		RequireStatus(t, resp, http.StatusOK)
		js := string(body)

		if !strings.Contains(js, `data-mode="ombilical"`) {
			t.Error("app.js missing data-mode=\"ombilical\" attribute")
		}
		if !strings.Contains(js, "Proof Tree") {
			t.Error("app.js missing 'Proof Tree' button label")
		}
		if !strings.Contains(js, `currentViewMode === 'ombilical'`) {
			t.Error("app.js missing ombilical mode branch in renderCurrentMode")
		}
	})

	t.Run("AppJS_ReplyBox_Offers_Piece_Claim", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		body, resp, err := h.RawBody("GET", "/static/js/app.js", nil, "")
		if err != nil {
			t.Fatalf("fetching app.js: %v", err)
		}
		RequireStatus(t, resp, http.StatusOK)
		js := string(body)

		if !strings.Contains(js, `value="claim"`) {
			t.Error("reply-box missing claim option")
		}
		if !strings.Contains(js, `value="piece"`) {
			t.Error("reply-box missing piece option")
		}
		// Legacy types should not appear
		if strings.Contains(js, `value="answer"`) {
			t.Error("reply-box still contains legacy 'answer' option")
		}
		if strings.Contains(js, `value="evidence"`) {
			t.Error("reply-box still contains legacy 'evidence' option")
		}
	})

	t.Run("CSS_Has_Piece_Claim_Badges", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		body, resp, err := h.RawBody("GET", "/static/css/style.css", nil, "")
		if err != nil {
			t.Fatalf("fetching style.css: %v", err)
		}
		RequireStatus(t, resp, http.StatusOK)
		css := string(body)

		if !strings.Contains(css, ".node-type-badge.claim") {
			t.Error("style.css missing .node-type-badge.claim")
		}
		if !strings.Contains(css, ".node-type-badge.piece") {
			t.Error("style.css missing .node-type-badge.piece")
		}
		if !strings.Contains(css, ".unsourced-badge") {
			t.Error("style.css missing .unsourced-badge")
		}
		// Legacy badge classes should not appear
		if strings.Contains(css, ".node-type-badge.question") {
			t.Error("style.css still contains legacy .node-type-badge.question")
		}
		if strings.Contains(css, ".node-type-badge.answer") {
			t.Error("style.css still contains legacy .node-type-badge.answer")
		}
	})

	t.Run("AppJS_Has_Unsourced_Badge", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		body, resp, err := h.RawBody("GET", "/static/js/app.js", nil, "")
		if err != nil {
			t.Fatalf("fetching app.js: %v", err)
		}
		RequireStatus(t, resp, http.StatusOK)
		js := string(body)

		if !strings.Contains(js, "unsourced-badge") {
			t.Error("app.js missing unsourced-badge class")
		}
		if !strings.Contains(js, "isUnsourcedClaim") {
			t.Error("app.js missing isUnsourcedClaim function")
		}
	})

	t.Run("API_Answer_Accepts_Piece_Claim", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		// Create a piece child node
		pieceID := h.AnswerNode(t, opToken, questionID, "Factual evidence document", "piece")
		if pieceID == "" {
			t.Fatal("failed to create piece node")
		}

		// Create a claim child node
		claimID := h.AnswerNode(t, opToken, questionID, "This is a sub-claim", "claim")
		if claimID == "" {
			t.Fatal("failed to create claim node")
		}

		// Verify legacy types are rejected
		var errResult map[string]interface{}
		resp, _ := h.JSON("POST", "/api/answer", map[string]interface{}{
			"parent_id": questionID,
			"body":      "Legacy answer type",
			"node_type": "answer",
		}, opToken, &errResult)
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("legacy type 'answer' should be rejected, got status %d", resp.StatusCode)
		}
	})
}
