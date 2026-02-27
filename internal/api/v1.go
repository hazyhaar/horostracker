// CLAUDE:SUMMARY V1 route aliases — registers /api/v1/ prefix forwarding to existing /api/ handlers for backward compatibility
package api

import "net/http"

// RegisterV1Routes adds /api/v1/ prefix aliases that forward to the existing /api/ handlers.
// The original /api/ routes remain active for backward compatibility.
func (a *API) RegisterV1Routes(mux *http.ServeMux) {
	// Auth
	mux.HandleFunc("POST /api/v1/register", a.handleRegister)
	mux.HandleFunc("POST /api/v1/login", a.handleLogin)

	// Nodes
	mux.HandleFunc("POST /api/v1/ask", a.handleAsk)
	mux.HandleFunc("POST /api/v1/answer", a.handleAnswer)
	mux.HandleFunc("GET /api/v1/tree/{id}", a.handleGetTree)
	mux.HandleFunc("GET /api/v1/node/{id}", a.handleGetNode)
	mux.HandleFunc("POST /api/v1/search", a.handleSearch)

	// Votes & thanks
	mux.HandleFunc("POST /api/v1/vote", a.handleVote)
	mux.HandleFunc("POST /api/v1/thank", a.handleThank)

	// Tags, bounties, questions
	mux.HandleFunc("GET /api/v1/tags", a.handleGetTags)
	mux.HandleFunc("GET /api/v1/bounties", a.handleGetBounties)
	mux.HandleFunc("POST /api/v1/bounty", a.handleCreateBounty)
	mux.HandleFunc("GET /api/v1/questions", a.handleGetQuestions)

	// User
	mux.HandleFunc("GET /api/v1/user/{handle}", a.handleGetUser)
	mux.HandleFunc("GET /api/v1/me", a.handleGetMe)

	// Integrity
	mux.HandleFunc("GET /api/v1/integrity", a.handleIntegrity)
	mux.HandleFunc("GET /api/v1/integrity/binary", a.handleIntegrityBinary)

	// Slug
	mux.HandleFunc("GET /api/v1/q/{slug}", a.handleGetNodeBySlug)
}
