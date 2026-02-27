package e2e

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

// TestGolden is the cumulative golden.db peuplement test.
// It runs last (alphabetically after all other test files) and creates
// a rich, varied database state via the HTTP API.
func TestGolden(t *testing.T) {
	h, dba := ensureHarness(t)
	start := time.Now()
	defer func() { Record(t, start, nil, nil) }()

	// Phase 1: Register 7 users + verify bot exists
	t.Log("Phase 1: Register users")
	aliceToken, _ := h.Register(t, Users.Alice.Handle, Users.Alice.Password)
	bobToken, _ := h.Register(t, Users.Bob.Handle, Users.Bob.Password)
	carolToken, _ := h.Register(t, Users.Carol.Handle, Users.Carol.Password)
	davidToken, _ := h.Register(t, Users.David.Handle, Users.David.Password)
	eveToken, _ := h.Register(t, Users.Eve.Handle, Users.Eve.Password)
	operatorToken, _ := h.Register(t, Users.Operator.Handle, Users.Operator.Password)
	_, _ = h.Register(t, Users.Provider.Handle, Users.Provider.Password)
	_, _ = h.Register(t, Users.Researcher.Handle, Users.Researcher.Password)

	// Verify bot user exists (created at startup)
	var botStatus map[string]interface{}
	h.JSON("GET", "/api/bot/status", nil, "", &botStatus)
	if botStatus["enabled"] != true {
		t.Error("bot should be enabled")
	}

	// Phase 2: Create 4 complex trees
	t.Log("Phase 2: Create complex trees")

	// Tree 1: Deep tree (depth 9) with mixed node types
	tree1Root := h.AskQuestion(t, aliceToken, QuestionScience, TagsScience)
	parent := tree1Root
	nodeTypes := []string{"claim", "piece", "claim", "claim", "claim", "claim", "claim", "claim", "claim"}
	for i, nt := range nodeTypes {
		parent = h.AnswerNode(t, bobToken, parent, fmt.Sprintf("Depth %d: %s node for deep tree test", i+1, nt), nt)
	}

	// Tree 2: Wide tree (width 12) with diverse authors
	tree2Root := h.AskQuestion(t, bobToken, QuestionTechnology, TagsCrypto)
	tokens := []string{aliceToken, bobToken, carolToken, davidToken, eveToken, operatorToken,
		aliceToken, bobToken, carolToken, davidToken, eveToken, operatorToken}
	for i, tok := range tokens {
		h.AnswerNode(t, tok, tree2Root, fmt.Sprintf("Wide answer %d from different author", i+1), "claim")
	}

	// Tree 3: Unicode tree with emoji and Arabic content
	tree3Root := h.AskQuestion(t, carolToken, TextEmoji, TagsAI)
	h.AnswerNode(t, davidToken, tree3Root, TextArabic, "claim")
	h.AnswerNode(t, eveToken, tree3Root, TextChinese, "claim")
	h.AnswerNode(t, aliceToken, tree3Root, TextLong10K, "piece")

	// Tree 4: Duplicate body questions (slug collision test)
	tree4Root1 := h.AskQuestion(t, aliceToken, TextDuplicate1, TagsPhilosophy)
	tree4Root2 := h.AskQuestion(t, bobToken, TextDuplicate2, TagsPhilosophy)

	// Verify different slugs
	node1 := h.GetNode(t, tree4Root1)
	node2 := h.GetNode(t, tree4Root2)
	slug1 := node1["slug"]
	slug2 := node2["slug"]
	if slug1 == slug2 {
		t.Errorf("identical bodies should produce different slugs: %v vs %v", slug1, slug2)
	}

	// Phase 3: Votes with conflicts and reversals
	t.Log("Phase 3: Votes")

	// Alice upvotes tree1, Bob downvotes, Carol upvotes, then Bob reverses to upvote
	h.Do("POST", "/api/vote", map[string]interface{}{"node_id": tree1Root, "value": 1}, aliceToken)
	h.Do("POST", "/api/vote", map[string]interface{}{"node_id": tree1Root, "value": -1}, bobToken)
	h.Do("POST", "/api/vote", map[string]interface{}{"node_id": tree1Root, "value": 1}, carolToken)
	h.Do("POST", "/api/vote", map[string]interface{}{"node_id": tree1Root, "value": 1}, bobToken) // reversal

	// Upvote tree2 heavily
	h.Do("POST", "/api/vote", map[string]interface{}{"node_id": tree2Root, "value": 1}, aliceToken)
	h.Do("POST", "/api/vote", map[string]interface{}{"node_id": tree2Root, "value": 1}, carolToken)
	h.Do("POST", "/api/vote", map[string]interface{}{"node_id": tree2Root, "value": 1}, davidToken)
	h.Do("POST", "/api/vote", map[string]interface{}{"node_id": tree2Root, "value": 1}, eveToken)

	// Phase 4: Thanks with uniqueness
	t.Log("Phase 4: Thanks")
	h.Do("POST", "/api/thank", map[string]interface{}{"node_id": tree1Root, "message": "Excellent question"}, bobToken)
	h.Do("POST", "/api/thank", map[string]interface{}{"node_id": tree1Root, "message": "Very insightful"}, carolToken)
	h.Do("POST", "/api/thank", map[string]interface{}{"node_id": tree2Root, "message": "Important topic"}, aliceToken)
	// Idempotent repeat
	h.Do("POST", "/api/thank", map[string]interface{}{"node_id": tree1Root, "message": "Duplicate thank"}, bobToken)

	// Phase 5: Active bounties
	t.Log("Phase 5: Bounties")
	h.Do("POST", "/api/bounty", map[string]interface{}{"node_id": tree1Root, "amount": 500}, aliceToken)
	h.Do("POST", "/api/bounty", map[string]interface{}{"node_id": tree2Root, "amount": 200}, bobToken)
	h.Do("POST", "/api/bounty", map[string]interface{}{"node_id": tree3Root, "amount": 100}, carolToken)

	// Phase 6: Verify credit_ledger for bot (if LLM available)
	t.Log("Phase 6: Bot credit verification")
	dba.AssertRowCountGTE(t, "credit_ledger", "reason = 'daily_allowance'", nil, 1)

	// Phase 7: All temperatures via child_count conditions
	t.Log("Phase 7: Temperature verification conditions")
	dba.AssertNodeFieldGTE(t, tree2Root, "child_count", 12) // wide tree
	dba.AssertNodeFieldGTE(t, tree1Root, "score", 2)         // 3 upvotes - 0 down (after reversal)

	// Phase 8: Global verification
	t.Log("Phase 8: Global verification")

	// ≥ 8 users (alice, bob, carol, david, eve, operator, provider, researcher + bot)
	userCount := dba.QueryScalarInt(t, "SELECT COUNT(*) FROM users")
	if userCount < 8 {
		t.Errorf("user count = %d, want >= 8", userCount)
	}

	// ≥ 30 nodes
	nodeCount := dba.QueryScalarInt(t, "SELECT COUNT(*) FROM nodes")
	if nodeCount < 30 {
		t.Errorf("node count = %d, want >= 30", nodeCount)
	}

	// ≥ 25 claims (formerly questions + answers + objections etc.)
	claimCount := dba.QueryScalarInt(t, "SELECT COUNT(*) FROM nodes WHERE node_type = 'claim'")
	if claimCount < 25 {
		t.Errorf("claim count = %d, want >= 25", claimCount)
	}

	// Both valid node types present
	validTypes := []string{"claim", "piece"}
	for _, nt := range validTypes {
		count := dba.QueryScalarInt(t, "SELECT COUNT(*) FROM nodes WHERE node_type = ?", nt)
		if count == 0 {
			t.Errorf("no nodes with type %s", nt)
		}
	}

	// Unique slugs for all questions
	slugCount := dba.QueryScalarInt(t, "SELECT COUNT(DISTINCT slug) FROM nodes WHERE slug IS NOT NULL")
	totalQuestions := dba.QueryScalarInt(t, "SELECT COUNT(*) FROM nodes WHERE slug IS NOT NULL")
	if slugCount != totalQuestions {
		t.Errorf("slug uniqueness: %d unique slugs for %d questions with slugs", slugCount, totalQuestions)
	}

	// Bounties active
	activeBounties := dba.QueryScalarInt(t, "SELECT COUNT(*) FROM bounties WHERE status = 'active'")
	if activeBounties < 3 {
		t.Errorf("active bounties = %d, want >= 3", activeBounties)
	}

	// Thanks exist
	thankCount := dba.QueryScalarInt(t, "SELECT COUNT(*) FROM thanks")
	if thankCount < 3 {
		t.Errorf("thanks count = %d, want >= 3", thankCount)
	}

	// Tags exist
	tagCount := dba.QueryScalarInt(t, "SELECT COUNT(DISTINCT tag) FROM tags")
	if tagCount < 5 {
		t.Errorf("unique tags = %d, want >= 5", tagCount)
	}

	// Unicode content stored correctly
	var arabicBody string
	dba.openAndQuery(t, "SELECT body FROM nodes WHERE body LIKE '%الذكاء%' LIMIT 1", &arabicBody)
	if !strings.Contains(arabicBody, "الذكاء") {
		t.Error("Arabic content not correctly stored")
	}

	// Emoji content stored correctly
	var emojiBody string
	dba.openAndQuery(t, "SELECT body FROM nodes WHERE body LIKE '%🌍%' LIMIT 1", &emojiBody)
	if !strings.Contains(emojiBody, "🌍") {
		t.Error("Emoji content not correctly stored")
	}

	t.Logf("Golden test complete: %d users, %d nodes, %d claims, %d tags, %d bounties, %d thanks",
		userCount, nodeCount, claimCount, tagCount, activeBounties, thankCount)
}

