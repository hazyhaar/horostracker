package e2e

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestSubtreeResolution(t *testing.T) {
	h, dba := ensureHarness(t)

	// --- Providers endpoint ---

	t.Run("GetProviders", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		var result map[string]interface{}
		resp, err := h.JSON("GET", "/api/providers", nil, "", &result)
		if err != nil {
			t.Fatalf("get providers: %v", err)
		}
		RequireStatus(t, resp, http.StatusOK)

		providers, ok := result["providers"].(map[string]interface{})
		if !ok {
			t.Fatal("expected providers to be an object")
		}

		// At least one provider should be configured (test harness sets up gemini/anthropic keys)
		if HasLLM() && len(providers) == 0 {
			t.Error("expected at least 1 provider when LLM keys are set")
		}

		// Each provider value should be an array of model strings
		for name, models := range providers {
			arr, ok := models.([]interface{})
			if !ok {
				t.Errorf("provider %s: expected array of models, got %T", name, models)
				continue
			}
			if len(arr) == 0 {
				t.Errorf("provider %s: expected at least 1 model", name)
			}
		}
	})

	// --- Resolution models endpoint (empty initially) ---

	t.Run("GetResolutionModels_Empty", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		token, _ := h.Register(t, "res_models_empty_user", "modelspass1234")
		questionID := h.AskQuestion(t, token, "What is the role of mitochondria in cellular respiration?", nil)

		var result map[string]interface{}
		resp, err := h.JSON("GET", "/api/resolution/"+questionID+"/models", nil, "", &result)
		if err != nil {
			t.Fatalf("get resolution models: %v", err)
		}
		RequireStatus(t, resp, http.StatusOK)

		count := result["count"].(float64)
		if count != 0 {
			t.Errorf("expected 0 resolutions for fresh node, got %v", count)
		}
	})

	// --- Unresolved nodes endpoint ---

	t.Run("GetUnresolved_RequiresAuth", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		token, _ := h.Register(t, "res_unres_auth_user", "unrespass1234")
		questionID := h.AskQuestion(t, token, "How do neural networks learn?", nil)

		// Without auth → 401
		resp, err := h.Do("GET", "/api/resolution/"+questionID+"/unresolved?provider=test&model=test", nil, "")
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		resp.Body.Close()
		RequireStatus(t, resp, http.StatusUnauthorized)
	})

	t.Run("GetUnresolved_RequiresParams", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		token, _ := h.Register(t, "res_unres_params_user", "paramspass1234")
		questionID := h.AskQuestion(t, token, "What causes tides?", nil)

		// Missing provider/model → 400
		resp, err := h.Do("GET", "/api/resolution/"+questionID+"/unresolved", nil, token)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		resp.Body.Close()
		RequireStatus(t, resp, http.StatusBadRequest)
	})

	t.Run("GetUnresolved_ReturnsNodes", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		token, _ := h.Register(t, "res_unres_list_user", "listpass1234")
		questionID := h.AskQuestion(t, token, "Is P equal to NP?", nil)
		h.AnswerNode(t, token, questionID, "Most researchers believe P != NP", "claim")
		h.AnswerNode(t, token, questionID, "There is no proof either way", "claim")

		var result map[string]interface{}
		resp, err := h.JSON("GET", "/api/resolution/"+questionID+"/unresolved?provider=test_provider&model=test_model", nil, token, &result)
		if err != nil {
			t.Fatalf("get unresolved: %v", err)
		}
		RequireStatus(t, resp, http.StatusOK)

		count := result["count"].(float64)
		if count < 1 {
			t.Errorf("expected at least 1 unresolved node, got %v", count)
		}
	})

	// --- Subtree resolution (requires LLM) ---

	t.Run("SubtreeResolution", func(t *testing.T) {
		if !HasLLM() {
			t.Skip("no LLM API keys set")
		}
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		token, _ := h.Register(t, "res_subtree_user", "subtreepass1234")
		questionID := h.AskQuestion(t, token, "Should governments regulate AI development?", nil)
		answerID := h.AnswerNode(t, token, questionID, "Regulation is necessary to prevent harm", "claim")
		h.AnswerNode(t, token, answerID, "Over-regulation stifles innovation", "claim")

		// Resolve the answer subtree
		var result map[string]interface{}
		resp, err := h.JSON("POST", "/api/resolution/"+answerID, map[string]interface{}{
			"subtree": true,
		}, token, &result)
		if err != nil {
			t.Fatalf("subtree resolution: %v", err)
		}
		RequireStatus(t, resp, http.StatusCreated)

		resolution, ok := result["resolution"].(map[string]interface{})
		if !ok {
			t.Fatal("expected resolution object in response")
		}
		if resolution["node_id"] != answerID {
			t.Errorf("resolution node_id = %v, want %v", resolution["node_id"], answerID)
		}
		if resolution["status"] != "completed" {
			t.Errorf("resolution status = %v, want completed", resolution["status"])
		}
		if resolution["content"] == nil || resolution["content"] == "" {
			t.Error("expected non-empty resolution content")
		}

		generation, ok := result["generation"].(map[string]interface{})
		if !ok {
			t.Fatal("expected generation object in response")
		}
		if generation["content"] == nil || generation["content"] == "" {
			t.Error("expected non-empty generation content")
		}

		// Verify in resolutions table via DB
		dba.AssertRowCountGTE(t, "resolutions", "node_id = ?", []interface{}{answerID}, 1)
	})

	t.Run("SubtreeResolution_RequiresAuth", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		token, _ := h.Register(t, "res_subtree_noauth", "noauthpass1234")
		questionID := h.AskQuestion(t, token, "What is thermodynamics?", nil)

		// Without auth → 401
		resp, err := h.Do("POST", "/api/resolution/"+questionID, map[string]interface{}{
			"subtree": true,
		}, "")
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		resp.Body.Close()
		RequireStatus(t, resp, http.StatusUnauthorized)
	})

	t.Run("SubtreeResolution_Upsert", func(t *testing.T) {
		if !HasLLM() {
			t.Skip("no LLM API keys set")
		}
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		token, _ := h.Register(t, "res_upsert_user", "upsertpass1234")
		questionID := h.AskQuestion(t, token, "What is the observer effect in quantum physics?", nil)
		h.AnswerNode(t, token, questionID, "Measurement disturbs the quantum state", "claim")

		// First resolution
		resp, err := h.JSON("POST", "/api/resolution/"+questionID, map[string]interface{}{
			"subtree": true,
		}, token, nil)
		if err != nil {
			t.Fatalf("first resolution: %v", err)
		}
		RequireStatus(t, resp, http.StatusCreated)

		// Second resolution on same node (should upsert, not duplicate)
		resp, err = h.JSON("POST", "/api/resolution/"+questionID, map[string]interface{}{
			"subtree": true,
		}, token, nil)
		if err != nil {
			t.Fatalf("second resolution: %v", err)
		}
		RequireStatus(t, resp, http.StatusCreated)

		// Check resolutions table: should still be 1 per provider/model triplet
		var models map[string]interface{}
		resp, err = h.JSON("GET", "/api/resolution/"+questionID+"/models", nil, "", &models)
		if err != nil {
			t.Fatalf("get models: %v", err)
		}
		RequireStatus(t, resp, http.StatusOK)

		count := models["count"].(float64)
		if count != 1 {
			t.Errorf("expected exactly 1 resolution after upsert, got %v", count)
		}
	})

	t.Run("SubtreeResolution_ModelsListing", func(t *testing.T) {
		if !HasLLM() {
			t.Skip("no LLM API keys set")
		}
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		token, _ := h.Register(t, "res_models_user", "modelslistpass1234")
		questionID := h.AskQuestion(t, token, "How does CRISPR-Cas9 work?", nil)
		h.AnswerNode(t, token, questionID, "CRISPR uses guide RNA to target specific DNA sequences", "claim")

		// Generate subtree resolution
		h.JSON("POST", "/api/resolution/"+questionID, map[string]interface{}{
			"subtree": true,
		}, token, nil)

		// List models
		var result map[string]interface{}
		resp, err := h.JSON("GET", "/api/resolution/"+questionID+"/models", nil, "", &result)
		if err != nil {
			t.Fatalf("get models: %v", err)
		}
		RequireStatus(t, resp, http.StatusOK)

		count := result["count"].(float64)
		if count < 1 {
			t.Error("expected at least 1 resolution in models listing")
		}

		modelsMap, ok := result["models"].(map[string]interface{})
		if !ok {
			t.Fatal("expected models to be an object")
		}
		if len(modelsMap) == 0 {
			t.Error("expected at least 1 provider/model key")
		}
	})

	// --- Batch resolution ---

	t.Run("BatchResolution_RequiresAuth", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		// Without auth → 401
		resp, err := h.Do("POST", "/api/resolution/batch", map[string]interface{}{
			"node_ids": []string{"fake-id"},
			"provider": "test",
			"model":    "test",
		}, "")
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		resp.Body.Close()
		RequireStatus(t, resp, http.StatusUnauthorized)
	})

	t.Run("BatchResolution_RequiresFields", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		token, _ := h.Register(t, "res_batch_fields_user", "batchpass1234")

		// Missing required fields → 400
		resp, err := h.Do("POST", "/api/resolution/batch", map[string]interface{}{
			"node_ids": []string{},
		}, token)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		resp.Body.Close()
		RequireStatus(t, resp, http.StatusBadRequest)
	})

	t.Run("BatchResolution_MaxNodes", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		token, _ := h.Register(t, "res_batch_max_user", "batchmaxpass1234")

		// 51 nodes → 400 (max 50)
		ids := make([]string, 51)
		for i := range ids {
			ids[i] = fmt.Sprintf("fake-id-%d", i)
		}

		resp, err := h.Do("POST", "/api/resolution/batch", map[string]interface{}{
			"node_ids": ids,
			"provider": "test",
			"model":    "test",
		}, token)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		resp.Body.Close()
		RequireStatus(t, resp, http.StatusBadRequest)
	})

	t.Run("BatchResolution_WithLLM", func(t *testing.T) {
		if !HasLLM() {
			t.Skip("no LLM API keys set")
		}
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		token, _ := h.Register(t, "res_batch_llm_user", "batchllmpass1234")
		q1 := h.AskQuestion(t, token, "What is the Halting Problem?", nil)
		h.AnswerNode(t, token, q1, "It proves some problems are undecidable", "claim")

		q2 := h.AskQuestion(t, token, "What is Gödel's incompleteness theorem?", nil)
		h.AnswerNode(t, token, q2, "No consistent system can prove all truths about arithmetic", "claim")

		var result map[string]interface{}
		resp, err := h.JSON("POST", "/api/resolution/batch", map[string]interface{}{
			"node_ids": []string{q1, q2},
			"provider": "",
			"model":    "",
		}, token, &result)
		if err != nil {
			t.Fatalf("batch resolution: %v", err)
		}
		RequireStatus(t, resp, http.StatusOK)

		total := result["total"].(float64)
		if total != 2 {
			t.Errorf("total = %v, want 2", total)
		}

		succeeded := result["succeeded"].(float64)
		failed := result["failed"].(float64)
		t.Logf("Batch result: %d succeeded, %d failed out of %d", int(succeeded), int(failed), int(total))

		if succeeded+failed != total {
			t.Errorf("succeeded(%v) + failed(%v) != total(%v)", succeeded, failed, total)
		}
	})

	// --- Anti-loop bot ---

	t.Run("BotAntiLoop_403OnBotNode", func(t *testing.T) {
		if !HasLLM() {
			t.Skip("no LLM API keys set")
		}
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		token, _ := h.Register(t, "bot_antiloop_user", "antilooppass1234")
		questionID := h.AskQuestion(t, token, "What is the Turing test and why does it matter?", nil)

		// First bot answer should succeed
		var result map[string]interface{}
		resp, err := h.JSON("POST", "/api/bot/answer/"+questionID, map[string]interface{}{}, token, &result)
		if err != nil {
			t.Fatalf("first bot answer: %v", err)
		}
		RequireStatus(t, resp, http.StatusCreated)

		botNode := result["node"].(map[string]interface{})
		botNodeID := botNode["id"].(string)

		// Try to make bot answer its own node → 403
		resp, err = h.Do("POST", "/api/bot/answer/"+botNodeID, map[string]interface{}{}, token)
		if err != nil {
			t.Fatalf("second bot answer: %v", err)
		}
		resp.Body.Close()
		RequireStatus(t, resp, http.StatusForbidden)
	})

	t.Run("BotAnswer_SuggestedQuestionsFooter", func(t *testing.T) {
		if !HasLLM() {
			t.Skip("no LLM API keys set")
		}
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		token, _ := h.Register(t, "bot_footer_user", "footerpass1234")
		questionID := h.AskQuestion(t, token, "How does photosynthesis work in plants?", nil)
		h.AnswerNode(t, token, questionID, "Plants use chlorophyll to capture sunlight", "claim")

		var result map[string]interface{}
		resp, err := h.JSON("POST", "/api/bot/answer/"+questionID, map[string]interface{}{}, token, &result)
		if err != nil {
			t.Fatalf("bot answer: %v", err)
		}
		RequireStatus(t, resp, http.StatusCreated)

		node := result["node"].(map[string]interface{})
		body := node["body"].(string)

		if !strings.Contains(body, "Questions suggérées") {
			t.Error("expected 'Questions suggérées' footer in bot response body")
		}
	})

	t.Run("BotAnswer_RequiresAuth", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		token, _ := h.Register(t, "bot_noauth_user", "botnoauth1234")
		questionID := h.AskQuestion(t, token, "What is gravity?", nil)

		// Without auth → 401
		resp, err := h.Do("POST", "/api/bot/answer/"+questionID, map[string]interface{}{}, "")
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		resp.Body.Close()
		RequireStatus(t, resp, http.StatusUnauthorized)
	})

	// --- Abuse tests ---

	t.Run("Abuse_SQLiInProvider", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		token, _ := h.Register(t, "abuse_sqli_prov_user", "sqliprovpass1234")
		questionID := h.AskQuestion(t, token, "What is database normalization?", nil)

		// SQL injection in provider field — should not crash the server
		resp, err := h.Do("POST", "/api/resolution/"+questionID, map[string]interface{}{
			"subtree":  true,
			"provider": SQLiBasic,
			"model":    SQLiUnion,
		}, token)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		resp.Body.Close()

		// The server should return an error (503 because no matching provider) but NOT crash
		if resp.StatusCode == 0 {
			t.Fatal("server did not respond — possible crash from SQL injection")
		}
		t.Logf("SQL injection in provider: status %d (server stable)", resp.StatusCode)
	})

	t.Run("Abuse_SQLiInUnresolved", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		token, _ := h.Register(t, "abuse_sqli_unres_user", "sqliunrespass1234")
		questionID := h.AskQuestion(t, token, "What is SQL injection?", nil)

		// SQL injection in query params
		resp, err := h.Do("GET", "/api/resolution/"+questionID+"/unresolved?provider="+SQLiBasic+"&model="+SQLiUnion, nil, token)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		resp.Body.Close()

		if resp.StatusCode == 0 {
			t.Fatal("server did not respond — possible crash from SQL injection")
		}
		t.Logf("SQL injection in unresolved params: status %d (server stable)", resp.StatusCode)
	})

	t.Run("Abuse_OversizedBatch", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		token, _ := h.Register(t, "abuse_batch_size_user", "batchsizepass1234")

		// Create a 1MB batch payload to test maxBody protection
		hugeIDs := make([]string, 50)
		for i := range hugeIDs {
			hugeIDs[i] = strings.Repeat("a", 20000) // Each ID is 20KB
		}

		resp, err := h.Do("POST", "/api/resolution/batch", map[string]interface{}{
			"node_ids": hugeIDs,
			"provider": "test",
			"model":    "test",
		}, token)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		resp.Body.Close()

		// Should be rejected (400 or 413) due to maxBody or invalid IDs
		if resp.StatusCode == http.StatusOK {
			t.Error("expected rejection for oversized batch payload")
		}
		t.Logf("Oversized batch: status %d", resp.StatusCode)
	})

	t.Run("Abuse_XSSInResolutionProvider", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		token, _ := h.Register(t, "abuse_xss_prov_user", "xssprovpass1234")
		questionID := h.AskQuestion(t, token, "What is cross-site scripting?", nil)

		resp, err := h.Do("POST", "/api/resolution/"+questionID, map[string]interface{}{
			"subtree":  true,
			"provider": XSSScript,
			"model":    XSSImg,
		}, token)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		resp.Body.Close()

		if resp.StatusCode == 0 {
			t.Fatal("server did not respond — possible crash from XSS payload")
		}
		t.Logf("XSS in provider/model: status %d (server stable)", resp.StatusCode)
	})

	t.Run("Abuse_FakeJWT_ResolutionRoutes", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		token, _ := h.Register(t, "abuse_jwt_res_user", "jwtrespass1234")
		questionID := h.AskQuestion(t, token, "What are JSON Web Tokens?", nil)

		// alg=none JWT → should be rejected
		resp, err := h.Do("POST", "/api/resolution/"+questionID, map[string]interface{}{
			"subtree": true,
		}, JWTAlgNone)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		resp.Body.Close()
		RequireStatus(t, resp, http.StatusUnauthorized)

		// Wrong-secret JWT → should be rejected
		resp, err = h.Do("POST", "/api/resolution/"+questionID, map[string]interface{}{
			"subtree": true,
		}, JWTWrongSecret)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		resp.Body.Close()
		RequireStatus(t, resp, http.StatusUnauthorized)
	})

	t.Run("Abuse_FakeJWT_BatchRoute", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		resp, err := h.Do("POST", "/api/resolution/batch", map[string]interface{}{
			"node_ids": []string{"id1"},
			"provider": "test",
			"model":    "test",
		}, JWTAlgNone)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		resp.Body.Close()
		RequireStatus(t, resp, http.StatusUnauthorized)
	})

	t.Run("Abuse_FakeJWT_UnresolvedRoute", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		resp, err := h.Do("GET", "/api/resolution/fake-id/unresolved?provider=x&model=y", nil, JWTAlgNone)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		resp.Body.Close()
		RequireStatus(t, resp, http.StatusUnauthorized)
	})

	// --- Rate limiting on new routes ---

	t.Run("RateLimit_BatchPost", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		token, _ := h.Register(t, "ratelim_batch_user", "ratelimbatchpass1234")

		// The rate limit for POST /api/resolution/batch is 10 per 3600s.
		// Send 15 rapid requests — at least some should be rate-limited.
		limited := 0
		for i := 0; i < 15; i++ {
			resp, err := h.Do("POST", "/api/resolution/batch", map[string]interface{}{
				"node_ids": []string{"fake-id"},
				"provider": "test",
				"model":    "test",
			}, token)
			if err != nil {
				t.Fatalf("request %d: %v", i, err)
			}
			resp.Body.Close()
			if resp.StatusCode == http.StatusTooManyRequests {
				limited++
			}
		}

		t.Logf("Rate limit POST /api/resolution/batch: %d/15 requests were rate-limited", limited)
		if limited == 0 {
			t.Error("expected at least some batch requests to be rate-limited")
		}
	})

	// --- DB assertions for resolutions table ---

	t.Run("DB_ResolutionsTableExists", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		count := dba.QueryScalarInt(t, "SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='resolutions'")
		if count != 1 {
			t.Error("resolutions table does not exist in nodes.db")
		}
	})

	t.Run("DB_ResolutionsUniqueIndex", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		count := dba.QueryScalarInt(t, "SELECT COUNT(*) FROM sqlite_master WHERE type='index' AND name='idx_resolutions_triplet'")
		if count != 1 {
			t.Error("unique index idx_resolutions_triplet does not exist")
		}
	})
}
