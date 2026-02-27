// CLAUDE:SUMMARY MCP server setup — registers core horostracker tools (ask, answer, vote, search, bounties) over QUIC
// Package mcp registers the core horostracker tools on an MCP server.
// These tools are accessible to any MCP client connecting via QUIC (ALPN horos-mcp-v1).
package mcp

import (
	"context"
	"encoding/json"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/hazyhaar/horostracker/internal/db"
	"github.com/hazyhaar/pkg/audit"
	"github.com/hazyhaar/pkg/kit"
)

// NewServer creates an MCP Server with all core horostracker tools registered.
func NewServer(database *db.DB, auditLog audit.Logger) *mcp.Server {
	srv := mcp.NewServer(&mcp.Implementation{
		Name:    "horostracker",
		Version: "0.1.0",
	}, nil)

	registerAskQuestion(srv, database, auditLog)
	registerAnswerNode(srv, database, auditLog)
	registerGetTree(srv, database)
	registerGetNode(srv, database)
	registerSearchNodes(srv, database)
	registerVote(srv, database, auditLog)
	registerListQuestions(srv, database)
	registerListBounties(srv, database)
	registerGetTags(srv, database)

	return srv
}

// decodeArgs unmarshals req.Params.Arguments (json.RawMessage) into map[string]any.
func decodeArgs(req *mcp.CallToolRequest) map[string]any {
	args := make(map[string]any)
	if req.Params.Arguments != nil {
		_ = json.Unmarshal(req.Params.Arguments, &args)
	}
	return args
}

// --- ask_question ---

func registerAskQuestion(srv *mcp.Server, database *db.DB, auditLog audit.Logger) {
	var endpoint kit.Endpoint = func(ctx context.Context, request any) (any, error) {
		r := request.(*askQuestionReq)
		node, err := database.CreateNode(db.CreateNodeInput{
			NodeType: "question",
			Body:     r.Body,
			AuthorID: r.AuthorID,
			Tags:     r.Tags,
		})
		if err != nil {
			return nil, err
		}
		similar, _ := database.SearchNodes(r.Body, 5)
		return map[string]any{"node": node, "similar": similar}, nil
	}
	if auditLog != nil {
		endpoint = audit.Middleware(auditLog, "ask_question")(endpoint)
	}

	schema := json.RawMessage(`{
		"type": "object",
		"properties": {
			"body":      {"type": "string", "description": "The question text"},
			"author_id": {"type": "string", "description": "User ID of the asker"},
			"tags":      {"type": "array", "items": {"type": "string"}, "description": "Optional tags"}
		},
		"required": ["body", "author_id"]
	}`)
	tool := &mcp.Tool{
		Name:        "ask_question",
		Description: "Post a new question to the proof tree",
		InputSchema: schema,
	}

	kit.RegisterMCPTool(srv, tool, endpoint, func(req *mcp.CallToolRequest) (*kit.MCPDecodeResult, error) {
		args := decodeArgs(req)
		r := &askQuestionReq{
			Body:     stringArg(args, "body"),
			AuthorID: stringArg(args, "author_id"),
		}
		if tags, ok := args["tags"].([]any); ok {
			for _, t := range tags {
				if s, ok := t.(string); ok {
					r.Tags = append(r.Tags, s)
				}
			}
		}
		return &kit.MCPDecodeResult{Request: r}, nil
	})
}

type askQuestionReq struct {
	Body     string   `json:"body"`
	AuthorID string   `json:"author_id"`
	Tags     []string `json:"tags"`
}

// --- answer_node ---

func registerAnswerNode(srv *mcp.Server, database *db.DB, auditLog audit.Logger) {
	var endpoint kit.Endpoint = func(ctx context.Context, request any) (any, error) {
		r := request.(*answerNodeReq)
		nodeType := r.NodeType
		if nodeType == "" {
			nodeType = "answer"
		}
		return database.CreateNode(db.CreateNodeInput{
			ParentID: &r.ParentID,
			NodeType: nodeType,
			Body:     r.Body,
			AuthorID: r.AuthorID,
			ModelID:  r.ModelID,
			Tags:     r.Tags,
		})
	}
	if auditLog != nil {
		endpoint = audit.Middleware(auditLog, "answer_node")(endpoint)
	}

	schema := json.RawMessage(`{
		"type": "object",
		"properties": {
			"parent_id": {"type": "string", "description": "Parent node ID"},
			"body":      {"type": "string", "description": "Response text"},
			"author_id": {"type": "string", "description": "User ID"},
			"node_type": {"type": "string", "description": "One of: answer, evidence, objection, precision, correction, synthesis, llm"},
			"model_id":  {"type": "string", "description": "LLM model ID if this is an LLM-generated response"},
			"tags":      {"type": "array", "items": {"type": "string"}}
		},
		"required": ["parent_id", "body", "author_id"]
	}`)
	tool := &mcp.Tool{
		Name:        "answer_node",
		Description: "Add a child node (answer, evidence, objection, etc.) to the proof tree",
		InputSchema: schema,
	}

	kit.RegisterMCPTool(srv, tool, endpoint, func(req *mcp.CallToolRequest) (*kit.MCPDecodeResult, error) {
		args := decodeArgs(req)
		r := &answerNodeReq{
			ParentID: stringArg(args, "parent_id"),
			Body:     stringArg(args, "body"),
			AuthorID: stringArg(args, "author_id"),
			NodeType: stringArg(args, "node_type"),
		}
		if mid := stringArg(args, "model_id"); mid != "" {
			r.ModelID = &mid
		}
		if tags, ok := args["tags"].([]any); ok {
			for _, t := range tags {
				if s, ok := t.(string); ok {
					r.Tags = append(r.Tags, s)
				}
			}
		}
		return &kit.MCPDecodeResult{Request: r}, nil
	})
}

type answerNodeReq struct {
	ParentID string   `json:"parent_id"`
	Body     string   `json:"body"`
	AuthorID string   `json:"author_id"`
	NodeType string   `json:"node_type"`
	ModelID  *string  `json:"model_id,omitempty"`
	Tags     []string `json:"tags"`
}

// --- get_tree ---

func registerGetTree(srv *mcp.Server, database *db.DB) {
	schema := json.RawMessage(`{
		"type": "object",
		"properties": {
			"node_id":   {"type": "string", "description": "Root node ID of the tree"},
			"max_depth": {"type": "integer", "description": "Maximum depth to retrieve", "default": 50}
		},
		"required": ["node_id"]
	}`)
	tool := &mcp.Tool{
		Name:        "get_tree",
		Description: "Retrieve the full proof tree from a root node",
		InputSchema: schema,
	}

	kit.RegisterMCPTool(srv, tool, func(ctx context.Context, request any) (any, error) {
		r := request.(*getTreeReq)
		depth := r.MaxDepth
		if depth <= 0 {
			depth = 50
		}
		return database.GetTree(r.NodeID, depth)
	}, func(req *mcp.CallToolRequest) (*kit.MCPDecodeResult, error) {
		args := decodeArgs(req)
		return &kit.MCPDecodeResult{Request: &getTreeReq{
			NodeID:   stringArg(args, "node_id"),
			MaxDepth: intArg(args, "max_depth", 50),
		}}, nil
	})
}

type getTreeReq struct {
	NodeID   string `json:"node_id"`
	MaxDepth int    `json:"max_depth"`
}

// --- get_node ---

func registerGetNode(srv *mcp.Server, database *db.DB) {
	schema := json.RawMessage(`{
		"type": "object",
		"properties": {
			"node_id": {"type": "string", "description": "Node ID to retrieve"}
		},
		"required": ["node_id"]
	}`)
	tool := &mcp.Tool{
		Name:        "get_node",
		Description: "Retrieve a single node by ID",
		InputSchema: schema,
	}

	kit.RegisterMCPTool(srv, tool, func(ctx context.Context, request any) (any, error) {
		return database.GetNode(request.(*getNodeReq).NodeID)
	}, func(req *mcp.CallToolRequest) (*kit.MCPDecodeResult, error) {
		args := decodeArgs(req)
		return &kit.MCPDecodeResult{Request: &getNodeReq{NodeID: stringArg(args, "node_id")}}, nil
	})
}

type getNodeReq struct {
	NodeID string `json:"node_id"`
}

// --- search_nodes ---

func registerSearchNodes(srv *mcp.Server, database *db.DB) {
	schema := json.RawMessage(`{
		"type": "object",
		"properties": {
			"query": {"type": "string", "description": "FTS5 search query"},
			"limit": {"type": "integer", "description": "Max results", "default": 20}
		},
		"required": ["query"]
	}`)
	tool := &mcp.Tool{
		Name:        "search_nodes",
		Description: "Full-text search across all nodes",
		InputSchema: schema,
	}

	kit.RegisterMCPTool(srv, tool, func(ctx context.Context, request any) (any, error) {
		r := request.(*searchReq)
		results, err := database.SearchNodes(r.Query, r.Limit)
		if err != nil {
			return nil, err
		}
		return map[string]any{"results": results, "count": len(results)}, nil
	}, func(req *mcp.CallToolRequest) (*kit.MCPDecodeResult, error) {
		args := decodeArgs(req)
		return &kit.MCPDecodeResult{Request: &searchReq{
			Query: stringArg(args, "query"),
			Limit: intArg(args, "limit", 20),
		}}, nil
	})
}

type searchReq struct {
	Query string `json:"query"`
	Limit int    `json:"limit"`
}

// --- vote ---

func registerVote(srv *mcp.Server, database *db.DB, auditLog audit.Logger) {
	var endpoint kit.Endpoint = func(ctx context.Context, request any) (any, error) {
		r := request.(*voteReq)
		if err := database.Vote(r.UserID, r.NodeID, r.Value); err != nil {
			return nil, err
		}
		return map[string]string{"status": "ok"}, nil
	}
	if auditLog != nil {
		endpoint = audit.Middleware(auditLog, "vote")(endpoint)
	}

	schema := json.RawMessage(`{
		"type": "object",
		"properties": {
			"user_id": {"type": "string", "description": "Voter user ID"},
			"node_id": {"type": "string", "description": "Node to vote on"},
			"value":   {"type": "integer", "description": "1 (upvote) or -1 (downvote)", "enum": [-1, 1]}
		},
		"required": ["user_id", "node_id", "value"]
	}`)
	tool := &mcp.Tool{
		Name:        "vote",
		Description: "Upvote or downvote a node",
		InputSchema: schema,
	}

	kit.RegisterMCPTool(srv, tool, endpoint, func(req *mcp.CallToolRequest) (*kit.MCPDecodeResult, error) {
		args := decodeArgs(req)
		return &kit.MCPDecodeResult{Request: &voteReq{
			UserID: stringArg(args, "user_id"),
			NodeID: stringArg(args, "node_id"),
			Value:  intArg(args, "value", 1),
		}}, nil
	})
}

type voteReq struct {
	UserID string `json:"user_id"`
	NodeID string `json:"node_id"`
	Value  int    `json:"value"`
}

// --- list_questions ---

func registerListQuestions(srv *mcp.Server, database *db.DB) {
	schema := json.RawMessage(`{
		"type": "object",
		"properties": {
			"limit": {"type": "integer", "description": "Max results", "default": 20}
		}
	}`)
	tool := &mcp.Tool{
		Name:        "list_questions",
		Description: "List hot questions ordered by temperature and score",
		InputSchema: schema,
	}

	kit.RegisterMCPTool(srv, tool, func(ctx context.Context, request any) (any, error) {
		r := request.(*listQuestionsReq)
		limit := r.Limit
		if limit <= 0 {
			limit = 20
		}
		return database.GetHotQuestions(limit)
	}, func(req *mcp.CallToolRequest) (*kit.MCPDecodeResult, error) {
		args := decodeArgs(req)
		return &kit.MCPDecodeResult{Request: &listQuestionsReq{Limit: intArg(args, "limit", 20)}}, nil
	})
}

type listQuestionsReq struct {
	Limit int `json:"limit"`
}

// --- list_bounties ---

func registerListBounties(srv *mcp.Server, database *db.DB) {
	schema := json.RawMessage(`{
		"type": "object",
		"properties": {
			"tag":   {"type": "string", "description": "Optional tag filter"},
			"limit": {"type": "integer", "description": "Max results", "default": 20}
		}
	}`)
	tool := &mcp.Tool{
		Name:        "list_bounties",
		Description: "List active bounties, optionally filtered by tag",
		InputSchema: schema,
	}

	kit.RegisterMCPTool(srv, tool, func(ctx context.Context, request any) (any, error) {
		r := request.(*listBountiesReq)
		return database.GetBounties(r.Tag, r.Limit)
	}, func(req *mcp.CallToolRequest) (*kit.MCPDecodeResult, error) {
		args := decodeArgs(req)
		return &kit.MCPDecodeResult{Request: &listBountiesReq{
			Tag:   stringArg(args, "tag"),
			Limit: intArg(args, "limit", 20),
		}}, nil
	})
}

type listBountiesReq struct {
	Tag   string `json:"tag"`
	Limit int    `json:"limit"`
}

// --- get_tags ---

func registerGetTags(srv *mcp.Server, database *db.DB) {
	schema := json.RawMessage(`{
		"type": "object",
		"properties": {
			"limit": {"type": "integer", "description": "Max tags to return", "default": 30}
		}
	}`)
	tool := &mcp.Tool{
		Name:        "get_tags",
		Description: "Get popular tags with counts",
		InputSchema: schema,
	}

	kit.RegisterMCPTool(srv, tool, func(ctx context.Context, request any) (any, error) {
		r := request.(*getTagsReq)
		limit := r.Limit
		if limit <= 0 {
			limit = 30
		}
		return database.GetPopularTags(limit)
	}, func(req *mcp.CallToolRequest) (*kit.MCPDecodeResult, error) {
		args := decodeArgs(req)
		return &kit.MCPDecodeResult{Request: &getTagsReq{Limit: intArg(args, "limit", 30)}}, nil
	})
}

type getTagsReq struct {
	Limit int `json:"limit"`
}

// --- helpers ---

func stringArg(args map[string]any, key string) string {
	v, _ := args[key].(string)
	return v
}

func intArg(args map[string]any, key string, def int) int {
	switch v := args[key].(type) {
	case float64:
		return int(v)
	case int:
		return v
	case json.Number:
		n, _ := v.Int64()
		return int(n)
	default:
		return def
	}
}
