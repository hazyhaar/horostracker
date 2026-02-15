// Package mcp registers the core horostracker tools on an MCP server.
// These tools are accessible to any MCP client connecting via QUIC (ALPN horos-mcp-v1).
package mcp

import (
	"context"
	"encoding/json"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/hazyhaar/horostracker/internal/db"
	"github.com/hazyhaar/pkg/audit"
	"github.com/hazyhaar/pkg/kit"
)

// NewServer creates an MCPServer with all core horostracker tools registered.
func NewServer(database *db.DB, auditLog audit.Logger) *server.MCPServer {
	srv := server.NewMCPServer(
		"horostracker",
		"0.1.0",
		server.WithToolCapabilities(true),
	)

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

// --- ask_question ---

func registerAskQuestion(srv *server.MCPServer, database *db.DB, auditLog audit.Logger) {
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

	schema, _ := json.Marshal(map[string]any{
		"type": "object",
		"properties": map[string]any{
			"body":      map[string]string{"type": "string", "description": "The question text"},
			"author_id": map[string]string{"type": "string", "description": "User ID of the asker"},
			"tags":      map[string]any{"type": "array", "items": map[string]string{"type": "string"}, "description": "Optional tags"},
		},
		"required": []string{"body", "author_id"},
	})
	tool := mcp.NewToolWithRawSchema("ask_question", "Post a new question to the proof tree", schema)

	kit.RegisterMCPTool(srv, tool, endpoint, func(req mcp.CallToolRequest) (*kit.MCPDecodeResult, error) {
		args := req.GetArguments()
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

func registerAnswerNode(srv *server.MCPServer, database *db.DB, auditLog audit.Logger) {
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

	schema, _ := json.Marshal(map[string]any{
		"type": "object",
		"properties": map[string]any{
			"parent_id": map[string]string{"type": "string", "description": "Parent node ID"},
			"body":      map[string]string{"type": "string", "description": "Response text"},
			"author_id": map[string]string{"type": "string", "description": "User ID"},
			"node_type": map[string]string{"type": "string", "description": "One of: answer, evidence, objection, precision, correction, synthesis, llm"},
			"model_id":  map[string]string{"type": "string", "description": "LLM model ID if this is an LLM-generated response"},
			"tags":      map[string]any{"type": "array", "items": map[string]string{"type": "string"}},
		},
		"required": []string{"parent_id", "body", "author_id"},
	})
	tool := mcp.NewToolWithRawSchema("answer_node", "Add a child node (answer, evidence, objection, etc.) to the proof tree", schema)

	kit.RegisterMCPTool(srv, tool, endpoint, func(req mcp.CallToolRequest) (*kit.MCPDecodeResult, error) {
		args := req.GetArguments()
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

func registerGetTree(srv *server.MCPServer, database *db.DB) {
	schema, _ := json.Marshal(map[string]any{
		"type": "object",
		"properties": map[string]any{
			"node_id":   map[string]string{"type": "string", "description": "Root node ID of the tree"},
			"max_depth": map[string]any{"type": "integer", "description": "Maximum depth to retrieve", "default": 50},
		},
		"required": []string{"node_id"},
	})
	tool := mcp.NewToolWithRawSchema("get_tree", "Retrieve the full proof tree from a root node", schema)

	kit.RegisterMCPTool(srv, tool, func(ctx context.Context, request any) (any, error) {
		r := request.(*getTreeReq)
		depth := r.MaxDepth
		if depth <= 0 {
			depth = 50
		}
		return database.GetTree(r.NodeID, depth)
	}, func(req mcp.CallToolRequest) (*kit.MCPDecodeResult, error) {
		args := req.GetArguments()
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

func registerGetNode(srv *server.MCPServer, database *db.DB) {
	schema, _ := json.Marshal(map[string]any{
		"type": "object",
		"properties": map[string]any{
			"node_id": map[string]string{"type": "string", "description": "Node ID to retrieve"},
		},
		"required": []string{"node_id"},
	})
	tool := mcp.NewToolWithRawSchema("get_node", "Retrieve a single node by ID", schema)

	kit.RegisterMCPTool(srv, tool, func(ctx context.Context, request any) (any, error) {
		return database.GetNode(request.(*getNodeReq).NodeID)
	}, func(req mcp.CallToolRequest) (*kit.MCPDecodeResult, error) {
		args := req.GetArguments()
		return &kit.MCPDecodeResult{Request: &getNodeReq{NodeID: stringArg(args, "node_id")}}, nil
	})
}

type getNodeReq struct {
	NodeID string `json:"node_id"`
}

// --- search_nodes ---

func registerSearchNodes(srv *server.MCPServer, database *db.DB) {
	schema, _ := json.Marshal(map[string]any{
		"type": "object",
		"properties": map[string]any{
			"query": map[string]string{"type": "string", "description": "FTS5 search query"},
			"limit": map[string]any{"type": "integer", "description": "Max results", "default": 20},
		},
		"required": []string{"query"},
	})
	tool := mcp.NewToolWithRawSchema("search_nodes", "Full-text search across all nodes", schema)

	kit.RegisterMCPTool(srv, tool, func(ctx context.Context, request any) (any, error) {
		r := request.(*searchReq)
		results, err := database.SearchNodes(r.Query, r.Limit)
		if err != nil {
			return nil, err
		}
		return map[string]any{"results": results, "count": len(results)}, nil
	}, func(req mcp.CallToolRequest) (*kit.MCPDecodeResult, error) {
		args := req.GetArguments()
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

func registerVote(srv *server.MCPServer, database *db.DB, auditLog audit.Logger) {
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

	schema, _ := json.Marshal(map[string]any{
		"type": "object",
		"properties": map[string]any{
			"user_id": map[string]string{"type": "string", "description": "Voter user ID"},
			"node_id": map[string]string{"type": "string", "description": "Node to vote on"},
			"value":   map[string]any{"type": "integer", "description": "1 (upvote) or -1 (downvote)", "enum": []int{-1, 1}},
		},
		"required": []string{"user_id", "node_id", "value"},
	})
	tool := mcp.NewToolWithRawSchema("vote", "Upvote or downvote a node", schema)

	kit.RegisterMCPTool(srv, tool, endpoint, func(req mcp.CallToolRequest) (*kit.MCPDecodeResult, error) {
		args := req.GetArguments()
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

func registerListQuestions(srv *server.MCPServer, database *db.DB) {
	schema, _ := json.Marshal(map[string]any{
		"type": "object",
		"properties": map[string]any{
			"limit": map[string]any{"type": "integer", "description": "Max results", "default": 20},
		},
	})
	tool := mcp.NewToolWithRawSchema("list_questions", "List hot questions ordered by temperature and score", schema)

	kit.RegisterMCPTool(srv, tool, func(ctx context.Context, request any) (any, error) {
		r := request.(*listQuestionsReq)
		limit := r.Limit
		if limit <= 0 {
			limit = 20
		}
		return database.GetHotQuestions(limit)
	}, func(req mcp.CallToolRequest) (*kit.MCPDecodeResult, error) {
		args := req.GetArguments()
		return &kit.MCPDecodeResult{Request: &listQuestionsReq{Limit: intArg(args, "limit", 20)}}, nil
	})
}

type listQuestionsReq struct {
	Limit int `json:"limit"`
}

// --- list_bounties ---

func registerListBounties(srv *server.MCPServer, database *db.DB) {
	schema, _ := json.Marshal(map[string]any{
		"type": "object",
		"properties": map[string]any{
			"tag":   map[string]string{"type": "string", "description": "Optional tag filter"},
			"limit": map[string]any{"type": "integer", "description": "Max results", "default": 20},
		},
	})
	tool := mcp.NewToolWithRawSchema("list_bounties", "List active bounties, optionally filtered by tag", schema)

	kit.RegisterMCPTool(srv, tool, func(ctx context.Context, request any) (any, error) {
		r := request.(*listBountiesReq)
		return database.GetBounties(r.Tag, r.Limit)
	}, func(req mcp.CallToolRequest) (*kit.MCPDecodeResult, error) {
		args := req.GetArguments()
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

func registerGetTags(srv *server.MCPServer, database *db.DB) {
	schema, _ := json.Marshal(map[string]any{
		"type": "object",
		"properties": map[string]any{
			"limit": map[string]any{"type": "integer", "description": "Max tags to return", "default": 30},
		},
	})
	tool := mcp.NewToolWithRawSchema("get_tags", "Get popular tags with counts", schema)

	kit.RegisterMCPTool(srv, tool, func(ctx context.Context, request any) (any, error) {
		r := request.(*getTagsReq)
		limit := r.Limit
		if limit <= 0 {
			limit = 30
		}
		return database.GetPopularTags(limit)
	}, func(req mcp.CallToolRequest) (*kit.MCPDecodeResult, error) {
		args := req.GetArguments()
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
