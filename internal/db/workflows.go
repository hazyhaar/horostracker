// CLAUDE:SUMMARY Workflow DB operations — VACF workflow/step CRUD, run tracking, batch management, criteria lists
package db

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"
)

// Workflow represents a dynamic workflow definition.
type Workflow struct {
	WorkflowID       string     `json:"workflow_id"`
	Name             string     `json:"name"`
	Description      string     `json:"description"`
	WorkflowType     string     `json:"workflow_type"`
	OwnerID          string     `json:"owner_id"`
	OwnerRole        string     `json:"owner_role"`
	Status           string     `json:"status"`
	Version          int        `json:"version"`
	PrePromptTemplate string    `json:"pre_prompt_template,omitempty"`
	CreatedAt        time.Time  `json:"created_at"`
	UpdatedAt        time.Time  `json:"updated_at"`
	ValidatedBy      *string    `json:"validated_by,omitempty"`
	ValidatedAt      *time.Time `json:"validated_at,omitempty"`
	RejectionReason  *string    `json:"rejection_reason,omitempty"`
	Steps            []WorkflowStep `json:"steps,omitempty"`
}

// WorkflowStep represents one VACF row.
type WorkflowStep struct {
	StepID         string `json:"step_id"`
	WorkflowID     string `json:"workflow_id"`
	StepOrder      int    `json:"step_order"`
	StepName       string `json:"step_name"`
	StepType       string `json:"step_type"`
	Provider       string `json:"provider,omitempty"`
	Model          string `json:"model,omitempty"`
	PromptTemplate string `json:"prompt_template,omitempty"`
	SystemPrompt   string `json:"system_prompt,omitempty"`
	ConfigJSON     string `json:"config_json"`
	CriteriaListID *string `json:"criteria_list_id,omitempty"`
	TimeoutMs      int    `json:"timeout_ms"`
	RetryMax       int    `json:"retry_max"`
	FanGroup       *string `json:"fan_group,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
}

// CriteriaList stores reusable evaluation criteria.
type CriteriaList struct {
	ListID      string    `json:"list_id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	ItemsJSON   string    `json:"items_json"`
	OwnerID     string    `json:"owner_id"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// WorkflowRun represents one execution of a workflow.
type WorkflowRun struct {
	RunID          string     `json:"run_id"`
	WorkflowID     string     `json:"workflow_id"`
	NodeID         *string    `json:"node_id,omitempty"`
	InitiatedBy    string     `json:"initiated_by"`
	Status         string     `json:"status"`
	PrePrompt      *string    `json:"pre_prompt,omitempty"`
	BatchID        *string    `json:"batch_id,omitempty"`
	TotalSteps     int        `json:"total_steps"`
	CompletedSteps int        `json:"completed_steps"`
	ResultJSON     *string    `json:"result_json,omitempty"`
	Error          *string    `json:"error,omitempty"`
	StartedAt      *time.Time `json:"started_at,omitempty"`
	CompletedAt    *time.Time `json:"completed_at,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
	WorkflowName   string     `json:"workflow_name,omitempty"`
}

// WorkflowStepRun represents one execution of a single step.
type WorkflowStepRun struct {
	StepRunID    string     `json:"step_run_id"`
	RunID        string     `json:"run_id"`
	StepID       string     `json:"step_id"`
	StepOrder    int        `json:"step_order"`
	Status       string     `json:"status"`
	InputJSON    *string    `json:"input_json,omitempty"`
	OutputJSON   *string    `json:"output_json,omitempty"`
	ModelUsed    *string    `json:"model_used,omitempty"`
	ProviderUsed *string    `json:"provider_used,omitempty"`
	TokensIn     int        `json:"tokens_in"`
	TokensOut    int        `json:"tokens_out"`
	LatencyMs    int        `json:"latency_ms"`
	Error        *string    `json:"error,omitempty"`
	Attempt      int        `json:"attempt"`
	StartedAt    *time.Time `json:"started_at,omitempty"`
	CompletedAt  *time.Time `json:"completed_at,omitempty"`
	StepName     string     `json:"step_name,omitempty"`
	StepType     string     `json:"step_type,omitempty"`
}

// --- Workflow CRUD ---

// CreateWorkflow inserts a new workflow definition.
func (db *FlowsDB) CreateWorkflow(w *Workflow) error {
	_, err := db.Exec(`
		INSERT INTO workflows (workflow_id, name, description, workflow_type, owner_id, owner_role, status, version, pre_prompt_template)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		w.WorkflowID, w.Name, w.Description, w.WorkflowType,
		w.OwnerID, w.OwnerRole, w.Status, w.Version, nilIfEmpty(w.PrePromptTemplate))
	return err
}

// GetWorkflow retrieves a workflow by ID, including its steps.
func (db *FlowsDB) GetWorkflow(workflowID string) (*Workflow, error) {
	w := &Workflow{}
	var validatedBy, rejectionReason sql.NullString
	var validatedAt sql.NullTime
	err := db.QueryRow(`
		SELECT workflow_id, name, description, workflow_type, owner_id, owner_role,
			status, version, COALESCE(pre_prompt_template,''), created_at, updated_at,
			validated_by, validated_at, rejection_reason
		FROM workflows WHERE workflow_id = ?`, workflowID).Scan(
		&w.WorkflowID, &w.Name, &w.Description, &w.WorkflowType,
		&w.OwnerID, &w.OwnerRole, &w.Status, &w.Version,
		&w.PrePromptTemplate, &w.CreatedAt, &w.UpdatedAt,
		&validatedBy, &validatedAt, &rejectionReason)
	if err != nil {
		return nil, err
	}
	if validatedBy.Valid {
		w.ValidatedBy = &validatedBy.String
	}
	if validatedAt.Valid {
		w.ValidatedAt = &validatedAt.Time
	}
	if rejectionReason.Valid {
		w.RejectionReason = &rejectionReason.String
	}

	steps, err := db.GetWorkflowSteps(workflowID)
	if err != nil {
		return nil, err
	}
	w.Steps = steps
	return w, nil
}

// GetWorkflowByName retrieves a workflow by its unique name.
func (db *FlowsDB) GetWorkflowByName(name string) (*Workflow, error) {
	var wfID string
	err := db.QueryRow(`SELECT workflow_id FROM workflows WHERE name = ?`, name).Scan(&wfID)
	if err != nil {
		return nil, err
	}
	return db.GetWorkflow(wfID)
}

// ListWorkflows returns workflows filtered by role and optional status.
func (db *FlowsDB) ListWorkflows(role, status string) ([]Workflow, error) {
	query := `SELECT workflow_id, name, description, workflow_type, owner_id, owner_role,
		status, version, COALESCE(pre_prompt_template,''), created_at, updated_at
		FROM workflows WHERE 1=1`
	var args []interface{}

	if role == "user" {
		query += ` AND status = 'active'`
	} else if role == "operator" || role == "provider" {
		query += ` AND (status = 'active' OR owner_role = ?)`
		args = append(args, role)
	}
	// admin sees everything

	if status != "" {
		query += ` AND status = ?`
		args = append(args, status)
	}

	query += ` ORDER BY updated_at DESC`

	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var workflows []Workflow
	for rows.Next() {
		var w Workflow
		if err := rows.Scan(&w.WorkflowID, &w.Name, &w.Description, &w.WorkflowType,
			&w.OwnerID, &w.OwnerRole, &w.Status, &w.Version,
			&w.PrePromptTemplate, &w.CreatedAt, &w.UpdatedAt); err != nil {
			return nil, err
		}
		workflows = append(workflows, w)
	}
	return workflows, rows.Err()
}

// UpdateWorkflowStatus transitions a workflow to a new status.
func (db *FlowsDB) UpdateWorkflowStatus(workflowID, status string, validatedBy *string, rejectionReason *string) error {
	query := `UPDATE workflows SET status = ?, updated_at = datetime('now')`
	args := []interface{}{status}

	if validatedBy != nil {
		query += `, validated_by = ?, validated_at = datetime('now')`
		args = append(args, *validatedBy)
	}
	if rejectionReason != nil {
		query += `, rejection_reason = ?`
		args = append(args, *rejectionReason)
	}

	query += ` WHERE workflow_id = ?`
	args = append(args, workflowID)

	_, err := db.Exec(query, args...)
	return err
}

// UpdateWorkflow updates mutable fields of a draft workflow.
func (db *FlowsDB) UpdateWorkflow(workflowID, name, description, prePromptTemplate string) error {
	_, err := db.Exec(`
		UPDATE workflows SET name = ?, description = ?, pre_prompt_template = ?, updated_at = datetime('now')
		WHERE workflow_id = ? AND status = 'draft'`,
		name, description, nilIfEmpty(prePromptTemplate), workflowID)
	return err
}

// --- Step CRUD ---

// CreateStep inserts a new workflow step.
func (db *FlowsDB) CreateStep(s *WorkflowStep) error {
	_, err := db.Exec(`
		INSERT INTO workflow_steps (step_id, workflow_id, step_order, step_name, step_type,
			provider, model, prompt_template, system_prompt, config_json,
			criteria_list_id, timeout_ms, retry_max, fan_group)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		s.StepID, s.WorkflowID, s.StepOrder, s.StepName, s.StepType,
		nilIfEmpty(s.Provider), nilIfEmpty(s.Model),
		nilIfEmpty(s.PromptTemplate), nilIfEmpty(s.SystemPrompt),
		s.ConfigJSON, s.CriteriaListID, s.TimeoutMs, s.RetryMax, s.FanGroup)
	return err
}

// UpdateStep updates an existing step.
func (db *FlowsDB) UpdateStep(s *WorkflowStep) error {
	_, err := db.Exec(`
		UPDATE workflow_steps SET step_order = ?, step_name = ?, step_type = ?,
			provider = ?, model = ?, prompt_template = ?, system_prompt = ?,
			config_json = ?, criteria_list_id = ?, timeout_ms = ?, retry_max = ?, fan_group = ?
		WHERE step_id = ?`,
		s.StepOrder, s.StepName, s.StepType,
		nilIfEmpty(s.Provider), nilIfEmpty(s.Model),
		nilIfEmpty(s.PromptTemplate), nilIfEmpty(s.SystemPrompt),
		s.ConfigJSON, s.CriteriaListID, s.TimeoutMs, s.RetryMax, s.FanGroup,
		s.StepID)
	return err
}

// DeleteStep removes a step.
func (db *FlowsDB) DeleteStep(stepID string) error {
	_, err := db.Exec(`DELETE FROM workflow_steps WHERE step_id = ?`, stepID)
	return err
}

// GetWorkflowSteps retrieves all steps for a workflow, ordered.
func (db *FlowsDB) GetWorkflowSteps(workflowID string) ([]WorkflowStep, error) {
	rows, err := db.Query(`
		SELECT step_id, workflow_id, step_order, step_name, step_type,
			COALESCE(provider,''), COALESCE(model,''),
			COALESCE(prompt_template,''), COALESCE(system_prompt,''),
			COALESCE(config_json,'{}'), criteria_list_id, timeout_ms, retry_max, fan_group, created_at
		FROM workflow_steps WHERE workflow_id = ? ORDER BY step_order, step_name`, workflowID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var steps []WorkflowStep
	for rows.Next() {
		var s WorkflowStep
		var criteriaID, fanGroup sql.NullString
		if err := rows.Scan(&s.StepID, &s.WorkflowID, &s.StepOrder, &s.StepName, &s.StepType,
			&s.Provider, &s.Model, &s.PromptTemplate, &s.SystemPrompt,
			&s.ConfigJSON, &criteriaID, &s.TimeoutMs, &s.RetryMax, &fanGroup, &s.CreatedAt); err != nil {
			return nil, err
		}
		if criteriaID.Valid {
			s.CriteriaListID = &criteriaID.String
		}
		if fanGroup.Valid {
			s.FanGroup = &fanGroup.String
		}
		steps = append(steps, s)
	}
	return steps, rows.Err()
}

// ReorderSteps updates step_order for a list of step IDs in the given order.
func (db *FlowsDB) ReorderSteps(workflowID string, stepIDs []string) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	for i, sid := range stepIDs {
		if _, err := tx.Exec(`UPDATE workflow_steps SET step_order = ? WHERE step_id = ? AND workflow_id = ?`,
			i+1, sid, workflowID); err != nil {
			_ = tx.Rollback()
			return err
		}
	}
	return tx.Commit()
}

// --- Criteria Lists CRUD ---

// CreateCriteriaList inserts a new criteria list.
func (db *FlowsDB) CreateCriteriaList(cl *CriteriaList) error {
	_, err := db.Exec(`
		INSERT INTO criteria_lists (list_id, name, description, items_json, owner_id)
		VALUES (?, ?, ?, ?, ?)`,
		cl.ListID, cl.Name, cl.Description, cl.ItemsJSON, cl.OwnerID)
	return err
}

// GetCriteriaList retrieves a criteria list by ID.
func (db *FlowsDB) GetCriteriaList(listID string) (*CriteriaList, error) {
	cl := &CriteriaList{}
	err := db.QueryRow(`
		SELECT list_id, name, description, items_json, owner_id, created_at, updated_at
		FROM criteria_lists WHERE list_id = ?`, listID).Scan(
		&cl.ListID, &cl.Name, &cl.Description, &cl.ItemsJSON, &cl.OwnerID,
		&cl.CreatedAt, &cl.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return cl, nil
}

// GetCriteriaListByName retrieves a criteria list by name.
func (db *FlowsDB) GetCriteriaListByName(name string) (*CriteriaList, error) {
	cl := &CriteriaList{}
	err := db.QueryRow(`
		SELECT list_id, name, description, items_json, owner_id, created_at, updated_at
		FROM criteria_lists WHERE name = ?`, name).Scan(
		&cl.ListID, &cl.Name, &cl.Description, &cl.ItemsJSON, &cl.OwnerID,
		&cl.CreatedAt, &cl.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return cl, nil
}

// ListCriteriaLists returns all criteria lists.
func (db *FlowsDB) ListCriteriaLists() ([]CriteriaList, error) {
	rows, err := db.Query(`
		SELECT list_id, name, description, items_json, owner_id, created_at, updated_at
		FROM criteria_lists ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var lists []CriteriaList
	for rows.Next() {
		var cl CriteriaList
		if err := rows.Scan(&cl.ListID, &cl.Name, &cl.Description, &cl.ItemsJSON,
			&cl.OwnerID, &cl.CreatedAt, &cl.UpdatedAt); err != nil {
			return nil, err
		}
		lists = append(lists, cl)
	}
	return lists, rows.Err()
}

// UpdateCriteriaList updates a criteria list.
func (db *FlowsDB) UpdateCriteriaList(listID, name, description, itemsJSON string) error {
	_, err := db.Exec(`
		UPDATE criteria_lists SET name = ?, description = ?, items_json = ?, updated_at = datetime('now')
		WHERE list_id = ?`,
		name, description, itemsJSON, listID)
	return err
}

// --- Workflow Runs ---

// CreateWorkflowRun inserts a new run.
func (db *FlowsDB) CreateWorkflowRun(r *WorkflowRun) error {
	_, err := db.Exec(`
		INSERT INTO workflow_runs (run_id, workflow_id, node_id, initiated_by, status, pre_prompt, batch_id, total_steps)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		r.RunID, r.WorkflowID, r.NodeID, r.InitiatedBy, r.Status, r.PrePrompt, r.BatchID, r.TotalSteps)
	return err
}

// GetWorkflowRun retrieves a run by ID.
func (db *FlowsDB) GetWorkflowRun(runID string) (*WorkflowRun, error) {
	r := &WorkflowRun{}
	var nodeID, prePrompt, batchID, resultJSON, errStr sql.NullString
	var startedAt, completedAt sql.NullTime
	err := db.QueryRow(`
		SELECT r.run_id, r.workflow_id, r.node_id, r.initiated_by, r.status,
			r.pre_prompt, r.batch_id, r.total_steps, r.completed_steps,
			r.result_json, r.error, r.started_at, r.completed_at, r.created_at,
			w.name
		FROM workflow_runs r JOIN workflows w ON r.workflow_id = w.workflow_id
		WHERE r.run_id = ?`, runID).Scan(
		&r.RunID, &r.WorkflowID, &nodeID, &r.InitiatedBy, &r.Status,
		&prePrompt, &batchID, &r.TotalSteps, &r.CompletedSteps,
		&resultJSON, &errStr, &startedAt, &completedAt, &r.CreatedAt,
		&r.WorkflowName)
	if err != nil {
		return nil, err
	}
	if nodeID.Valid {
		r.NodeID = &nodeID.String
	}
	if prePrompt.Valid {
		r.PrePrompt = &prePrompt.String
	}
	if batchID.Valid {
		r.BatchID = &batchID.String
	}
	if resultJSON.Valid {
		r.ResultJSON = &resultJSON.String
	}
	if errStr.Valid {
		r.Error = &errStr.String
	}
	if startedAt.Valid {
		r.StartedAt = &startedAt.Time
	}
	if completedAt.Valid {
		r.CompletedAt = &completedAt.Time
	}
	return r, nil
}

// UpdateRunStatus updates a run's status and optional fields.
func (db *FlowsDB) UpdateRunStatus(runID, status string, resultJSON, errMsg *string) error {
	query := `UPDATE workflow_runs SET status = ?`
	args := []interface{}{status}

	if status == "running" {
		query += `, started_at = datetime('now')`
	}
	if status == "completed" || status == "failed" || status == "cancelled" {
		query += `, completed_at = datetime('now')`
	}
	if resultJSON != nil {
		query += `, result_json = ?`
		args = append(args, *resultJSON)
	}
	if errMsg != nil {
		query += `, error = ?`
		args = append(args, *errMsg)
	}

	query += ` WHERE run_id = ?`
	args = append(args, runID)

	_, err := db.Exec(query, args...)
	return err
}

// IncrementCompletedSteps atomically increments the completed step counter.
func (db *FlowsDB) IncrementCompletedSteps(runID string) error {
	_, err := db.Exec(`UPDATE workflow_runs SET completed_steps = completed_steps + 1 WHERE run_id = ?`, runID)
	return err
}

// GetStepRuns retrieves all step runs for a workflow run.
func (db *FlowsDB) GetStepRuns(runID string) ([]WorkflowStepRun, error) {
	rows, err := db.Query(`
		SELECT sr.step_run_id, sr.run_id, sr.step_id, sr.step_order, sr.status,
			sr.input_json, sr.output_json, sr.model_used, sr.provider_used,
			COALESCE(sr.tokens_in,0), COALESCE(sr.tokens_out,0), COALESCE(sr.latency_ms,0),
			sr.error, sr.attempt, sr.started_at, sr.completed_at,
			COALESCE(ws.step_name,''), COALESCE(ws.step_type,'')
		FROM workflow_step_runs sr
		LEFT JOIN workflow_steps ws ON sr.step_id = ws.step_id
		WHERE sr.run_id = ? ORDER BY sr.step_order, sr.started_at`, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var runs []WorkflowStepRun
	for rows.Next() {
		var sr WorkflowStepRun
		var inputJSON, outputJSON, modelUsed, providerUsed, errStr sql.NullString
		var startedAt, completedAt sql.NullTime
		if err := rows.Scan(&sr.StepRunID, &sr.RunID, &sr.StepID, &sr.StepOrder, &sr.Status,
			&inputJSON, &outputJSON, &modelUsed, &providerUsed,
			&sr.TokensIn, &sr.TokensOut, &sr.LatencyMs,
			&errStr, &sr.Attempt, &startedAt, &completedAt,
			&sr.StepName, &sr.StepType); err != nil {
			return nil, err
		}
		if inputJSON.Valid {
			sr.InputJSON = &inputJSON.String
		}
		if outputJSON.Valid {
			sr.OutputJSON = &outputJSON.String
		}
		if modelUsed.Valid {
			sr.ModelUsed = &modelUsed.String
		}
		if providerUsed.Valid {
			sr.ProviderUsed = &providerUsed.String
		}
		if errStr.Valid {
			sr.Error = &errStr.String
		}
		if startedAt.Valid {
			sr.StartedAt = &startedAt.Time
		}
		if completedAt.Valid {
			sr.CompletedAt = &completedAt.Time
		}
		runs = append(runs, sr)
	}
	return runs, rows.Err()
}

// ListRunsByBatch returns all runs in a batch.
func (db *FlowsDB) ListRunsByBatch(batchID string) ([]WorkflowRun, error) {
	rows, err := db.Query(`
		SELECT r.run_id, r.workflow_id, r.node_id, r.initiated_by, r.status,
			r.total_steps, r.completed_steps, r.created_at, w.name
		FROM workflow_runs r JOIN workflows w ON r.workflow_id = w.workflow_id
		WHERE r.batch_id = ? ORDER BY r.created_at`, batchID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var runs []WorkflowRun
	for rows.Next() {
		var r WorkflowRun
		var nodeID sql.NullString
		if err := rows.Scan(&r.RunID, &r.WorkflowID, &nodeID, &r.InitiatedBy, &r.Status,
			&r.TotalSteps, &r.CompletedSteps, &r.CreatedAt, &r.WorkflowName); err != nil {
			return nil, err
		}
		if nodeID.Valid {
			r.NodeID = &nodeID.String
		}
		runs = append(runs, r)
	}
	return runs, rows.Err()
}

// --- Audit Log ---

// InsertAuditLog records a workflow event.
func (db *FlowsDB) InsertAuditLog(runID, stepRunID, eventType string, eventData interface{}) error {
	var dataJSON *string
	if eventData != nil {
		b, err := json.Marshal(eventData)
		if err != nil {
			return fmt.Errorf("marshaling audit event data: %w", err)
		}
		s := string(b)
		dataJSON = &s
	}
	_, err := db.Exec(`
		INSERT INTO workflow_audit_log (run_id, step_run_id, event_type, event_data_json)
		VALUES (?, ?, ?, ?)`,
		nilIfEmpty(runID), nilIfEmpty(stepRunID), eventType, dataJSON)
	return err
}

// GetAuditLog retrieves audit events for a run.
func (db *FlowsDB) GetAuditLog(runID string) ([]map[string]interface{}, error) {
	rows, err := db.Query(`
		SELECT log_id, run_id, step_run_id, event_type, event_data_json, created_at
		FROM workflow_audit_log WHERE run_id = ? ORDER BY log_id`, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var logs []map[string]interface{}
	for rows.Next() {
		var logID int
		var rID, srID, evType sql.NullString
		var dataJSON sql.NullString
		var createdAt time.Time
		if err := rows.Scan(&logID, &rID, &srID, &evType, &dataJSON, &createdAt); err != nil {
			return nil, err
		}
		entry := map[string]interface{}{
			"log_id":     logID,
			"run_id":     rID.String,
			"event_type": evType.String,
			"created_at": createdAt,
		}
		if srID.Valid {
			entry["step_run_id"] = srID.String
		}
		if dataJSON.Valid {
			entry["event_data_json"] = dataJSON.String
		}
		logs = append(logs, entry)
	}
	return logs, rows.Err()
}

// CountWorkflows returns the total number of workflows.
func (db *FlowsDB) CountWorkflows() int {
	var count int
	_ = db.QueryRow(`SELECT COUNT(*) FROM workflows`).Scan(&count)
	return count
}

// AllowedStepTypes returns the step types a role can use when creating workflows.
func AllowedStepTypes(role string) map[string]bool {
	switch role {
	case "operator":
		return map[string]bool{"llm": true, "check": true}
	case "provider":
		return map[string]bool{"llm": true, "check": true, "sql": true}
	case "admin", "operator_admin":
		return map[string]bool{"llm": true, "check": true, "sql": true, "http": true}
	default:
		return map[string]bool{}
	}
}

func nilIfEmpty(s string) interface{} {
	if s == "" {
		return nil
	}
	return s
}

// ValidateStepTypesForRole checks that all steps use allowed types.
func ValidateStepTypesForRole(steps []WorkflowStep, role string) error {
	allowed := AllowedStepTypes(role)
	for i := range steps {
		if !allowed[steps[i].StepType] {
			return fmt.Errorf("role %q cannot create steps of type %q", role, steps[i].StepType)
		}
	}
	return nil
}
