package api

import (
	"database/sql"
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"strings"

	"github.com/hazyhaar/horostracker/internal/auth"
	"github.com/hazyhaar/horostracker/internal/config"
	"github.com/hazyhaar/horostracker/internal/db"
	"github.com/hazyhaar/horostracker/internal/llm"
)

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
	mux.HandleFunc("POST /api/search", a.handleSearch)

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
		log.Printf("error creating user: %v", err)
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

	var req struct {
		Body string   `json:"body"`
		Tags []string `json:"tags"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if req.Body == "" {
		jsonError(w, "body is required", http.StatusBadRequest)
		return
	}

	node, err := a.db.CreateNode(db.CreateNodeInput{
		NodeType: "question",
		Body:     req.Body,
		AuthorID: claims.UserID,
		Tags:     req.Tags,
	})
	if err != nil {
		log.Printf("error creating question: %v", err)
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}

	// Search for similar questions
	similar, _ := a.db.SearchNodes(req.Body, 5)

	jsonResp(w, http.StatusCreated, map[string]interface{}{
		"node":    node,
		"similar": similar,
	})
}

func (a *API) handleAnswer(w http.ResponseWriter, r *http.Request) {
	claims := a.auth.ExtractClaims(r)
	if claims == nil {
		jsonError(w, "authentication required", http.StatusUnauthorized)
		return
	}

	var req struct {
		ParentID string   `json:"parent_id"`
		Body     string   `json:"body"`
		NodeType string   `json:"node_type"`
		ModelID  *string  `json:"model_id"`
		Metadata string   `json:"metadata"`
		Tags     []string `json:"tags"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if req.ParentID == "" || req.Body == "" {
		jsonError(w, "parent_id and body are required", http.StatusBadRequest)
		return
	}

	validTypes := map[string]bool{
		"answer": true, "evidence": true, "objection": true,
		"precision": true, "correction": true, "synthesis": true, "llm": true,
	}
	if req.NodeType == "" {
		req.NodeType = "answer"
	}
	if !validTypes[req.NodeType] {
		jsonError(w, "invalid node_type", http.StatusBadRequest)
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
		log.Printf("error creating node: %v", err)
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}

	jsonResp(w, http.StatusCreated, node)
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
		log.Printf("error getting tree: %v", err)
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

	results, err := a.db.SearchNodes(req.Query, req.Limit)
	if err != nil {
		log.Printf("search error: %v", err)
		jsonError(w, "search error", http.StatusInternalServerError)
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
		log.Printf("vote error: %v", err)
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
		log.Printf("thank error: %v", err)
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

// --- Helpers ---

func jsonResp(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

func jsonError(w http.ResponseWriter, msg string, status int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}
