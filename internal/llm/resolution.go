// CLAUDE:SUMMARY Resolution engine — synthesizes proof trees into structured dialogues between argumentative lines
package llm

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/hazyhaar/horostracker/internal/db"
)

// ResolutionEngine generates and manages Resolutions for proof trees.
type ResolutionEngine struct {
	client  *Client
	flowsDB *db.FlowsDB
	logger  *slog.Logger
}

// NewResolutionEngine creates a Resolution generation engine.
func NewResolutionEngine(client *Client, flowsDB *db.FlowsDB, logger *slog.Logger) *ResolutionEngine {
	return &ResolutionEngine{client: client, flowsDB: flowsDB, logger: logger}
}

// GenerateResolution produces a structured dialogue from a proof tree.
// The tree is serialized to text, then an LLM synthesizes it into a Resolution.
//
//nolint:misspell // French-language LLM prompts
func (e *ResolutionEngine) GenerateResolution(ctx context.Context, tree *db.Node, provider, model string) (*ResolutionResult, error) {
	treeText := serializeTree(tree, 0)
	if treeText == "" {
		return nil, fmt.Errorf("empty tree")
	}

	messages := []Message{
		{
			Role: "system",
			Content: `Tu es un générateur de Résolution pour une raffinerie de connaissances.

Une Résolution est un dialogue structuré entre LIGNES ARGUMENTATIVES (pas entre personnes).
Chaque ligne argumentative représente une position défendue dans l'arbre de preuves.

Format de sortie :
1. CONTEXTE — résumé de la question en 2-3 phrases
2. LIGNES ARGUMENTATIVES — identifier chaque position distincte
3. DIALOGUE — échange structuré entre les lignes, avec références aux sources
4. POINTS DE CONVERGENCE — ce sur quoi les lignes s'accordent
5. POINTS DE DIVERGENCE — ce qui reste contesté, avec nuances
6. INCERTITUDES — ce qu'on ne sait pas encore
7. VERDICT — synthèse équilibrée avec degré de confiance

Règles :
- Fidélité absolue à l'arbre source — ne rien inventer
- Chaque affirmation doit être traçable à un nœud de l'arbre
- Les sources sont citées inline [source: URL]
- Les votes et scores reflètent le poids communautaire
- La température indique le niveau de controverse`,
		},
		{
			Role: "user",
			Content: fmt.Sprintf("Génère une Résolution pour cet arbre de preuves :\n\n%s", treeText),
		},
	}

	req := Request{
		Model:       model,
		Messages:    messages,
		Temperature: 0.3,
		MaxTokens:   4096,
	}

	var resp *Response
	var err error
	if provider != "" {
		resp, err = e.client.CompleteWith(ctx, provider, req)
	} else {
		resp, err = e.client.Complete(ctx, req)
	}
	if err != nil {
		return nil, fmt.Errorf("generating resolution: %w", err)
	}

	// Persist to flows.db
	if e.flowsDB != nil {
		stepID := db.NewID()
		flowID := "res_" + db.NewID()
		_, _ = e.flowsDB.Exec(`
			INSERT INTO flow_steps (id, flow_id, step_index, node_id, model_id, provider,
				prompt, system_prompt, response_raw, response_parsed,
				tokens_in, tokens_out, latency_ms, finish_reason)
			VALUES (?, ?, 0, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			stepID, flowID, tree.ID,
			resp.Model, resp.Provider,
			messages[1].Content, messages[0].Content,
			resp.Content, resp.Content,
			resp.TokensIn, resp.TokensOut,
			int(resp.Latency.Milliseconds()), resp.FinishReason)
	}

	return &ResolutionResult{
		Content:  resp.Content,
		Provider: resp.Provider,
		Model:    resp.Model,
		TokensIn: resp.TokensIn,
		TokensOut: resp.TokensOut,
		LatencyMs: int(resp.Latency.Milliseconds()),
	}, nil
}

// ResolutionResult holds the output of a Resolution generation.
type ResolutionResult struct {
	Content   string `json:"content"`
	Provider  string `json:"provider"`
	Model     string `json:"model"`
	TokensIn  int    `json:"tokens_in"`
	TokensOut int    `json:"tokens_out"`
	LatencyMs int    `json:"latency_ms"`
}

// RenderResolution transforms a Resolution into a specific format.
//
//nolint:misspell // French-language LLM prompts
func (e *ResolutionEngine) RenderResolution(ctx context.Context, resolution string, format string, provider, model string) (*RenderResult, error) {
	prompts := map[string]string{
		"article": "Transforme cette Résolution en un article clair et lisible, avec titre, chapô, et paragraphes structurés. Conserve toutes les sources et nuances.",
		"faq":     "Transforme cette Résolution en une FAQ (questions-réponses). Chaque question couvre un aspect clé du débat. Les réponses citent les sources.",
		"thread":  "Transforme cette Résolution en un thread X (Twitter). Maximum 15 tweets. Chaque tweet est autonome mais s'enchaîne logiquement. Utilise des emojis avec parcimonie. Le premier tweet accroche, le dernier conclut.",
		"summary": "Résume cette Résolution en 3 paragraphes maximum. L'essentiel, les points clés, la conclusion.",
	}

	prompt, ok := prompts[format]
	if !ok {
		return nil, fmt.Errorf("unsupported render format: %s (supported: article, faq, thread, summary)", format)
	}

	messages := []Message{
		{Role: "system", Content: "Tu transformes des Résolutions en différents formats médias. Fidélité absolue au contenu source."},
		{Role: "user", Content: fmt.Sprintf("%s\n\nRésolution source :\n\n%s", prompt, resolution)},
	}

	req := Request{
		Model:       model,
		Messages:    messages,
		Temperature: 0.4,
		MaxTokens:   4096,
	}

	var resp *Response
	var err error
	if provider != "" {
		resp, err = e.client.CompleteWith(ctx, provider, req)
	} else {
		resp, err = e.client.Complete(ctx, req)
	}
	if err != nil {
		return nil, fmt.Errorf("rendering resolution: %w", err)
	}

	return &RenderResult{
		Format:   format,
		Content:  resp.Content,
		Provider: resp.Provider,
		Model:    resp.Model,
	}, nil
}

// RenderResult holds a rendered format of a Resolution.
type RenderResult struct {
	Format   string `json:"format"`
	Content  string `json:"content"`
	Provider string `json:"provider"`
	Model    string `json:"model"`
}

// serializeTree converts a node tree to a readable text representation.
func serializeTree(node *db.Node, depth int) string {
	if node == nil {
		return ""
	}
	var b strings.Builder
	indent := strings.Repeat("  ", depth)

	// Node header
	fmt.Fprintf(&b, "%s[%s] (score:%d, temp:%s", indent, node.NodeType, node.Score, node.Temperature)
	if node.ModelID != nil {
		fmt.Fprintf(&b, ", model:%s", *node.ModelID)
	}
	b.WriteString(")\n")

	// Body
	lines := strings.Split(node.Body, "\n")
	for _, line := range lines {
		fmt.Fprintf(&b, "%s  %s\n", indent, line)
	}

	// Children
	for _, child := range node.Children {
		b.WriteString(serializeTree(child, depth+1))
	}

	return b.String()
}
