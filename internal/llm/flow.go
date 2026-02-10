package llm

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/hazyhaar/horostracker/internal/db"
)

// FlowStep defines a single step in a thinking flow.
type FlowStep struct {
	Name     string `toml:"name"`
	Provider string `toml:"provider"` // provider name or "$TARGET"
	Model    string `toml:"model"`    // model ID or "$TARGET"
	Role     string `toml:"role"`     // "attacker", "defender", "judge", "synthesizer"
	Prompt   string `toml:"prompt"`   // prompt template with {{.Body}}, {{.PreviousResponse}}, etc.
	System   string `toml:"system"`   // system prompt template
}

// FlowConfig defines a complete thinking flow.
type FlowConfig struct {
	Name        string     `toml:"name"`
	Description string     `toml:"description"`
	Steps       []FlowStep `toml:"steps"`
}

// FlowEngine executes thinking flows and persists results to flows.db.
type FlowEngine struct {
	client   *Client
	flowsDB  *db.FlowsDB
	logger   *slog.Logger
}

// NewFlowEngine creates a flow execution engine.
func NewFlowEngine(client *Client, flowsDB *db.FlowsDB, logger *slog.Logger) *FlowEngine {
	return &FlowEngine{client: client, flowsDB: flowsDB, logger: logger}
}

// FlowContext carries data through flow steps.
type FlowContext struct {
	FlowID           string
	NodeID           string // the node this flow operates on
	Body             string // the original question/claim body
	PreviousResponse string // last step's response
	AllResponses     map[string]string // keyed by step name
	TargetProvider   string // provider to use for $TARGET
	TargetModel      string // model to use for $TARGET
}

// FlowResult holds the complete result of a flow execution.
type FlowResult struct {
	FlowID    string
	Steps     []StepResult
	Duration  time.Duration
}

// StepResult holds a single step's result.
type StepResult struct {
	Name     string
	Provider string
	Model    string
	Response *Response
	Error    error
}

// Execute runs a thinking flow with the given context.
func (e *FlowEngine) Execute(ctx context.Context, flow FlowConfig, fctx FlowContext) (*FlowResult, error) {
	if fctx.FlowID == "" {
		fctx.FlowID = db.NewID()
	}
	if fctx.AllResponses == nil {
		fctx.AllResponses = make(map[string]string)
	}

	result := &FlowResult{FlowID: fctx.FlowID}
	start := time.Now()

	for i, step := range flow.Steps {
		e.logger.Info("flow step",
			"flow", flow.Name,
			"step", step.Name,
			"index", i,
			"provider", step.Provider,
		)

		sr, err := e.executeStep(ctx, step, &fctx, i)
		if err != nil {
			sr = StepResult{Name: step.Name, Error: err}
		}
		result.Steps = append(result.Steps, sr)

		// Update context for next step
		if sr.Response != nil {
			fctx.PreviousResponse = sr.Response.Content
			fctx.AllResponses[step.Name] = sr.Response.Content
		}

		// Persist to flows.db
		e.persistStep(fctx, step, sr, i)
	}

	result.Duration = time.Since(start)
	return result, nil
}

func (e *FlowEngine) executeStep(ctx context.Context, step FlowStep, fctx *FlowContext, index int) (StepResult, error) {
	// Resolve provider and model
	provider := step.Provider
	model := step.Model
	if provider == "$TARGET" {
		provider = fctx.TargetProvider
	}
	if model == "$TARGET" {
		model = fctx.TargetModel
	}

	// Render prompt template
	prompt := renderTemplate(step.Prompt, fctx)
	system := renderTemplate(step.System, fctx)

	var messages []Message
	if system != "" {
		messages = append(messages, Message{Role: "system", Content: system})
	}
	messages = append(messages, Message{Role: "user", Content: prompt})

	req := Request{
		Model:    model,
		Messages: messages,
	}

	var resp *Response
	var err error
	if provider != "" {
		resp, err = e.client.CompleteWith(ctx, provider, req)
	} else {
		resp, err = e.client.Complete(ctx, req)
	}

	return StepResult{
		Name:     step.Name,
		Provider: provider,
		Model:    model,
		Response: resp,
		Error:    err,
	}, err
}

func (e *FlowEngine) persistStep(fctx FlowContext, step FlowStep, sr StepResult, index int) {
	if e.flowsDB == nil {
		return
	}

	id := db.NewID()
	var responseRaw, responseParsed, errMsg string
	var tokensIn, tokensOut, latencyMs int
	var finishReason string
	var provider, model string

	if sr.Response != nil {
		responseRaw = sr.Response.Content
		responseParsed = sr.Response.Content
		tokensIn = sr.Response.TokensIn
		tokensOut = sr.Response.TokensOut
		latencyMs = int(sr.Response.Latency.Milliseconds())
		finishReason = sr.Response.FinishReason
		provider = sr.Response.Provider
		model = sr.Response.Model
	} else {
		provider = sr.Provider
		model = sr.Model
	}
	if sr.Error != nil {
		errMsg = sr.Error.Error()
	}

	prompt := renderTemplate(step.Prompt, &fctx)
	systemPrompt := renderTemplate(step.System, &fctx)

	e.flowsDB.Exec(`
		INSERT INTO flow_steps (id, flow_id, step_index, node_id, model_id, provider,
			prompt, system_prompt, response_raw, response_parsed,
			tokens_in, tokens_out, latency_ms, finish_reason, error)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, fctx.FlowID, index, nilIfEmpty(fctx.NodeID),
		model, provider, prompt, systemPrompt,
		responseRaw, responseParsed,
		tokensIn, tokensOut, latencyMs, finishReason, nilIfEmpty(errMsg))
}

// renderTemplate replaces {{.Body}}, {{.PreviousResponse}}, {{.Step.<name>}} in template.
func renderTemplate(tmpl string, fctx *FlowContext) string {
	if tmpl == "" {
		return ""
	}
	s := tmpl
	s = strings.ReplaceAll(s, "{{.Body}}", fctx.Body)
	s = strings.ReplaceAll(s, "{{.PreviousResponse}}", fctx.PreviousResponse)
	for name, resp := range fctx.AllResponses {
		s = strings.ReplaceAll(s, fmt.Sprintf("{{.Step.%s}}", name), resp)
	}
	return s
}

func nilIfEmpty(s string) interface{} {
	if s == "" {
		return nil
	}
	return s
}
