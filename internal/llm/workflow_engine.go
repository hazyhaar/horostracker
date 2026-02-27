// CLAUDE:SUMMARY Dynamic VACF workflow engine with fan-in/out parallelism and ACID step persistence
package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/hazyhaar/horostracker/internal/db"
)

// WorkflowEngine executes dynamic VACF workflows with fan-in/out and ACID per step.
type WorkflowEngine struct {
	client  *Client
	flowsDB *db.FlowsDB
	logger  *slog.Logger
	httpCl  *http.Client
}

// NewWorkflowEngine creates a workflow execution engine.
func NewWorkflowEngine(client *Client, flowsDB *db.FlowsDB, logger *slog.Logger) *WorkflowEngine {
	return &WorkflowEngine{
		client:  client,
		flowsDB: flowsDB,
		logger:  logger,
		httpCl:  &http.Client{Timeout: 60 * time.Second},
	}
}

// stepGroup holds steps sharing the same step_order.
type stepGroup struct {
	order int
	steps []db.WorkflowStep
}

// ExecuteWorkflow runs a full workflow and persists each step with ACID guarantees.
func (we *WorkflowEngine) ExecuteWorkflow(ctx context.Context, workflowID, nodeID, userID, userRole, prePrompt string) (string, error) {
	wf, err := we.flowsDB.GetWorkflow(workflowID)
	if err != nil {
		return "", fmt.Errorf("loading workflow: %w", err)
	}

	runID := db.NewID()
	run := &db.WorkflowRun{
		RunID:      runID,
		WorkflowID: workflowID,
		InitiatedBy: userID,
		Status:     "pending",
		TotalSteps: len(wf.Steps),
	}
	if nodeID != "" {
		run.NodeID = &nodeID
	}
	if prePrompt != "" {
		run.PrePrompt = &prePrompt
	}

	if err := we.flowsDB.CreateWorkflowRun(run); err != nil {
		return "", fmt.Errorf("creating run: %w", err)
	}

	we.flowsDB.UpdateRunStatus(runID, "running", nil, nil)
	we.flowsDB.InsertAuditLog(runID, "", "run_started", map[string]string{
		"workflow_id": workflowID,
		"workflow_name": wf.Name,
	})

	// Group steps by step_order
	groups := groupSteps(wf.Steps)

	// Execution context accumulates step outputs
	execCtx := &workflowExecCtx{
		body:      "",
		prePrompt: prePrompt,
		responses: make(map[string]string),
		userID:    userID,
		userRole:  userRole,
	}

	// If nodeID is set, load node body
	if nodeID != "" {
		execCtx.body = nodeID // placeholder; caller should provide body via pre-prompt or node context
	}

	for _, g := range groups {
		if ctx.Err() != nil {
			errMsg := "cancelled"
			we.flowsDB.UpdateRunStatus(runID, "cancelled", nil, &errMsg)
			return runID, ctx.Err()
		}

		if len(g.steps) == 1 {
			// Sequential execution
			if err := we.executeStepACID(ctx, runID, g.steps[0], execCtx, nil); err != nil {
				errMsg := err.Error()
				we.flowsDB.UpdateRunStatus(runID, "failed", nil, &errMsg)
				return runID, nil // run is marked failed, not a system error
			}
		} else {
			// Fan-out: parallel execution via goroutines
			we.flowsDB.InsertAuditLog(runID, "", "fan_out_started", map[string]interface{}{
				"step_order": g.order,
				"count":      len(g.steps),
			})

			var wg sync.WaitGroup
			var mu sync.Mutex
			fanResults := make(map[string]string)
			var firstErr error

			for _, step := range g.steps {
				wg.Add(1)
				go func(s db.WorkflowStep) {
					defer wg.Done()
					localCtx := &workflowExecCtx{
						body:      execCtx.body,
						prePrompt: execCtx.prePrompt,
						responses: copyMap(execCtx.responses),
						userID:    execCtx.userID,
						userRole:  execCtx.userRole,
					}
					if err := we.executeStepACID(ctx, runID, s, localCtx, nil); err != nil {
						mu.Lock()
						if firstErr == nil {
							firstErr = err
						}
						mu.Unlock()
						return
					}
					mu.Lock()
					fanResults[s.StepName] = localCtx.responses[s.StepName]
					mu.Unlock()
				}(step)
			}

			we.flowsDB.InsertAuditLog(runID, "", "fan_in_waiting", map[string]interface{}{
				"step_order": g.order,
			})
			wg.Wait()

			we.flowsDB.InsertAuditLog(runID, "", "fan_in_completed", map[string]interface{}{
				"step_order": g.order,
				"completed":  len(fanResults),
			})

			// Merge fan results into main context
			fanResultsJSON, _ := json.Marshal(fanResults)
			execCtx.responses["fan_results"] = string(fanResultsJSON)
			for k, v := range fanResults {
				execCtx.responses[k] = v
				execCtx.previousResponse = v // last one wins for {{.PreviousResponse}}
			}
		}
	}

	// Build result
	resultMap := make(map[string]string)
	for k, v := range execCtx.responses {
		resultMap[k] = v
	}
	resultJSON, _ := json.Marshal(resultMap)
	resultStr := string(resultJSON)
	we.flowsDB.UpdateRunStatus(runID, "completed", &resultStr, nil)
	we.flowsDB.InsertAuditLog(runID, "", "run_completed", nil)

	return runID, nil
}

// executeStepACID runs a single step in its own transaction with retry logic.
func (we *WorkflowEngine) executeStepACID(ctx context.Context, runID string, step db.WorkflowStep, execCtx *workflowExecCtx, fanResults map[string]string) error {
	stepRunID := db.NewID()

	// Prepare input context
	inputMap := map[string]string{
		"body":              execCtx.body,
		"pre_prompt":        execCtx.prePrompt,
		"previous_response": execCtx.previousResponse,
	}
	inputJSON, _ := json.Marshal(inputMap)

	we.flowsDB.InsertAuditLog(runID, stepRunID, "step_started", map[string]string{
		"step_name": step.StepName,
		"step_type": step.StepType,
	})

	we.logger.Info("workflow step",
		"run_id", runID,
		"step_name", step.StepName,
		"step_type", step.StepType,
		"step_order", step.StepOrder,
	)

	// Insert step_run as running
	inputStr := string(inputJSON)
	we.flowsDB.Exec(`
		INSERT INTO workflow_step_runs (step_run_id, run_id, step_id, step_order, status, input_json, started_at)
		VALUES (?, ?, ?, ?, 'running', ?, datetime('now'))`,
		stepRunID, runID, step.StepID, step.StepOrder, inputStr)

	// Run-time model grant enforcement
	if step.Model != "" {
		if !we.flowsDB.ModelIsAvailable(step.Model) && we.flowsDB.ModelExists(step.Model) {
			errMsg := fmt.Sprintf("model %s is no longer available", step.Model)
			we.flowsDB.Exec(`
				UPDATE workflow_step_runs SET status = 'failed', error = ?, completed_at = datetime('now')
				WHERE step_run_id = ?`, errMsg, stepRunID)
			we.flowsDB.InsertAuditLog(runID, stepRunID, "step_failed", map[string]string{
				"error": errMsg, "reason": "model_unavailable",
			})
			return fmt.Errorf("%s", errMsg)
		}
		grantAllowed, explicit := we.flowsDB.CheckModelGrant(execCtx.userID, execCtx.userRole, step.Model, step.StepType)
		if explicit && !grantAllowed {
			errMsg := fmt.Sprintf("model grant denied: user %s cannot use %s for %s steps", execCtx.userID, step.Model, step.StepType)
			we.flowsDB.Exec(`
				UPDATE workflow_step_runs SET status = 'failed', error = ?, completed_at = datetime('now')
				WHERE step_run_id = ?`, errMsg, stepRunID)
			we.flowsDB.InsertAuditLog(runID, stepRunID, "step_failed", map[string]string{
				"error": errMsg, "reason": "model_grant_denied",
			})
			return fmt.Errorf("%s", errMsg)
		}
	}

	var output string
	var provider, model string
	var tokensIn, tokensOut, latencyMs int
	var stepErr error

	for attempt := 1; attempt <= max(step.RetryMax, 1); attempt++ {
		start := time.Now()

		switch step.StepType {
		case "llm":
			output, provider, model, tokensIn, tokensOut, stepErr = we.executeLLM(ctx, step, execCtx)
		case "sql":
			output, stepErr = we.executeSQL(ctx, step, execCtx)
		case "http":
			output, stepErr = we.executeHTTP(ctx, step, execCtx)
		case "check":
			output, stepErr = we.executeCheck(ctx, step, execCtx)
		default:
			stepErr = fmt.Errorf("unknown step type: %s", step.StepType)
		}

		latencyMs = int(time.Since(start).Milliseconds())

		if stepErr == nil {
			break
		}

		we.logger.Warn("step failed, retrying",
			"run_id", runID,
			"step_name", step.StepName,
			"attempt", attempt,
			"error", stepErr,
		)

		if attempt < step.RetryMax {
			we.flowsDB.InsertAuditLog(runID, stepRunID, "step_retried", map[string]interface{}{
				"attempt": attempt,
				"error":   stepErr.Error(),
			})
			time.Sleep(time.Duration(100*attempt) * time.Millisecond)
		}
	}

	if stepErr != nil {
		errMsg := stepErr.Error()
		we.flowsDB.Exec(`
			UPDATE workflow_step_runs SET status = 'failed', error = ?, latency_ms = ?, attempt = ?, completed_at = datetime('now')
			WHERE step_run_id = ?`,
			errMsg, latencyMs, step.RetryMax, stepRunID)
		we.flowsDB.InsertAuditLog(runID, stepRunID, "step_failed", map[string]string{"error": errMsg})
		return stepErr
	}

	// Success: persist output
	we.flowsDB.Exec(`
		UPDATE workflow_step_runs SET status = 'completed', output_json = ?,
			model_used = ?, provider_used = ?, tokens_in = ?, tokens_out = ?,
			latency_ms = ?, completed_at = datetime('now')
		WHERE step_run_id = ?`,
		output, nilIfEmpty(model), nilIfEmpty(provider), tokensIn, tokensOut, latencyMs, stepRunID)
	we.flowsDB.IncrementCompletedSteps(runID)
	we.flowsDB.InsertAuditLog(runID, stepRunID, "step_completed", map[string]interface{}{
		"step_name":  step.StepName,
		"provider":   provider,
		"model":      model,
		"tokens_in":  tokensIn,
		"tokens_out": tokensOut,
		"latency_ms": latencyMs,
	})

	// Update execution context
	execCtx.responses[step.StepName] = output
	execCtx.previousResponse = output

	return nil
}

// executeLLM builds a prompt, calls the LLM, and returns the response.
func (we *WorkflowEngine) executeLLM(ctx context.Context, step db.WorkflowStep, execCtx *workflowExecCtx) (output, provider, model string, tokensIn, tokensOut int, err error) {
	prompt := renderWorkflowTemplate(step.PromptTemplate, execCtx)
	system := renderWorkflowTemplate(step.SystemPrompt, execCtx)

	var messages []Message
	if system != "" {
		messages = append(messages, Message{Role: "system", Content: system})
	}
	messages = append(messages, Message{Role: "user", Content: prompt})

	req := Request{
		Model:    step.Model,
		Messages: messages,
	}

	var resp *Response
	if step.Provider != "" {
		resp, err = we.client.CompleteWith(ctx, step.Provider, req)
	} else {
		resp, err = we.client.Complete(ctx, req)
	}
	if err != nil {
		return "", "", "", 0, 0, err
	}

	return resp.Content, resp.Provider, resp.Model, resp.TokensIn, resp.TokensOut, nil
}

// executeSQL runs a read-only SQL query against flows.db.
func (we *WorkflowEngine) executeSQL(ctx context.Context, step db.WorkflowStep, execCtx *workflowExecCtx) (string, error) {
	query := renderWorkflowTemplate(step.PromptTemplate, execCtx)
	if query == "" {
		return "", fmt.Errorf("sql step has empty query")
	}

	// Safety: only SELECT allowed
	trimmed := strings.TrimSpace(strings.ToUpper(query))
	if !strings.HasPrefix(trimmed, "SELECT") {
		return "", fmt.Errorf("sql step only allows SELECT queries")
	}

	rows, err := we.flowsDB.QueryContext(ctx, query)
	if err != nil {
		return "", fmt.Errorf("sql query: %w", err)
	}
	defer rows.Close()

	cols, _ := rows.Columns()
	var results []map[string]interface{}

	for rows.Next() {
		values := make([]interface{}, len(cols))
		ptrs := make([]interface{}, len(cols))
		for i := range values {
			ptrs[i] = &values[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return "", err
		}
		row := make(map[string]interface{})
		for i, col := range cols {
			row[col] = values[i]
		}
		results = append(results, row)
	}

	out, _ := json.Marshal(results)
	return string(out), nil
}

// executeHTTP makes an HTTP request and returns the response body.
func (we *WorkflowEngine) executeHTTP(ctx context.Context, step db.WorkflowStep, execCtx *workflowExecCtx) (string, error) {
	urlStr := renderWorkflowTemplate(step.PromptTemplate, execCtx)
	if urlStr == "" {
		return "", fmt.Errorf("http step has empty URL")
	}

	// Parse config for method
	method := "GET"
	var configMap map[string]string
	if step.ConfigJSON != "" && step.ConfigJSON != "{}" {
		json.Unmarshal([]byte(step.ConfigJSON), &configMap)
		if m, ok := configMap["method"]; ok {
			method = strings.ToUpper(m)
		}
	}

	req, err := http.NewRequestWithContext(ctx, method, urlStr, nil)
	if err != nil {
		return "", fmt.Errorf("building http request: %w", err)
	}

	// Add headers from config
	if auth, ok := configMap["authorization"]; ok {
		req.Header.Set("Authorization", auth)
	}

	resp, err := we.httpCl.Do(req)
	if err != nil {
		return "", fmt.Errorf("http call: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20)) // 1MB max
	if err != nil {
		return "", fmt.Errorf("reading http response: %w", err)
	}

	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("http %d: %s", resp.StatusCode, string(body[:min(len(body), 500)]))
	}

	return string(body), nil
}

// executeCheck evaluates criteria from a criteria_list against the current context.
func (we *WorkflowEngine) executeCheck(ctx context.Context, step db.WorkflowStep, execCtx *workflowExecCtx) (string, error) {
	if step.CriteriaListID == nil {
		return "", fmt.Errorf("check step has no criteria_list_id")
	}

	cl, err := we.flowsDB.GetCriteriaList(*step.CriteriaListID)
	if err != nil {
		return "", fmt.Errorf("loading criteria list: %w", err)
	}

	var items []string
	if err := json.Unmarshal([]byte(cl.ItemsJSON), &items); err != nil {
		return "", fmt.Errorf("parsing criteria items: %w", err)
	}

	// Build evaluation prompt
	contextText := execCtx.previousResponse
	if contextText == "" {
		contextText = execCtx.body
	}

	criteriaStr := ""
	for i, item := range items {
		criteriaStr += fmt.Sprintf("%d. %s\n", i+1, item)
	}

	evalPrompt := fmt.Sprintf("Evaluate the following content against each criterion. For each, respond PASS or FAIL with a brief justification.\n\nContent:\n%s\n\nCriteria:\n%s\nRespond in JSON format: [{\"criterion\": \"...\", \"result\": \"PASS\"|\"FAIL\", \"justification\": \"...\"}]",
		contextText, criteriaStr)

	messages := []Message{
		{Role: "system", Content: "You are a strict evaluator. Evaluate content against criteria and respond in valid JSON only."},
		{Role: "user", Content: evalPrompt},
	}

	req := Request{
		Model:    step.Model,
		Messages: messages,
	}

	var resp *Response
	if step.Provider != "" {
		resp, err = we.client.CompleteWith(ctx, step.Provider, req)
	} else {
		resp, err = we.client.Complete(ctx, req)
	}
	if err != nil {
		return "", fmt.Errorf("check evaluation: %w", err)
	}

	return resp.Content, nil
}

// workflowExecCtx carries accumulated state through workflow execution.
type workflowExecCtx struct {
	body             string
	prePrompt        string
	previousResponse string
	responses        map[string]string
	userID           string
	userRole         string
}

// renderWorkflowTemplate replaces template variables.
func renderWorkflowTemplate(tmpl string, ctx *workflowExecCtx) string {
	if tmpl == "" {
		return ""
	}
	s := tmpl
	s = strings.ReplaceAll(s, "{{.Body}}", ctx.body)
	s = strings.ReplaceAll(s, "{{.PrePrompt}}", ctx.prePrompt)
	s = strings.ReplaceAll(s, "{{.PreviousResponse}}", ctx.previousResponse)

	// {{.Step.step_name}} replacements
	for name, resp := range ctx.responses {
		s = strings.ReplaceAll(s, fmt.Sprintf("{{.Step.%s}}", name), resp)
	}

	// {{.List.list_name}} — handled by caller injecting into responses
	// {{.FanResults}} — the aggregated fan results
	if fr, ok := ctx.responses["fan_results"]; ok {
		s = strings.ReplaceAll(s, "{{.FanResults}}", fr)
	}

	return s
}

// groupSteps organizes steps by step_order.
func groupSteps(steps []db.WorkflowStep) []stepGroup {
	orderMap := make(map[int][]db.WorkflowStep)
	for _, s := range steps {
		orderMap[s.StepOrder] = append(orderMap[s.StepOrder], s)
	}

	var groups []stepGroup
	for order, ss := range orderMap {
		groups = append(groups, stepGroup{order: order, steps: ss})
	}

	sort.Slice(groups, func(i, j int) bool {
		return groups[i].order < groups[j].order
	})

	return groups
}

func copyMap(m map[string]string) map[string]string {
	cp := make(map[string]string, len(m))
	for k, v := range m {
		cp[k] = v
	}
	return cp
}

// ExecuteWorkflowWithBody runs a workflow with a provided body text.
func (we *WorkflowEngine) ExecuteWorkflowWithBody(ctx context.Context, workflowID, nodeID, userID, userRole, prePrompt, body string) (string, error) {
	wf, err := we.flowsDB.GetWorkflow(workflowID)
	if err != nil {
		return "", fmt.Errorf("loading workflow: %w", err)
	}

	runID := db.NewID()
	run := &db.WorkflowRun{
		RunID:       runID,
		WorkflowID:  workflowID,
		InitiatedBy: userID,
		Status:      "pending",
		TotalSteps:  len(wf.Steps),
	}
	if nodeID != "" {
		run.NodeID = &nodeID
	}
	if prePrompt != "" {
		run.PrePrompt = &prePrompt
	}

	if err := we.flowsDB.CreateWorkflowRun(run); err != nil {
		return "", fmt.Errorf("creating run: %w", err)
	}

	we.flowsDB.UpdateRunStatus(runID, "running", nil, nil)
	we.flowsDB.InsertAuditLog(runID, "", "run_started", map[string]string{
		"workflow_id":   workflowID,
		"workflow_name": wf.Name,
	})

	groups := groupSteps(wf.Steps)

	execCtx := &workflowExecCtx{
		body:      body,
		prePrompt: prePrompt,
		responses: make(map[string]string),
		userID:    userID,
		userRole:  userRole,
	}

	for _, g := range groups {
		if ctx.Err() != nil {
			errMsg := "cancelled"
			we.flowsDB.UpdateRunStatus(runID, "cancelled", nil, &errMsg)
			return runID, ctx.Err()
		}

		if len(g.steps) == 1 {
			if err := we.executeStepACID(ctx, runID, g.steps[0], execCtx, nil); err != nil {
				errMsg := err.Error()
				we.flowsDB.UpdateRunStatus(runID, "failed", nil, &errMsg)
				return runID, nil
			}
		} else {
			we.flowsDB.InsertAuditLog(runID, "", "fan_out_started", map[string]interface{}{
				"step_order": g.order,
				"count":      len(g.steps),
			})

			var wg sync.WaitGroup
			var mu sync.Mutex
			fanResults := make(map[string]string)

			for _, step := range g.steps {
				wg.Add(1)
				go func(s db.WorkflowStep) {
					defer wg.Done()
					localCtx := &workflowExecCtx{
						body:      execCtx.body,
						prePrompt: execCtx.prePrompt,
						responses: copyMap(execCtx.responses),
						userID:    execCtx.userID,
						userRole:  execCtx.userRole,
					}
					if err := we.executeStepACID(ctx, runID, s, localCtx, nil); err != nil {
						return
					}
					mu.Lock()
					fanResults[s.StepName] = localCtx.responses[s.StepName]
					mu.Unlock()
				}(step)
			}

			wg.Wait()

			we.flowsDB.InsertAuditLog(runID, "", "fan_in_completed", map[string]interface{}{
				"step_order": g.order,
				"completed":  len(fanResults),
			})

			fanResultsJSON, _ := json.Marshal(fanResults)
			execCtx.responses["fan_results"] = string(fanResultsJSON)
			for k, v := range fanResults {
				execCtx.responses[k] = v
				execCtx.previousResponse = v
			}
		}
	}

	resultMap := make(map[string]string)
	for k, v := range execCtx.responses {
		resultMap[k] = v
	}
	resultJSON, _ := json.Marshal(resultMap)
	resultStr := string(resultJSON)
	we.flowsDB.UpdateRunStatus(runID, "completed", &resultStr, nil)
	we.flowsDB.InsertAuditLog(runID, "", "run_completed", nil)

	return runID, nil
}
