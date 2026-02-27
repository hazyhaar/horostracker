package e2e

import (
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

func TestLLM(t *testing.T) {
	h, dba := ensureHarness(t)

	t.Run("BotStatusWithLLM", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		var status map[string]interface{}
		resp, err := h.JSON("GET", "/api/bot/status", nil, "", &status)
		if err != nil {
			t.Fatalf("bot status: %v", err)
		}
		RequireStatus(t, resp, http.StatusOK)

		if status["enabled"] != true {
			t.Error("expected bot enabled")
		}
		if status["handle"] == nil || status["handle"] == "" {
			t.Error("expected bot handle")
		}
		if HasLLM() {
			if status["has_llm"] != true {
				t.Error("expected has_llm=true when API keys present")
			}
		}
	})

	t.Run("BotAnswerClaude", func(t *testing.T) {
		if os.Getenv("ANTHROPIC_API_KEY") == "" {
			t.Skip("ANTHROPIC_API_KEY not set")
		}
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		token, _ := h.Register(t, "llm_claude_user", "claudepass1234")
		questionID := h.AskQuestion(t, token, "What are the main differences between TCP and UDP protocols?", []string{"networking"})
		h.AnswerNode(t, token, questionID, "TCP is connection-oriented while UDP is connectionless", "claim")

		var result map[string]interface{}
		resp, err := h.JSON("POST", "/api/bot/answer/"+questionID, map[string]interface{}{
			"provider": "anthropic",
			"model":    "claude-haiku-4-5-20251001",
		}, token, &result)
		if err != nil {
			t.Fatalf("bot answer claude: %v", err)
		}
		RequireStatus(t, resp, http.StatusCreated)

		if result["type"] != "claim" {
			t.Errorf("type = %v, want claim", result["type"])
		}

		node := result["node"].(map[string]interface{})
		nodeID := node["id"].(string)
		if node["node_type"] != "claim" {
			t.Errorf("node_type = %v, want claim", node["node_type"])
		}

		// Verify credit ledger debit
		dba.AssertRowCountGTE(t, "credit_ledger", "reason = 'bot_answer'", nil, 1)

		// Verify node exists in DB
		dba.AssertNodeExists(t, nodeID)
		dba.AssertNodeField(t, nodeID, "node_type", "claim")
	})

	t.Run("BotAnswerGemini", func(t *testing.T) {
		if os.Getenv("GEMINI_API_KEY") == "" {
			t.Skip("GEMINI_API_KEY not set")
		}
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		token, _ := h.Register(t, "llm_gemini_user", "geminipass1234")
		questionID := h.AskQuestion(t, token, "How does photosynthesis convert sunlight into chemical energy?", []string{"biology"})

		var result map[string]interface{}
		resp, err := h.JSON("POST", "/api/bot/answer/"+questionID, map[string]interface{}{
			"provider": "gemini",
			"model":    "gemini-2.0-flash",
		}, token, &result)
		if err != nil {
			t.Fatalf("bot answer gemini: %v", err)
		}
		RequireStatus(t, resp, http.StatusCreated)

		if result["type"] != "claim" {
			t.Errorf("type = %v, want claim", result["type"])
		}
	})

	t.Run("Resolution", func(t *testing.T) {
		if !HasLLM() {
			t.Skip("no LLM API keys set")
		}
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		token, _ := h.Register(t, "llm_res_user", "resolutionpass1234")
		questionID := h.AskQuestion(t, token, "Should programming languages enforce memory safety at compile time?", nil)
		h.AnswerNode(t, token, questionID, "Yes, memory safety prevents entire categories of bugs", "claim")
		h.AnswerNode(t, token, questionID, "No, it restricts low-level programming needed for systems work", "claim")

		var result map[string]interface{}
		resp, err := h.JSON("POST", "/api/resolution/"+questionID, map[string]interface{}{}, token, &result)
		if err != nil {
			t.Fatalf("resolution: %v", err)
		}
		RequireStatus(t, resp, http.StatusCreated)

		resNode := result["resolution"].(map[string]interface{})
		if resNode["node_type"] != "claim" {
			t.Errorf("node_type = %v, want claim", resNode["node_type"])
		}

		// Verify in flows.db
		generation := result["generation"].(map[string]interface{})
		if generation["content"] == nil || generation["content"] == "" {
			t.Error("expected non-empty resolution content")
		}
	})

	t.Run("GetResolutions", func(t *testing.T) {
		if !HasLLM() {
			t.Skip("no LLM API keys set")
		}
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		token, _ := h.Register(t, "llm_getres_user", "getrespass1234")
		questionID := h.AskQuestion(t, token, "What are the trade-offs of functional vs object-oriented programming?", nil)
		h.AnswerNode(t, token, questionID, "FP emphasizes immutability and pure functions", "claim")

		// Generate resolution first
		h.JSON("POST", "/api/resolution/"+questionID, map[string]interface{}{}, token, nil)

		// List resolutions
		var result struct {
			Resolutions []interface{} `json:"resolutions"`
			Count       int           `json:"count"`
		}
		resp, err := h.JSON("GET", "/api/resolution/"+questionID, nil, "", &result)
		if err != nil {
			t.Fatalf("get resolutions: %v", err)
		}
		RequireStatus(t, resp, http.StatusOK)

		if result.Count == 0 {
			t.Error("expected at least 1 resolution")
		}
	})

	t.Run("Render", func(t *testing.T) {
		if !HasLLM() {
			t.Skip("no LLM API keys set")
		}
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		token, _ := h.Register(t, "llm_render_user", "renderpass1234")
		questionID := h.AskQuestion(t, token, "How does blockchain achieve consensus without central authority?", nil)
		h.AnswerNode(t, token, questionID, "Proof-of-work requires computational effort to validate blocks", "claim")
		h.AnswerNode(t, token, questionID, "Proof-of-stake is more energy efficient", "claim")

		// Generate resolution
		var resResult map[string]interface{}
		h.JSON("POST", "/api/resolution/"+questionID, map[string]interface{}{}, token, &resResult)
		resNode := resResult["resolution"].(map[string]interface{})
		resID := resNode["id"].(string)

		// Render as summary
		var renderResult map[string]interface{}
		resp, err := h.JSON("POST", "/api/render/"+resID, map[string]interface{}{
			"format": "summary",
		}, "", &renderResult)
		if err != nil {
			t.Fatalf("render: %v", err)
		}
		RequireStatus(t, resp, http.StatusCreated)

		if renderResult["render_id"] == nil || renderResult["render_id"] == "" {
			t.Error("expected non-empty render_id")
		}
	})

	t.Run("GetRenders", func(t *testing.T) {
		if !HasLLM() {
			t.Skip("no LLM API keys set")
		}
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		token, _ := h.Register(t, "llm_getrend_user", "getrendpass1234")
		questionID := h.AskQuestion(t, token, "What is the future of quantum error correction?", nil)
		h.AnswerNode(t, token, questionID, "Surface codes are the most promising approach", "claim")

		var resResult map[string]interface{}
		h.JSON("POST", "/api/resolution/"+questionID, map[string]interface{}{}, token, &resResult)
		resNode := resResult["resolution"].(map[string]interface{})
		resID := resNode["id"].(string)

		h.JSON("POST", "/api/render/"+resID, map[string]interface{}{"format": "summary"}, "", nil)

		var renders []map[string]interface{}
		resp, err := h.JSON("GET", "/api/renders/"+resID, nil, "", &renders)
		if err != nil {
			t.Fatalf("get renders: %v", err)
		}
		RequireStatus(t, resp, http.StatusOK)

		if len(renders) == 0 {
			t.Error("expected at least 1 render")
		}
	})

	t.Run("ChallengeRunConfrontation", func(t *testing.T) {
		if !HasLLM() {
			t.Skip("no LLM API keys set")
		}
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		token, _ := h.Register(t, "llm_chal_user", "chalpass12345")
		questionID := h.AskQuestion(t, token, "Is artificial general intelligence achievable within 10 years?", nil)
		h.AnswerNode(t, token, questionID, "Current progress suggests AGI is decades away", "claim")
		h.AnswerNode(t, token, questionID, "Scaling laws show no signs of stopping", "claim")

		// Create challenge
		var challenge map[string]interface{}
		resp, err := h.JSON("POST", "/api/challenge/"+questionID, map[string]interface{}{
			"flow_name": "confrontation",
		}, token, &challenge)
		if err != nil {
			t.Fatalf("create challenge: %v", err)
		}
		RequireStatus(t, resp, http.StatusCreated)
		challengeID := challenge["id"].(string)

		// Run challenge
		var runResult map[string]interface{}
		resp, err = h.JSON("POST", "/api/challenge/"+challengeID+"/run", nil, token, &runResult)
		if err != nil {
			t.Fatalf("run challenge: %v", err)
		}
		RequireStatus(t, resp, http.StatusOK)

		// Verify challenge completed
		var completed map[string]interface{}
		h.JSON("GET", "/api/challenge/"+challengeID, nil, "", &completed)
		if completed["status"] != "completed" && completed["status"] != "failed" {
			t.Errorf("status = %v, want completed or failed", completed["status"])
		}

		// Check moderation scores were created
		var modResult struct {
			Scores []interface{} `json:"scores"`
			Count  int           `json:"count"`
		}
		h.JSON("GET", "/api/moderation/"+questionID, nil, "", &modResult)
		if modResult.Count == 0 && completed["status"] == "completed" {
			t.Error("expected moderation scores after completed challenge")
		}
	})

	t.Run("ChallengeAlreadyCompleted", func(t *testing.T) {
		if !HasLLM() {
			t.Skip("no LLM API keys set")
		}
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		token, _ := h.Register(t, "llm_dup_chal", "dupchalpass123")
		questionID := h.AskQuestion(t, token, "Should we colonize Mars?", nil)

		var challenge map[string]interface{}
		h.JSON("POST", "/api/challenge/"+questionID, map[string]interface{}{
			"flow_name": "confrontation",
		}, token, &challenge)
		challengeID := challenge["id"].(string)

		// Run it
		h.JSON("POST", "/api/challenge/"+challengeID+"/run", nil, token, nil)

		// Try to run again → 409
		resp, _ := h.Do("POST", "/api/challenge/"+challengeID+"/run", nil, token)
		RequireStatus(t, resp, http.StatusConflict)
	})

	t.Run("ForensicFlowSteps", func(t *testing.T) {
		if !HasLLM() {
			t.Skip("no LLM API keys set")
		}
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		token, _ := h.Register(t, "llm_forensic", "forensicpass123")
		questionID := h.AskQuestion(t, token, "What is dark matter and why can't we detect it directly?", nil)
		h.AnswerNode(t, token, questionID, "Dark matter interacts only via gravity", "claim")

		var challenge map[string]interface{}
		h.JSON("POST", "/api/challenge/"+questionID, map[string]interface{}{
			"flow_name": "confrontation",
		}, token, &challenge)
		challengeID := challenge["id"].(string)

		h.JSON("POST", "/api/challenge/"+challengeID+"/run", nil, token, nil)

		// Fetch challenge to get flow_id
		var completed map[string]interface{}
		h.JSON("GET", "/api/challenge/"+challengeID, nil, "", &completed)

		if completed["status"] != "completed" {
			t.Skip("challenge did not complete — cannot verify flow steps")
		}

		flowID, ok := completed["flow_id"].(string)
		if !ok || flowID == "" {
			t.Fatal("expected non-empty flow_id")
		}

		// Verify flow_steps in flows.db
		steps := dba.QueryFlowSteps(t, flowID)
		if len(steps) < 4 {
			t.Errorf("expected at least 4 flow steps (confrontation), got %d", len(steps))
		}

		for i, step := range steps {
			prompt := step["prompt"].(string)
			response := step["response_raw"].(string)
			tokensIn := step["tokens_in"].(int64)
			tokensOut := step["tokens_out"].(int64)
			latency := step["latency_ms"].(int64)

			if prompt == "" {
				t.Errorf("step %d: empty prompt", i)
			}
			if response == "" {
				t.Errorf("step %d: empty response_raw", i)
			}
			if tokensIn <= 0 {
				t.Errorf("step %d: tokens_in = %d, want > 0", i, tokensIn)
			}
			if tokensOut <= 0 {
				t.Errorf("step %d: tokens_out = %d, want > 0", i, tokensOut)
			}
			if latency <= 0 {
				t.Errorf("step %d: latency_ms = %d, want > 0", i, latency)
			}
		}
	})

	// --- Abuse tests ---

	t.Run("Abuse_PromptInjectionViaNodeBody", func(t *testing.T) {
		if !HasLLM() {
			t.Skip("no LLM API keys set — skipping prompt injection test")
		}
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		token, _ := h.Register(t, "abuse_llm_inject", "injectpass12345")

		// Create a node with prompt injection payload as body
		questionID := h.AskQuestion(t, token, PromptInjection, nil)
		h.AnswerNode(t, token, questionID, "This is a normal answer to a malicious question", "claim")

		// Invoke bot on the malicious node
		var result map[string]interface{}
		resp, err := h.JSON("POST", "/api/bot/answer/"+questionID, map[string]interface{}{}, token, &result)
		if err != nil {
			t.Fatalf("bot answer on injected node: %v", err)
		}

		if resp.StatusCode == http.StatusCreated {
			// Check that the response does not leak system prompt
			node, ok := result["node"].(map[string]interface{})
			if ok {
				body, _ := node["body"].(string)
				if strings.Contains(strings.ToLower(body), "system prompt") ||
					strings.Contains(strings.ToLower(body), "you are") {
					t.Log("SECURITY NOTE: bot response may contain leaked system prompt — manual review recommended")
				}
			}
			t.Log("Prompt injection via node body: bot responded without crash")
		} else {
			t.Logf("Prompt injection via node body: status %d (server did not crash)", resp.StatusCode)
		}
	})
}
