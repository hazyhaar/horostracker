// CLAUDE:SUMMARY Core API struct and shared HTTP handlers — auth, nodes CRUD, search, votes, tags, bounties, user profiles
package api

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/hazyhaar/horostracker/internal/auth"
	"github.com/hazyhaar/horostracker/internal/config"
	"github.com/hazyhaar/horostracker/internal/db"
	"github.com/hazyhaar/horostracker/internal/llm"
)

// handleRe validates handle format: ASCII alphanumeric, underscore, hyphen only.
var handleRe = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

// fts5SpecialRe matches FTS5 metacharacters that can cause syntax errors.
var fts5SpecialRe = regexp.MustCompile(`[*"():{}^]`)

// maxBodySize is the maximum HTTP body size for node creation endpoints.
const maxBodySize = 200 * 1024 // 200KB

// SearchRateLimiter is the rate limiter for POST /api/search (30 req/60s).
var SearchRateLimiter = NewRateLimiter(30, 60*time.Second)

type API struct {
	db              *db.DB
	flowsDB         *db.FlowsDB
	metricsDB       *db.MetricsDB
	flowsDBPath     string
	metricsDBPath   string
	auth            *auth.Auth
	resEngine       *llm.ResolutionEngine
	challengeRunner *llm.ChallengeRunner
	replayEngine    *llm.ReplayEngine
	workflowEngine  *llm.WorkflowEngine
	modelDiscovery  *llm.ModelDiscovery
	llmClient       *llm.Client
	botUserID       string
	fedConfig       *config.FederationConfig
	instConfig      *config.InstanceConfig
}

// SetBotUserID sets the bot user ID for auto-answer endpoints.
func (a *API) SetBotUserID(id string) {
	a.botUserID = id
}

// SetFlowsDB sets the flows database for forensic/replay endpoints.
func (a *API) SetFlowsDB(fdb *db.FlowsDB, path string) {
	a.flowsDB = fdb
	a.flowsDBPath = path
}

// SetMetricsDB sets the metrics database for forensic endpoints.
func (a *API) SetMetricsDB(mdb *db.MetricsDB, path string) {
	a.metricsDB = mdb
	a.metricsDBPath = path
}

// SetReplayEngine sets the replay engine.
func (a *API) SetReplayEngine(re *llm.ReplayEngine) {
	a.replayEngine = re
}

// SetLLMClient sets the LLM client for dispatch/replay endpoints.
func (a *API) SetLLMClient(c *llm.Client) {
	a.llmClient = c
}

func New(database *db.DB, a *auth.Auth) *API {
	return &API{db: database, auth: a}
}

func (a *API) RegisterRoutes(mux *http.ServeMux) {
	// Auth
	mux.HandleFunc("POST /api/register", a.handleRegister)
	mux.HandleFunc("POST /api/login", a.handleLogin)

	// Nodes
	mux.HandleFunc("POST /api/ask", a.handleAsk)
	mux.HandleFunc("POST /api/answer", a.handleAnswer)
	mux.HandleFunc("GET /api/tree/{id}", a.handleGetTree)
	mux.HandleFunc("GET /api/node/{id}", a.handleGetNode)
	mux.HandleFunc("POST /api/search", RateLimitMiddleware(SearchRateLimiter, a.handleSearch))

	// Votes & thanks
	mux.HandleFunc("POST /api/vote", a.handleVote)
	mux.HandleFunc("POST /api/thank", a.handleThank)

	// Tags
	mux.HandleFunc("GET /api/tags", a.handleGetTags)

	// Bounties
	mux.HandleFunc("GET /api/bounties", a.handleGetBounties)
	mux.HandleFunc("POST /api/bounty", a.handleCreateBounty)

	// Questions feed
	mux.HandleFunc("GET /api/questions", a.handleGetQuestions)

	// User profile
	mux.HandleFunc("GET /api/user/{handle}", a.handleGetUser)
	mux.HandleFunc("GET /api/me", a.handleGetMe)

	// Resolution + renders
	a.RegisterResolutionRoutes(mux)

	// Dataset export
	a.RegisterExportRoutes(mux)

	// Adversarial challenges
	a.RegisterChallengeRoutes(mux)

	// Bot
	a.RegisterBotRoutes(mux)

	// Federation
	a.RegisterFederationRoutes(mux)

	// Envelopes (piece routing)
	a.RegisterEnvelopeRoutes(mux)

	// Integrity
	a.RegisterIntegrityRoutes(mux)

	// Replay
	a.RegisterReplayRoutes(mux)

	// Forensic
	a.RegisterForensicRoutes(mux)

	// Dispatch (multi-model parallel)
	a.RegisterDispatchRoutes(mux)

	// Provider self-registration
	a.RegisterProviderRoutes(mux)

	// Dataset factory
	a.RegisterDatasetRoutes(mux)

	// Benchmarks
	a.RegisterBenchmarkRoutes(mux)

	// Deduplication
	a.RegisterDedupRoutes(mux)

	// Dynamic workflows (VACF)
	a.RegisterWorkflowRoutes(mux)

	// Safety
	mux.HandleFunc("GET /api/nodes/{id}/safety", a.handleGetSafety)
	mux.HandleFunc("GET /api/safety/patterns", a.handleListSafetyPatterns)
	mux.HandleFunc("GET /api/safety/patterns/export", a.handleExportSafetyPatterns)
	mux.HandleFunc("POST /api/safety/patterns", a.handleCreateSafetyPattern)
	mux.HandleFunc("PUT /api/safety/patterns/{id}/vote", a.handleVoteSafetyPattern)
	mux.HandleFunc("GET /api/safety/leaderboard", a.handleSafetyLeaderboard)

	// Clones & access
	mux.HandleFunc("GET /api/questions/{id}/clones", a.handleGetClones)
	mux.HandleFunc("POST /api/questions/{id}/access", a.handleConfigureAccess)

	// Soft-delete
	mux.HandleFunc("DELETE /api/node/{id}", a.handleDeleteNode)

	// Assertions (decompose + validate)
	mux.HandleFunc("POST /api/node/{id}/decompose", a.handleDecompose)
	mux.HandleFunc("POST /api/node/{id}/assertions", a.handleCreateAssertions)
	mux.HandleFunc("GET /api/node/{id}/assertions", a.handleGetAssertions)

	// Sources
	mux.HandleFunc("POST /api/node/{id}/source", a.handleAddSource)
	mux.HandleFunc("GET /api/node/{id}/sources", a.handleGetSources)

	// 5W1H
	mux.HandleFunc("GET /api/source/{id}/5w1h", a.handleGetSource5W1H)

	// Slug lookup
	mux.HandleFunc("GET /api/q/{slug}", a.handleGetNodeBySlug)

	// API v1 prefix aliases (backward compat)
	a.RegisterV1Routes(mux)
}

// --- Auth ---

func (a *API) handleRegister(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Handle   string `json:"handle"`
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if req.Handle == "" || req.Password == "" {
		jsonError(w, "handle and password are required", http.StatusBadRequest)
		return
	}
	if len(req.Handle) < 3 || len(req.Handle) > 30 {
		jsonError(w, "handle must be 3-30 characters", http.StatusBadRequest)
		return
	}
	if !handleRe.MatchString(req.Handle) {
		jsonError(w, "handle must contain only ASCII letters, digits, underscore or hyphen", http.StatusBadRequest)
		return
	}
	if strings.ContainsRune(req.Handle, 0) {
		jsonError(w, "handle contains invalid characters", http.StatusBadRequest)
		return
	}
	if len(req.Password) < 8 {
		jsonError(w, "password must be at least 8 characters", http.StatusBadRequest)
		return
	}

	hash, err := a.auth.HashPassword(req.Password)
	if err != nil {
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}

	user, err := a.db.CreateUser(db.CreateUserInput{
		Handle:       req.Handle,
		Email:        req.Email,
		PasswordHash: hash,
	})
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			jsonError(w, "handle or email already taken", http.StatusConflict)
			return
		}
		slog.Error("creating user", "error", err)
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}

	token, err := a.auth.GenerateToken(user.ID, user.Handle)
	if err != nil {
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}

	jsonResp(w, http.StatusCreated, map[string]interface{}{
		"user":  user,
		"token": token,
	})
}

func (a *API) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Handle   string `json:"handle"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, "invalid request body", http.StatusBadRequest)
		return
	}

	user, passwordHash, err := a.db.GetUserByHandle(req.Handle)
	if err != nil {
		jsonError(w, "invalid credentials", http.StatusUnauthorized)
		return
	}

	if !a.auth.CheckPassword(passwordHash, req.Password) {
		jsonError(w, "invalid credentials", http.StatusUnauthorized)
		return
	}

	token, err := a.auth.GenerateToken(user.ID, user.Handle)
	if err != nil {
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}

	jsonResp(w, http.StatusOK, map[string]interface{}{
		"user":  user,
		"token": token,
	})
}

// --- Nodes ---

func (a *API) handleAsk(w http.ResponseWriter, r *http.Request) {
	claims := a.auth.ExtractClaims(r)
	if claims == nil {
		jsonError(w, "authentication required", http.StatusUnauthorized)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxBodySize)
	var req struct {
		Body string   `json:"body"`
		Tags []string `json:"tags"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		if strings.Contains(err.Error(), "too large") {
			jsonError(w, "request body too large", http.StatusRequestEntityTooLarge)
			return
		}
		jsonError(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if req.Body == "" {
		jsonError(w, "body is required", http.StatusBadRequest)
		return
	}

	node, err := a.db.CreateNode(db.CreateNodeInput{
		NodeType: "claim",
		Body:     req.Body,
		AuthorID: claims.UserID,
		Tags:     req.Tags,
	})
	if err != nil {
		slog.Error("creating root claim", "error", err)
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}

	// Search for similar claims
	similar, _ := a.db.SearchNodes(req.Body, 5)

	// Safety scoring
	safetyResult := a.db.ScoreContent(req.Body)
	_ = a.db.SaveSafetyScore(node.ID, safetyResult)

	// If LLM is available, decompose via ALL providers in parallel for benchmarking
	var decompositions []map[string]interface{}
	if a.llmClient != nil && len(a.llmClient.Providers()) > 0 {
		decompositions = DecomposeAllProviders(r.Context(), a.llmClient, req.Body)
	}

	jsonResp(w, http.StatusCreated, map[string]interface{}{
		"node":            node,
		"similar":         similar,
		"safety_score":    safetyResult,
		"decompositions":  decompositions,
	})
}

func (a *API) handleAnswer(w http.ResponseWriter, r *http.Request) {
	claims := a.auth.ExtractClaims(r)
	if claims == nil {
		jsonError(w, "authentication required", http.StatusUnauthorized)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxBodySize)
	var req struct {
		ParentID string   `json:"parent_id"`
		Body     string   `json:"body"`
		NodeType string   `json:"node_type"`
		ModelID  *string  `json:"model_id"`
		Metadata string   `json:"metadata"`
		Tags     []string `json:"tags"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		if strings.Contains(err.Error(), "too large") {
			jsonError(w, "request body too large", http.StatusRequestEntityTooLarge)
			return
		}
		jsonError(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if req.ParentID == "" || req.Body == "" {
		jsonError(w, "parent_id and body are required", http.StatusBadRequest)
		return
	}

	validTypes := map[string]bool{
		"claim": true, "piece": true,
	}
	if req.NodeType == "" {
		req.NodeType = "claim"
	}
	if !validTypes[req.NodeType] {
		jsonError(w, "invalid node_type: must be 'claim' or 'piece'", http.StatusBadRequest)
		return
	}

	node, err := a.db.CreateNode(db.CreateNodeInput{
		ParentID: &req.ParentID,
		NodeType: req.NodeType,
		Body:     req.Body,
		AuthorID: claims.UserID,
		ModelID:  req.ModelID,
		Metadata: req.Metadata,
		Tags:     req.Tags,
	})
	if err != nil {
		slog.Error("creating node", "error", err)
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}

	// Safety scoring
	safetyResult := a.db.ScoreContent(req.Body)
	_ = a.db.SaveSafetyScore(node.ID, safetyResult)

	// Marshal node to map and add safety_score
	nodeJSON, _ := json.Marshal(node)
	var resp map[string]interface{}
	_ = json.Unmarshal(nodeJSON, &resp)
	resp["safety_score"] = safetyResult

	jsonResp(w, http.StatusCreated, resp)
}

func (a *API) handleGetTree(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		jsonError(w, "id is required", http.StatusBadRequest)
		return
	}

	depthStr := r.URL.Query().Get("depth")
	maxDepth := 50
	if depthStr != "" {
		if d, err := strconv.Atoi(depthStr); err == nil && d > 0 {
			maxDepth = d
		}
	}

	tree, err := a.db.GetTree(id, maxDepth)
	if err != nil {
		if err == sql.ErrNoRows {
			jsonError(w, "node not found", http.StatusNotFound)
			return
		}
		slog.Error("getting tree", "error", err)
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}

	// Increment view count asynchronously
	go a.db.IncrementViewCount(id)

	jsonResp(w, http.StatusOK, tree)
}

func (a *API) handleGetNode(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		jsonError(w, "id is required", http.StatusBadRequest)
		return
	}

	node, err := a.db.GetNode(id)
	if err != nil {
		if err == sql.ErrNoRows {
			jsonError(w, "node not found", http.StatusNotFound)
			return
		}
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}

	go a.db.IncrementViewCount(id)

	jsonResp(w, http.StatusOK, node)
}

func (a *API) handleSearch(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Query string `json:"query"`
		Limit int    `json:"limit"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if req.Query == "" {
		jsonError(w, "query is required", http.StatusBadRequest)
		return
	}
	// Reject oversized queries
	if len(req.Query) > 50*1024 {
		jsonError(w, "query too large", http.StatusRequestEntityTooLarge)
		return
	}
	// Strip control characters (CRLF injection, null bytes)
	req.Query = strings.Map(func(r rune) rune {
		if r < 0x20 && r != ' ' {
			return ' '
		}
		return r
	}, req.Query)
	// Sanitize FTS5 special characters to prevent syntax errors
	req.Query = fts5SpecialRe.ReplaceAllString(req.Query, " ")
	req.Query = strings.TrimSpace(strings.Join(strings.Fields(req.Query), " "))
	if req.Query == "" {
		jsonError(w, "query is required", http.StatusBadRequest)
		return
	}

	results, err := a.db.SearchNodes(req.Query, req.Limit)
	if err != nil {
		// FTS5 syntax errors should not return 500 — return empty results
		slog.Error("search failed", "error", err)
		jsonResp(w, http.StatusOK, map[string]interface{}{
			"results": []*db.Node{},
			"count":   0,
		})
		return
	}

	jsonResp(w, http.StatusOK, map[string]interface{}{
		"results": results,
		"count":   len(results),
	})
}

// --- Votes & Thanks ---

func (a *API) handleVote(w http.ResponseWriter, r *http.Request) {
	claims := a.auth.ExtractClaims(r)
	if claims == nil {
		jsonError(w, "authentication required", http.StatusUnauthorized)
		return
	}

	var req struct {
		NodeID string `json:"node_id"`
		Value  int    `json:"value"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if req.Value != 1 && req.Value != -1 {
		jsonError(w, "value must be 1 or -1", http.StatusBadRequest)
		return
	}

	if err := a.db.Vote(claims.UserID, req.NodeID, req.Value); err != nil {
		if err == db.ErrSelfVote {
			jsonError(w, "cannot vote on your own node", http.StatusForbidden)
			return
		}
		slog.Error("vote failed", "error", err)
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}

	jsonResp(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (a *API) handleThank(w http.ResponseWriter, r *http.Request) {
	claims := a.auth.ExtractClaims(r)
	if claims == nil {
		jsonError(w, "authentication required", http.StatusUnauthorized)
		return
	}

	var req struct {
		NodeID  string `json:"node_id"`
		Message string `json:"message"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if req.NodeID == "" {
		jsonError(w, "node_id is required", http.StatusBadRequest)
		return
	}

	if err := a.db.Thank(claims.UserID, req.NodeID, req.Message); err != nil {
		slog.Error("thank failed", "error", err)
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}

	jsonResp(w, http.StatusOK, map[string]string{"status": "ok"})
}

// --- Tags ---

func (a *API) handleGetTags(w http.ResponseWriter, r *http.Request) {
	limitStr := r.URL.Query().Get("limit")
	limit := 30
	if limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil && l > 0 {
			limit = l
		}
	}

	tags, err := a.db.GetPopularTags(limit)
	if err != nil {
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}

	jsonResp(w, http.StatusOK, tags)
}

// --- Bounties ---

func (a *API) handleGetBounties(w http.ResponseWriter, r *http.Request) {
	tag := r.URL.Query().Get("tag")
	limitStr := r.URL.Query().Get("limit")
	limit := 20
	if limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil && l > 0 {
			limit = l
		}
	}

	bounties, err := a.db.GetBounties(tag, limit)
	if err != nil {
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}

	jsonResp(w, http.StatusOK, bounties)
}

func (a *API) handleCreateBounty(w http.ResponseWriter, r *http.Request) {
	claims := a.auth.ExtractClaims(r)
	if claims == nil {
		jsonError(w, "authentication required", http.StatusUnauthorized)
		return
	}

	var req struct {
		NodeID string `json:"node_id"`
		Amount int    `json:"amount"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if req.NodeID == "" || req.Amount <= 0 {
		jsonError(w, "node_id and positive amount required", http.StatusBadRequest)
		return
	}
	if req.Amount > 1000000 {
		jsonError(w, "bounty amount exceeds maximum (1,000,000)", http.StatusBadRequest)
		return
	}

	bounty, err := a.db.CreateBounty(req.NodeID, claims.UserID, req.Amount)
	if err != nil {
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}

	jsonResp(w, http.StatusCreated, bounty)
}

// --- Questions feed ---

func (a *API) handleGetQuestions(w http.ResponseWriter, r *http.Request) {
	limitStr := r.URL.Query().Get("limit")
	limit := 20
	if limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil && l > 0 {
			limit = l
		}
	}

	questions, err := a.db.GetHotQuestions(limit)
	if err != nil {
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}

	jsonResp(w, http.StatusOK, questions)
}

// --- User ---

func (a *API) handleGetUser(w http.ResponseWriter, r *http.Request) {
	handle := r.PathValue("handle")
	if handle == "" {
		jsonError(w, "handle is required", http.StatusBadRequest)
		return
	}

	user, _, err := a.db.GetUserByHandle(handle)
	if err != nil {
		if err == sql.ErrNoRows {
			jsonError(w, "user not found", http.StatusNotFound)
			return
		}
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}

	jsonResp(w, http.StatusOK, user)
}

func (a *API) handleGetMe(w http.ResponseWriter, r *http.Request) {
	claims := a.auth.ExtractClaims(r)
	if claims == nil {
		jsonError(w, "authentication required", http.StatusUnauthorized)
		return
	}

	user, err := a.db.GetUserByID(claims.UserID)
	if err != nil {
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}

	jsonResp(w, http.StatusOK, user)
}

// --- Slug lookup ---

func (a *API) handleGetNodeBySlug(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	if slug == "" {
		jsonError(w, "slug is required", http.StatusBadRequest)
		return
	}

	node, err := a.db.GetNodeBySlug(slug)
	if err != nil {
		if err == sql.ErrNoRows {
			jsonError(w, "question not found", http.StatusNotFound)
			return
		}
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}

	go a.db.IncrementViewCount(node.ID)

	jsonResp(w, http.StatusOK, node)
}

// --- Safety ---

func (a *API) handleGetSafety(w http.ResponseWriter, r *http.Request) {
	nodeID := r.PathValue("id")
	if nodeID == "" {
		jsonError(w, "id is required", http.StatusBadRequest)
		return
	}

	timeline, err := a.db.GetSafetyTimeline(nodeID)
	if err != nil {
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}

	jsonResp(w, http.StatusOK, timeline)
}

func (a *API) handleListSafetyPatterns(w http.ResponseWriter, r *http.Request) {
	patterns, err := a.db.ListSafetyPatterns()
	if err != nil {
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}

	jsonResp(w, http.StatusOK, map[string]interface{}{
		"patterns": patterns,
		"count":    len(patterns),
	})
}

func (a *API) handleExportSafetyPatterns(w http.ResponseWriter, r *http.Request) {
	patterns, err := a.db.ListSafetyPatterns()
	if err != nil {
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}

	jsonResp(w, http.StatusOK, patterns)
}

func (a *API) handleCreateSafetyPattern(w http.ResponseWriter, r *http.Request) {
	claims := a.auth.ExtractClaims(r)
	if claims == nil {
		jsonError(w, "authentication required", http.StatusUnauthorized)
		return
	}

	var req struct {
		Pattern     string `json:"pattern"`
		PatternType string `json:"pattern_type"`
		ListType    string `json:"list_type"`
		Severity    string `json:"severity"`
		Language    string `json:"language"`
		Category    string `json:"category"`
		Description string `json:"description"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if req.Pattern == "" {
		jsonError(w, "pattern is required", http.StatusBadRequest)
		return
	}
	if req.PatternType == "" {
		req.PatternType = "exact"
	}
	if req.ListType == "" {
		req.ListType = "flag"
	}
	if req.Severity == "" {
		req.Severity = "low"
	}

	id, err := a.db.CreateSafetyPattern(req.Pattern, req.PatternType, req.ListType, req.Severity, req.Language, req.Category, req.Description, claims.UserID)
	if err != nil {
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}

	jsonResp(w, http.StatusCreated, map[string]interface{}{
		"id":     id,
		"status": "created",
	})
}

func (a *API) handleVoteSafetyPattern(w http.ResponseWriter, r *http.Request) {
	claims := a.auth.ExtractClaims(r)
	if claims == nil {
		jsonError(w, "authentication required", http.StatusUnauthorized)
		return
	}

	idStr := r.PathValue("id")
	patternID, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		jsonError(w, "invalid pattern id", http.StatusBadRequest)
		return
	}

	var req struct {
		Up bool `json:"up"`
	}
	if decErr := json.NewDecoder(r.Body).Decode(&req); decErr != nil {
		jsonError(w, "invalid request body", http.StatusBadRequest)
		return
	}

	votesUp, votesDown, err := a.db.VoteSafetyPattern(patternID, req.Up)
	if err != nil {
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}

	jsonResp(w, http.StatusOK, map[string]interface{}{
		"votes_up":   votesUp,
		"votes_down": votesDown,
	})
}

func (a *API) handleSafetyLeaderboard(w http.ResponseWriter, r *http.Request) {
	leaderboard, err := a.db.GetSafetyLeaderboard()
	if err != nil {
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}

	jsonResp(w, http.StatusOK, map[string]interface{}{
		"leaderboard": leaderboard,
		"count":       len(leaderboard),
	})
}

// --- Clones & Access ---

func (a *API) handleGetClones(w http.ResponseWriter, r *http.Request) {
	questionID := r.PathValue("id")
	if questionID == "" {
		jsonError(w, "id is required", http.StatusBadRequest)
		return
	}

	clones, err := a.db.GetClonesForTree(questionID)
	if err != nil {
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}

	jsonResp(w, http.StatusOK, map[string]interface{}{
		"clones": clones,
		"count":  len(clones),
	})
}

func (a *API) handleConfigureAccess(w http.ResponseWriter, r *http.Request) {
	claims := a.auth.ExtractClaims(r)
	if claims == nil {
		jsonError(w, "authentication required", http.StatusUnauthorized)
		return
	}

	questionID := r.PathValue("id")
	var req struct {
		Visibility string `json:"visibility"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, "invalid request body", http.StatusBadRequest)
		return
	}

	validVis := map[string]bool{"public": true, "research": true, "provider": true, "instance": true}
	if !validVis[req.Visibility] {
		jsonError(w, "invalid visibility", http.StatusBadRequest)
		return
	}

	_, err := a.db.Exec("UPDATE nodes SET visibility = ? WHERE id = ? AND author_id = ?", req.Visibility, questionID, claims.UserID)
	if err != nil {
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}

	jsonResp(w, http.StatusOK, map[string]string{"status": "ok"})
}

// --- Soft-delete ---

func (a *API) handleDeleteNode(w http.ResponseWriter, r *http.Request) {
	claims := a.auth.ExtractClaims(r)
	if claims == nil {
		jsonError(w, "authentication required", http.StatusUnauthorized)
		return
	}
	nodeID := r.PathValue("id")
	if nodeID == "" {
		jsonError(w, "id is required", http.StatusBadRequest)
		return
	}

	node, err := a.db.GetNode(nodeID)
	if err != nil {
		jsonError(w, "node not found", http.StatusNotFound)
		return
	}
	if node.AuthorID != claims.UserID {
		user, err := a.db.GetUserByID(claims.UserID)
		if err != nil || user.Role != "operator" {
			jsonError(w, "forbidden", http.StatusForbidden)
			return
		}
	}

	if err := a.db.SoftDeleteNode(nodeID); err != nil {
		slog.Error("deleting node", "error", err)
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}
	jsonResp(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// --- Assertions ---

func (a *API) handleDecompose(w http.ResponseWriter, r *http.Request) {
	claims := a.auth.ExtractClaims(r)
	if claims == nil {
		jsonError(w, "authentication required", http.StatusUnauthorized)
		return
	}
	nodeID := r.PathValue("id")

	node, err := a.db.GetNode(nodeID)
	if err != nil {
		jsonError(w, "node not found", http.StatusNotFound)
		return
	}
	if node.NodeType != "claim" {
		jsonError(w, "only claims can be decomposed", http.StatusBadRequest)
		return
	}
	if node.AuthorID != claims.UserID {
		u, uErr := a.db.GetUserByID(claims.UserID)
		if uErr != nil || u.Role != "operator" {
			jsonError(w, "forbidden", http.StatusForbidden)
			return
		}
	}

	if a.llmClient == nil {
		jsonError(w, "LLM service not configured", http.StatusServiceUnavailable)
		return
	}

	assertions, err := DecomposeQuestion(r.Context(), a.llmClient, node.Body)
	if err != nil {
		slog.Error("decomposing question", "error", err)
		jsonError(w, "decomposition failed: "+err.Error(), http.StatusInternalServerError)
		return
	}

	jsonResp(w, http.StatusOK, map[string]interface{}{
		"assertions": assertions,
	})
}

func (a *API) handleCreateAssertions(w http.ResponseWriter, r *http.Request) {
	claims := a.auth.ExtractClaims(r)
	if claims == nil {
		jsonError(w, "authentication required", http.StatusUnauthorized)
		return
	}
	nodeID := r.PathValue("id")

	node, err := a.db.GetNode(nodeID)
	if err != nil {
		jsonError(w, "node not found", http.StatusNotFound)
		return
	}
	if node.NodeType != "claim" {
		jsonError(w, "sub-claims can only be created from claims", http.StatusBadRequest)
		return
	}
	if node.AuthorID != claims.UserID {
		u, uErr := a.db.GetUserByID(claims.UserID)
		if uErr != nil || u.Role != "operator" {
			jsonError(w, "forbidden", http.StatusForbidden)
			return
		}
	}

	var req struct {
		Claims []string `json:"assertions"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if len(req.Claims) == 0 {
		jsonError(w, "at least one claim is required", http.StatusBadRequest)
		return
	}

	created := make([]*db.Node, 0, len(req.Claims))
	for _, text := range req.Claims {
		text = strings.TrimSpace(text)
		if text == "" {
			continue
		}
		n, err := a.db.CreateClaimNode(text, claims.UserID, nodeID)
		if err != nil {
			slog.Error("creating sub-claim", "error", err)
			jsonError(w, "error creating sub-claim", http.StatusInternalServerError)
			return
		}
		created = append(created, n)
	}

	jsonResp(w, http.StatusCreated, map[string]interface{}{
		"nodes": created,
		"count": len(created),
	})
}

func (a *API) handleGetAssertions(w http.ResponseWriter, r *http.Request) {
	nodeID := r.PathValue("id")
	subclaims, err := a.db.GetClaimsByParentClaim(nodeID)
	if err != nil {
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}
	if subclaims == nil {
		subclaims = []*db.Node{}
	}
	jsonResp(w, http.StatusOK, map[string]interface{}{
		"assertions": subclaims,
		"count":      len(subclaims),
	})
}

// --- Sources ---

func (a *API) handleAddSource(w http.ResponseWriter, r *http.Request) {
	claims := a.auth.ExtractClaims(r)
	if claims == nil {
		jsonError(w, "authentication required", http.StatusUnauthorized)
		return
	}
	nodeID := r.PathValue("id")

	node, err := a.db.GetNode(nodeID)
	if err != nil {
		jsonError(w, "node not found", http.StatusNotFound)
		return
	}
	if node.NodeType != "piece" && node.NodeType != "claim" {
		jsonError(w, "sources can only be attached to piece or claim nodes", http.StatusBadRequest)
		return
	}

	var req struct {
		URL         *string `json:"url"`
		ContentText *string `json:"content_text"`
		Title       *string `json:"title"`
	}
	if decErr := json.NewDecoder(r.Body).Decode(&req); decErr != nil {
		jsonError(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if (req.URL == nil || *req.URL == "") && (req.ContentText == nil || *req.ContentText == "") {
		jsonError(w, "url or content_text is required", http.StatusBadRequest)
		return
	}

	// Compute content hash
	var hashInput string
	if req.ContentText != nil && *req.ContentText != "" {
		hashInput = *req.ContentText
	} else if req.URL != nil {
		hashInput = *req.URL
	}
	contentHash := computeSHA256(hashInput)

	// Extract domain from URL
	var domain *string
	if req.URL != nil && *req.URL != "" {
		d := extractDomain(*req.URL)
		if d != "" {
			domain = &d
		}
	}

	source, err := a.db.CreateSource(nodeID, req.URL, req.ContentText, req.Title, domain, &contentHash)
	if err != nil {
		slog.Error("creating source", "error", err)
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}

	// Async 5W1H extraction if LLM client is configured with providers
	if a.llmClient != nil && len(a.llmClient.Providers()) > 0 {
		var sourceText string
		if req.ContentText != nil && *req.ContentText != "" {
			sourceText = *req.ContentText
		}
		if sourceText != "" {
			go func(srcID, text string) {
				dims, err := Extract5W1H(context.Background(), a.llmClient, text)
				if err != nil {
					slog.Error("5W1H extraction failed", "source_id", srcID, "error", err)
					return
				}
				for dim, entries := range dims {
					for _, entry := range entries {
						if err := a.db.CreateSource5W1H(srcID, dim, entry, 0.5); err != nil {
							slog.Error("5W1H insert failed", "error", err)
						}
					}
				}
			}(source.ID, sourceText)
		}
	}

	jsonResp(w, http.StatusCreated, source)
}

func (a *API) handleGetSources(w http.ResponseWriter, r *http.Request) {
	nodeID := r.PathValue("id")
	sources, err := a.db.GetSourcesByNode(nodeID)
	if err != nil {
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}
	if sources == nil {
		sources = []*db.Source{}
	}
	jsonResp(w, http.StatusOK, map[string]interface{}{
		"sources": sources,
		"count":   len(sources),
	})
}

// --- 5W1H ---

func (a *API) handleGetSource5W1H(w http.ResponseWriter, r *http.Request) {
	sourceID := r.PathValue("id")
	entries, err := a.db.GetSource5W1H(sourceID)
	if err != nil {
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}

	// Group by dimension
	grouped := make(map[string][]string)
	for _, e := range entries {
		grouped[e.Dimension] = append(grouped[e.Dimension], e.Content)
	}

	jsonResp(w, http.StatusOK, map[string]interface{}{
		"source_id":  sourceID,
		"dimensions": grouped,
		"raw":        entries,
	})
}

// --- Helpers ---

func jsonResp(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}

func jsonError(w http.ResponseWriter, msg string, status int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

func computeSHA256(input string) string {
	h := sha256.Sum256([]byte(input))
	return hex.EncodeToString(h[:])
}

func extractDomain(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	return u.Hostname()
}

// DecomposeAllProviders calls every configured LLM provider in parallel to
// decompose a question into atomic claims. Returns one result per provider,
// enabling side-by-side benchmarking of decomposition quality.
func DecomposeAllProviders(ctx context.Context, client *llm.Client, questionText string) []map[string]interface{} {
	providers := client.Providers()
	type result struct {
		provider string
		data     map[string]interface{}
	}
	ch := make(chan result, len(providers))

	for _, prov := range providers {
		go func(provName string) {
			claims, resp, err := decomposeWith(ctx, client, provName, questionText)
			if err != nil {
				slog.Error("decompose failed", "provider", provName, "error", err)
				ch <- result{provName, map[string]interface{}{
					"provider": provName,
					"error":    err.Error(),
				}}
				return
			}
			entry := map[string]interface{}{
				"provider":   resp.Provider,
				"model":      resp.Model,
				"claims":     claims,
				"tokens_in":  resp.TokensIn,
				"tokens_out": resp.TokensOut,
				"latency_ms": resp.Latency.Milliseconds(),
			}
			ch <- result{provName, entry}
		}(prov)
	}

	results := make([]map[string]interface{}, 0, len(providers))
	for range providers {
		r := <-ch
		results = append(results, r.data)
	}
	return results
}

// decomposeWith calls a specific provider to decompose a question.
func decomposeWith(ctx context.Context, client *llm.Client, providerName string, questionText string) ([]string, *llm.Response, error) {
	resp, err := client.CompleteWith(ctx, providerName, decomposeRequest(questionText))
	if err != nil {
		return nil, nil, err
	}
	claims, err := parseJSONArray(resp.Content)
	if err != nil {
		return nil, nil, err
	}
	return claims, resp, nil
}

func decomposeRequest(questionText string) llm.Request {
	return llm.Request{
		Messages: []llm.Message{
			{Role: "system", Content: `Tu es un décomposeur d'assertions. Tu reçois une question complexe et tu la découpes en assertions atomiques — des affirmations vérifiables, indépendantes, formulées de manière factuelle.

Règles :
- Chaque assertion doit pouvoir être soutenue ou démolie par des sources
- Pas d'opinions, pas de questions ouvertes
- Formulation affirmative (pas interrogative)
- Une assertion = une seule idée vérifiable
- Retourne UNIQUEMENT un tableau JSON de strings, sans commentaire

Example :
Question : "Mon bailleur peut-il augmenter le loyer de 40% en zone tendue alors que le bail est encore en cours ?"
Réponse : ["Le bailleur est soumis à l'encadrement des loyers en zone tendue", "Une augmentation de loyer en cours de bail est limitée à l'IRL", "Une augmentation de 40% dépasse le plafond légal de révision annuelle", "Le bail en cours protège le locataire contre les augmentations hors clause de révision"]`},
			{Role: "user", Content: questionText},
		},
		Temperature: 0.3,
		MaxTokens:   2048,
	}
}

// parseJSONArray extracts a JSON string array from LLM output, stripping markdown fences.
func parseJSONArray(content string) ([]string, error) {
	content = strings.TrimSpace(content)
	if strings.HasPrefix(content, "```") {
		lines := strings.Split(content, "\n")
		var cleaned []string
		for _, l := range lines {
			if strings.HasPrefix(l, "```") {
				continue
			}
			cleaned = append(cleaned, l)
		}
		content = strings.Join(cleaned, "\n")
	}
	var arr []string
	if err := json.Unmarshal([]byte(content), &arr); err != nil {
		return nil, err
	}
	return arr, nil
}

// DecomposeQuestion calls the LLM to split a question into atomic assertions (single provider, fallback chain).
func DecomposeQuestion(ctx context.Context, client *llm.Client, questionText string) ([]string, error) {
	resp, err := client.Complete(ctx, llm.Request{
		Messages: []llm.Message{
			{Role: "system", Content: `Tu es un décomposeur d'assertions. Tu reçois une question complexe et tu la découpes en assertions atomiques — des affirmations vérifiables, indépendantes, formulées de manière factuelle.

Règles :
- Chaque assertion doit pouvoir être soutenue ou démolie par des sources
- Pas d'opinions, pas de questions ouvertes
- Formulation affirmative (pas interrogative)
- Une assertion = une seule idée vérifiable
- Retourne UNIQUEMENT un tableau JSON de strings, sans commentaire

Example :
Question : "Mon bailleur peut-il augmenter le loyer de 40% en zone tendue alors que le bail est encore en cours ?"
Réponse : ["Le bailleur est soumis à l'encadrement des loyers en zone tendue", "Une augmentation de loyer en cours de bail est limitée à l'IRL", "Une augmentation de 40% dépasse le plafond légal de révision annuelle", "Le bail en cours protège le locataire contre les augmentations hors clause de révision"]`},
			{Role: "user", Content: questionText},
		},
		Temperature: 0.3,
		MaxTokens:   2048,
	})
	if err != nil {
		return nil, err
	}
	if resp == nil {
		return nil, fmt.Errorf("no LLM provider available")
	}

	// Parse JSON array from LLM response
	content := strings.TrimSpace(resp.Content)
	// Strip markdown code block if present
	if strings.HasPrefix(content, "```") {
		lines := strings.Split(content, "\n")
		var cleaned []string
		for _, l := range lines {
			if strings.HasPrefix(l, "```") {
				continue
			}
			cleaned = append(cleaned, l)
		}
		content = strings.Join(cleaned, "\n")
	}

	var assertions []string
	if err := json.Unmarshal([]byte(content), &assertions); err != nil {
		return nil, err
	}
	return assertions, nil
}

// Extract5W1H calls the LLM to extract 5W1H dimensions from source text.
func Extract5W1H(ctx context.Context, client *llm.Client, sourceText string) (map[string][]string, error) {
	resp, err := client.Complete(ctx, llm.Request{
		Messages: []llm.Message{
			{Role: "system", Content: `Tu extrais les dimensions 5W1H d'un document source anonymisé.
Pour chaque dimension, liste les éléments identifiés.
Retourne UNIQUEMENT un objet JSON avec les clés : who, what, when, where, why, how.
Chaque valeur est un tableau de strings. Si une dimension est absente, tableau vide.`},
			{Role: "user", Content: sourceText},
		},
		Temperature: 0.2,
		MaxTokens:   2048,
	})
	if err != nil {
		return nil, err
	}
	if resp == nil {
		return nil, fmt.Errorf("no LLM provider available")
	}

	content := strings.TrimSpace(resp.Content)
	if strings.HasPrefix(content, "```") {
		lines := strings.Split(content, "\n")
		var cleaned []string
		for _, l := range lines {
			if strings.HasPrefix(l, "```") {
				continue
			}
			cleaned = append(cleaned, l)
		}
		content = strings.Join(cleaned, "\n")
	}

	var dims map[string][]string
	if err := json.Unmarshal([]byte(content), &dims); err != nil {
		return nil, err
	}
	return dims, nil
}
