package llm

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/hazyhaar/horostracker/internal/db"
)

// ReplayEngine replays flow steps with different models for comparison.
type ReplayEngine struct {
	client  *Client
	flowsDB *db.FlowsDB
	logger  *slog.Logger
}

// NewReplayEngine creates a replay execution engine.
func NewReplayEngine(client *Client, flowsDB *db.FlowsDB, logger *slog.Logger) *ReplayEngine {
	return &ReplayEngine{client: client, flowsDB: flowsDB, logger: logger}
}

// ReplayResult holds the outcome of replaying a single step.
type ReplayResult struct {
	OriginalStepID string        `json:"original_step_id"`
	ReplayStepID   string        `json:"replay_step_id"`
	Provider       string        `json:"provider"`
	Model          string        `json:"model"`
	Content        string        `json:"content"`
	TokensIn       int           `json:"tokens_in"`
	TokensOut      int           `json:"tokens_out"`
	LatencyMs      int           `json:"latency_ms"`
	Error          string        `json:"error,omitempty"`
}

// ReplayStep replays a single flow step with a different model.
func (re *ReplayEngine) ReplayStep(ctx context.Context, stepID, provider, model string) (*ReplayResult, error) {
	// Load original step
	orig, err := re.loadStep(stepID)
	if err != nil {
		return nil, fmt.Errorf("loading step %s: %w", stepID, err)
	}

	// Build messages from original prompt
	var messages []Message
	if orig.SystemPrompt != "" {
		messages = append(messages, Message{Role: "system", Content: orig.SystemPrompt})
	}
	messages = append(messages, Message{Role: "user", Content: orig.Prompt})

	// Route model
	modelStr := model
	if provider != "" {
		modelStr = provider + "/" + model
	}

	req := Request{
		Model:    modelStr,
		Messages: messages,
	}

	start := time.Now()
	resp, err := re.client.Complete(ctx, req)
	latency := time.Since(start)

	result := &ReplayResult{
		OriginalStepID: stepID,
		Provider:       provider,
		Model:          model,
		LatencyMs:      int(latency.Milliseconds()),
	}

	if err != nil {
		result.Error = err.Error()
		// Still persist the failed replay
		replayID := re.persistReplay(orig, result, provider, model)
		result.ReplayStepID = replayID
		return result, nil
	}

	result.Content = resp.Content
	result.TokensIn = resp.TokensIn
	result.TokensOut = resp.TokensOut
	result.Provider = resp.Provider
	result.Model = resp.Model

	replayID := re.persistReplay(orig, result, resp.Provider, resp.Model)
	result.ReplayStepID = replayID

	re.logger.Info("step replayed",
		"original", stepID,
		"replay", replayID,
		"provider", resp.Provider,
		"model", resp.Model,
		"latency_ms", result.LatencyMs,
	)

	return result, nil
}

// ReplayBatchResult holds the outcome of a bulk replay.
type ReplayBatchResult struct {
	BatchID      string `json:"batch_id"`
	TotalSteps   int    `json:"total_steps"`
	Completed    int    `json:"completed"`
	Failed       int    `json:"failed"`
	Status       string `json:"status"`
}

// ReplayBulk replays all steps matching a filter with a new model.
func (re *ReplayEngine) ReplayBulk(ctx context.Context, batchID, filterModel, replayProvider, replayModel, filterTag string) (*ReplayBatchResult, error) {
	// Query matching steps
	query := `SELECT id FROM flow_steps WHERE replay_of_id IS NULL`
	args := []interface{}{}

	if filterModel != "" {
		query += ` AND model_id = ?`
		args = append(args, filterModel)
	}
	query += ` ORDER BY created_at`

	rows, err := re.flowsDB.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("querying steps: %w", err)
	}
	defer rows.Close()

	var stepIDs []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			continue
		}
		stepIDs = append(stepIDs, id)
	}

	// Create batch record
	re.flowsDB.Exec(`INSERT INTO replay_batches (id, original_model, replay_model, scope, filter_tag, total_steps, status)
		VALUES (?, ?, ?, ?, ?, ?, 'running')`,
		batchID, filterModel, replayProvider+"/"+replayModel, "filtered", filterTag, len(stepIDs))

	result := &ReplayBatchResult{
		BatchID:    batchID,
		TotalSteps: len(stepIDs),
		Status:     "running",
	}

	// Replay each step
	for _, sid := range stepIDs {
		select {
		case <-ctx.Done():
			result.Status = "failed"
			re.updateBatch(batchID, result)
			return result, ctx.Err()
		default:
		}

		_, err := re.ReplayStep(ctx, sid, replayProvider, replayModel)
		if err != nil {
			result.Failed++
		} else {
			result.Completed++
		}
	}

	result.Status = "completed"
	re.updateBatch(batchID, result)
	return result, nil
}

type originalStep struct {
	ID           string
	FlowID       string
	StepIndex    int
	NodeID       string
	ModelID      string
	Provider     string
	Prompt       string
	SystemPrompt string
}

func (re *ReplayEngine) loadStep(id string) (*originalStep, error) {
	row := re.flowsDB.QueryRow(`
		SELECT id, flow_id, step_index, COALESCE(node_id,''), model_id, provider, prompt, COALESCE(system_prompt,'')
		FROM flow_steps WHERE id = ?`, id)

	var s originalStep
	err := row.Scan(&s.ID, &s.FlowID, &s.StepIndex, &s.NodeID, &s.ModelID, &s.Provider, &s.Prompt, &s.SystemPrompt)
	if err != nil {
		return nil, err
	}
	return &s, nil
}

func (re *ReplayEngine) persistReplay(orig *originalStep, result *ReplayResult, provider, model string) string {
	id := db.NewID()
	errStr := ""
	if result.Error != "" {
		errStr = result.Error
	}

	re.flowsDB.Exec(`
		INSERT INTO flow_steps (id, flow_id, step_index, node_id, model_id, provider,
			prompt, system_prompt, response_raw, response_parsed,
			tokens_in, tokens_out, latency_ms, replay_of_id, error)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, orig.FlowID, orig.StepIndex, nilIfEmpty(orig.NodeID),
		model, provider, orig.Prompt, nilIfEmpty(orig.SystemPrompt),
		result.Content, result.Content,
		result.TokensIn, result.TokensOut, result.LatencyMs,
		orig.ID, nilIfEmpty(errStr))

	return id
}

func (re *ReplayEngine) updateBatch(batchID string, result *ReplayBatchResult) {
	re.flowsDB.Exec(`UPDATE replay_batches SET status = ?, improvements = ?, regressions = ?, unchanged = ?, completed_at = datetime('now')
		WHERE id = ?`,
		result.Status, result.Completed, result.Failed, result.TotalSteps-result.Completed-result.Failed, batchID)
}
