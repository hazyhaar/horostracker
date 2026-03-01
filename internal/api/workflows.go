// CLAUDE:SUMMARY Workflow API — VACF workflow CRUD, step management, submission/activation lifecycle, batch runs, model discovery, criteria lists
package api

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/hazyhaar/horostracker/internal/db"
	"github.com/hazyhaar/horostracker/internal/llm"
)

// SetWorkflowEngine sets the workflow engine for dynamic workflow execution.
func (a *API) SetWorkflowEngine(we *llm.WorkflowEngine) {
	a.workflowEngine = we
}

// SetModelDiscovery sets the model discovery service.
func (a *API) SetModelDiscovery(md *llm.ModelDiscovery) {
	a.modelDiscovery = md
}

// RegisterWorkflowRoutes adds VACF workflow management endpoints.
func (a *API) RegisterWorkflowRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/workflows", a.handleListWorkflows)
	mux.HandleFunc("GET /api/workflows/{id}", a.handleGetWorkflow)
	mux.HandleFunc("POST /api/workflows", a.handleCreateWorkflow)
	mux.HandleFunc("PUT /api/workflows/{id}", a.handleUpdateWorkflow)

	mux.HandleFunc("POST /api/workflows/{id}/steps", a.handleCreateStep)
	mux.HandleFunc("PUT /api/workflows/{id}/steps/{stepId}", a.handleUpdateStep)
	mux.HandleFunc("DELETE /api/workflows/{id}/steps/{stepId}", a.handleDeleteStep)

	mux.HandleFunc("POST /api/workflows/{id}/submit", a.handleSubmitWorkflow)
	mux.HandleFunc("POST /api/workflows/{id}/activate", a.handleActivateWorkflow)
	mux.HandleFunc("POST /api/workflows/{id}/archive", a.handleArchiveWorkflow)

	mux.HandleFunc("POST /api/workflows/{id}/run", a.handleRunWorkflow)
	mux.HandleFunc("GET /api/workflows/runs/{runId}", a.handleGetWorkflowRun)
	mux.HandleFunc("GET /api/workflows/runs/{runId}/steps", a.handleGetStepRuns)

	mux.HandleFunc("POST /api/workflows/batch", a.handleBatchRun)
	mux.HandleFunc("GET /api/workflows/batch/{batchId}", a.handleGetBatch)

	mux.HandleFunc("GET /api/models", a.handleListModels)
	mux.HandleFunc("POST /api/models/discover", a.handleDiscoverModels)

	mux.HandleFunc("GET /api/criteria-lists", a.handleListCriteriaLists)
	mux.HandleFunc("POST /api/criteria-lists", a.handleCreateCriteriaList)
	mux.HandleFunc("PUT /api/criteria-lists/{id}", a.handleUpdateCriteriaList)

	mux.HandleFunc("GET /api/model-grants", a.handleListGrants)
	mux.HandleFunc("POST /api/model-grants", a.handleCreateGrant)
	mux.HandleFunc("DELETE /api/model-grants/{id}", a.handleDeleteGrant)

	// v2: Provider model registration & allowed models
	mux.HandleFunc("POST /api/models", a.handleCreateModel)
	mux.HandleFunc("GET /api/my-allowed-models", a.handleMyAllowedModels)
	mux.HandleFunc("POST /api/model-grants/bulk", a.handleBulkGrants)

	// v2: Operator groups
	mux.HandleFunc("GET /api/operator-groups", a.handleListOperatorGroups)
	mux.HandleFunc("POST /api/operator-groups", a.handleCreateOperatorGroup)
	mux.HandleFunc("PUT /api/operator-groups/{id}", a.handleUpdateOperatorGroup)
	mux.HandleFunc("DELETE /api/operator-groups/{id}", a.handleDeleteOperatorGroup)
	mux.HandleFunc("GET /api/operator-groups/{id}/members", a.handleListGroupMembers)
	mux.HandleFunc("POST /api/operator-groups/{id}/members", a.handleAddGroupMember)
	mux.HandleFunc("DELETE /api/operator-groups/{id}/members/{operatorId}", a.handleRemoveGroupMember)
	mux.HandleFunc("GET /api/operator-groups/ungrouped", a.handleListUngroupedOperators)
}

// getUserRole resolves the role for an authenticated user.
func (a *API) getUserRole(userID string) string {
	user, err := a.db.GetUserByID(userID)
	if err != nil {
		return "user"
	}
	return user.Role
}

// isOperator checks if the user is an operator.
func (a *API) isOperator(userID string) bool {
	return a.getUserRole(userID) == "operator"
}

// --- Workflow CRUD ---

func (a *API) handleListWorkflows(w http.ResponseWriter, r *http.Request) {
	claims := a.auth.ExtractClaims(r)
	role := "user"
	if claims != nil {
		role = a.getUserRole(claims.UserID)
	}
	status := r.URL.Query().Get("status")

	workflows, err := a.flowsDB.ListWorkflows(role, status)
	if err != nil {
		jsonError(w, "listing workflows: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if workflows == nil {
		workflows = []db.Workflow{}
	}
	jsonResp(w, http.StatusOK, workflows)
}

func (a *API) handleGetWorkflow(w http.ResponseWriter, r *http.Request) {
	wfID := r.PathValue("id")
	wf, err := a.flowsDB.GetWorkflow(wfID)
	if err != nil {
		jsonError(w, "workflow not found", http.StatusNotFound)
		return
	}
	jsonResp(w, http.StatusOK, wf)
}

func (a *API) handleCreateWorkflow(w http.ResponseWriter, r *http.Request) {
	claims := a.auth.ExtractClaims(r)
	if claims == nil {
		jsonError(w, "authentication required", http.StatusUnauthorized)
		return
	}

	role := a.getUserRole(claims.UserID)
	if role == "user" || role == "anon" {
		jsonError(w, "insufficient permissions to create workflows", http.StatusForbidden)
		return
	}

	var req struct {
		Name              string `json:"name"`
		Description       string `json:"description"`
		WorkflowType      string `json:"workflow_type"`
		PrePromptTemplate string `json:"pre_prompt_template"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if req.Name == "" || req.WorkflowType == "" {
		jsonError(w, "name and workflow_type are required", http.StatusBadRequest)
		return
	}

	wf := &db.Workflow{
		WorkflowID:        db.NewID(),
		Name:              req.Name,
		Description:       req.Description,
		WorkflowType:      req.WorkflowType,
		OwnerID:           claims.UserID,
		OwnerRole:         role,
		Status:            "draft",
		Version:           1,
		PrePromptTemplate: req.PrePromptTemplate,
	}

	if err := a.flowsDB.CreateWorkflow(wf); err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			jsonError(w, "workflow name already exists", http.StatusConflict)
			return
		}
		jsonError(w, "creating workflow: "+err.Error(), http.StatusInternalServerError)
		return
	}

	jsonResp(w, http.StatusCreated, map[string]interface{}{"workflow": wf})
}

func (a *API) handleUpdateWorkflow(w http.ResponseWriter, r *http.Request) {
	claims := a.auth.ExtractClaims(r)
	if claims == nil {
		jsonError(w, "authentication required", http.StatusUnauthorized)
		return
	}

	wfID := r.PathValue("id")
	wf, err := a.flowsDB.GetWorkflow(wfID)
	if err != nil {
		jsonError(w, "workflow not found", http.StatusNotFound)
		return
	}
	if wf.Status != "draft" {
		jsonError(w, "only draft workflows can be modified", http.StatusBadRequest)
		return
	}
	if wf.OwnerID != claims.UserID && !a.isOperator(claims.UserID) {
		jsonError(w, "not the workflow owner", http.StatusForbidden)
		return
	}

	var req struct {
		Name              string `json:"name"`
		Description       string `json:"description"`
		PrePromptTemplate string `json:"pre_prompt_template"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if req.Name == "" {
		req.Name = wf.Name
	}
	if err := a.flowsDB.UpdateWorkflow(wfID, req.Name, req.Description, req.PrePromptTemplate); err != nil {
		jsonError(w, "updating workflow: "+err.Error(), http.StatusInternalServerError)
		return
	}

	updated, _ := a.flowsDB.GetWorkflow(wfID)
	jsonResp(w, http.StatusOK, updated)
}

// --- Step Management ---

func (a *API) handleCreateStep(w http.ResponseWriter, r *http.Request) {
	claims := a.auth.ExtractClaims(r)
	if claims == nil {
		jsonError(w, "authentication required", http.StatusUnauthorized)
		return
	}

	wfID := r.PathValue("id")
	wf, err := a.flowsDB.GetWorkflow(wfID)
	if err != nil {
		jsonError(w, "workflow not found", http.StatusNotFound)
		return
	}
	if wf.Status != "draft" {
		jsonError(w, "only draft workflows can be modified", http.StatusBadRequest)
		return
	}
	role := a.getUserRole(claims.UserID)
	if wf.OwnerID != claims.UserID && role != "operator" {
		jsonError(w, "not the workflow owner", http.StatusForbidden)
		return
	}

	var req struct {
		StepOrder      int     `json:"step_order"`
		StepName       string  `json:"step_name"`
		StepType       string  `json:"step_type"`
		Provider       string  `json:"provider"`
		Model          string  `json:"model"`
		PromptTemplate string  `json:"prompt_template"`
		SystemPrompt   string  `json:"system_prompt"`
		ConfigJSON     string  `json:"config_json"`
		CriteriaListID *string `json:"criteria_list_id"`
		TimeoutMs      int     `json:"timeout_ms"`
		RetryMax       int     `json:"retry_max"`
		FanGroup       *string `json:"fan_group"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if req.StepName == "" || req.StepType == "" {
		jsonError(w, "step_name and step_type are required", http.StatusBadRequest)
		return
	}

	allowed := db.AllowedStepTypes(role)
	if !allowed[req.StepType] {
		jsonError(w, "role "+role+" cannot create "+req.StepType+" steps", http.StatusForbidden)
		return
	}

	// Model grant enforcement: if a model is specified, check availability and grants
	if req.Model != "" {
		if !a.flowsDB.ModelIsAvailable(req.Model) && a.flowsDB.ModelExists(req.Model) {
			jsonError(w, "model "+req.Model+" is not currently available", http.StatusBadRequest)
			return
		}
		grantAllowed, explicit := a.flowsDB.CheckModelGrant(claims.UserID, role, req.Model, req.StepType)
		if explicit && !grantAllowed {
			jsonError(w, "model grant denied for "+req.Model+" on "+req.StepType+" steps", http.StatusForbidden)
			return
		}
	}

	if req.TimeoutMs == 0 {
		req.TimeoutMs = 30000
	}
	if req.RetryMax == 0 {
		req.RetryMax = 2
	}
	if req.ConfigJSON == "" {
		req.ConfigJSON = "{}"
	}

	step := &db.WorkflowStep{
		StepID:         db.NewID(),
		WorkflowID:     wfID,
		StepOrder:      req.StepOrder,
		StepName:       req.StepName,
		StepType:       req.StepType,
		Provider:       req.Provider,
		Model:          req.Model,
		PromptTemplate: req.PromptTemplate,
		SystemPrompt:   req.SystemPrompt,
		ConfigJSON:     req.ConfigJSON,
		CriteriaListID: req.CriteriaListID,
		TimeoutMs:      req.TimeoutMs,
		RetryMax:       req.RetryMax,
		FanGroup:       req.FanGroup,
	}

	if err := a.flowsDB.CreateStep(step); err != nil {
		jsonError(w, "creating step: "+err.Error(), http.StatusInternalServerError)
		return
	}

	jsonResp(w, http.StatusCreated, step)
}

func (a *API) handleUpdateStep(w http.ResponseWriter, r *http.Request) {
	claims := a.auth.ExtractClaims(r)
	if claims == nil {
		jsonError(w, "authentication required", http.StatusUnauthorized)
		return
	}

	wfID := r.PathValue("id")
	stepID := r.PathValue("stepId")

	wf, err := a.flowsDB.GetWorkflow(wfID)
	if err != nil {
		jsonError(w, "workflow not found", http.StatusNotFound)
		return
	}
	if wf.Status != "draft" {
		jsonError(w, "only draft workflows can be modified", http.StatusBadRequest)
		return
	}
	role := a.getUserRole(claims.UserID)
	if wf.OwnerID != claims.UserID && role != "operator" {
		jsonError(w, "not the workflow owner", http.StatusForbidden)
		return
	}

	var req struct {
		StepOrder      int     `json:"step_order"`
		StepName       string  `json:"step_name"`
		StepType       string  `json:"step_type"`
		Provider       string  `json:"provider"`
		Model          string  `json:"model"`
		PromptTemplate string  `json:"prompt_template"`
		SystemPrompt   string  `json:"system_prompt"`
		ConfigJSON     string  `json:"config_json"`
		CriteriaListID *string `json:"criteria_list_id"`
		TimeoutMs      int     `json:"timeout_ms"`
		RetryMax       int     `json:"retry_max"`
		FanGroup       *string `json:"fan_group"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, "invalid request body", http.StatusBadRequest)
		return
	}

	allowed := db.AllowedStepTypes(role)
	if req.StepType != "" && !allowed[req.StepType] {
		jsonError(w, "role "+role+" cannot use "+req.StepType+" steps", http.StatusForbidden)
		return
	}

	// Model grant enforcement on update
	if req.Model != "" {
		if !a.flowsDB.ModelIsAvailable(req.Model) && a.flowsDB.ModelExists(req.Model) {
			jsonError(w, "model "+req.Model+" is not currently available", http.StatusBadRequest)
			return
		}
		st := req.StepType
		if st == "" {
			st = "llm" // default step type for grant check
		}
		grantAllowed, explicit := a.flowsDB.CheckModelGrant(claims.UserID, role, req.Model, st)
		if explicit && !grantAllowed {
			jsonError(w, "model grant denied for "+req.Model+" on "+st+" steps", http.StatusForbidden)
			return
		}
	}

	if req.ConfigJSON == "" {
		req.ConfigJSON = "{}"
	}
	if req.TimeoutMs == 0 {
		req.TimeoutMs = 30000
	}
	if req.RetryMax == 0 {
		req.RetryMax = 2
	}

	step := &db.WorkflowStep{
		StepID:         stepID,
		WorkflowID:     wfID,
		StepOrder:      req.StepOrder,
		StepName:       req.StepName,
		StepType:       req.StepType,
		Provider:       req.Provider,
		Model:          req.Model,
		PromptTemplate: req.PromptTemplate,
		SystemPrompt:   req.SystemPrompt,
		ConfigJSON:     req.ConfigJSON,
		CriteriaListID: req.CriteriaListID,
		TimeoutMs:      req.TimeoutMs,
		RetryMax:       req.RetryMax,
		FanGroup:       req.FanGroup,
	}

	if err := a.flowsDB.UpdateStep(step); err != nil {
		jsonError(w, "updating step: "+err.Error(), http.StatusInternalServerError)
		return
	}

	jsonResp(w, http.StatusOK, step)
}

func (a *API) handleDeleteStep(w http.ResponseWriter, r *http.Request) {
	claims := a.auth.ExtractClaims(r)
	if claims == nil {
		jsonError(w, "authentication required", http.StatusUnauthorized)
		return
	}

	wfID := r.PathValue("id")
	stepID := r.PathValue("stepId")

	wf, err := a.flowsDB.GetWorkflow(wfID)
	if err != nil {
		jsonError(w, "workflow not found", http.StatusNotFound)
		return
	}
	if wf.Status != "draft" {
		jsonError(w, "only draft workflows can be modified", http.StatusBadRequest)
		return
	}
	if wf.OwnerID != claims.UserID && !a.isOperator(claims.UserID) {
		jsonError(w, "not the workflow owner", http.StatusForbidden)
		return
	}

	if err := a.flowsDB.DeleteStep(stepID); err != nil {
		jsonError(w, "deleting step: "+err.Error(), http.StatusInternalServerError)
		return
	}

	jsonResp(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// --- Lifecycle ---

func (a *API) handleSubmitWorkflow(w http.ResponseWriter, r *http.Request) {
	claims := a.auth.ExtractClaims(r)
	if claims == nil {
		jsonError(w, "authentication required", http.StatusUnauthorized)
		return
	}

	wfID := r.PathValue("id")
	wf, err := a.flowsDB.GetWorkflow(wfID)
	if err != nil {
		jsonError(w, "workflow not found", http.StatusNotFound)
		return
	}
	if wf.Status != "draft" {
		jsonError(w, "only draft workflows can be submitted", http.StatusBadRequest)
		return
	}
	if wf.OwnerID != claims.UserID && !a.isOperator(claims.UserID) {
		jsonError(w, "not the workflow owner", http.StatusForbidden)
		return
	}

	if err := a.flowsDB.UpdateWorkflowStatus(wfID, "pending_validation", nil, nil); err != nil {
		jsonError(w, "submitting: "+err.Error(), http.StatusInternalServerError)
		return
	}

	_ = a.flowsDB.InsertAuditLog("", "", "validation_requested", map[string]string{
		"workflow_id":  wfID,
		"submitted_by": claims.UserID,
	})

	jsonResp(w, http.StatusOK, map[string]string{"status": "pending_validation"})
}

func (a *API) handleActivateWorkflow(w http.ResponseWriter, r *http.Request) {
	claims := a.auth.ExtractClaims(r)
	if claims == nil || !a.isOperator(claims.UserID) {
		jsonError(w, "operator access required", http.StatusForbidden)
		return
	}

	wfID := r.PathValue("id")
	wf, err := a.flowsDB.GetWorkflow(wfID)
	if err != nil {
		jsonError(w, "workflow not found", http.StatusNotFound)
		return
	}
	if wf.Status != "validated" && wf.Status != "draft" && wf.Status != "pending_validation" {
		jsonError(w, "workflow must be validated, pending_validation or draft to activate", http.StatusBadRequest)
		return
	}

	validatedBy := claims.UserID
	if err := a.flowsDB.UpdateWorkflowStatus(wfID, "active", &validatedBy, nil); err != nil {
		jsonError(w, "activating: "+err.Error(), http.StatusInternalServerError)
		return
	}

	_ = a.flowsDB.InsertAuditLog("", "", "validation_approved", map[string]string{
		"workflow_id":  wfID,
		"activated_by": claims.UserID,
	})

	jsonResp(w, http.StatusOK, map[string]string{"status": "active"})
}

func (a *API) handleArchiveWorkflow(w http.ResponseWriter, r *http.Request) {
	claims := a.auth.ExtractClaims(r)
	if claims == nil || !a.isOperator(claims.UserID) {
		jsonError(w, "operator access required", http.StatusForbidden)
		return
	}

	wfID := r.PathValue("id")
	if err := a.flowsDB.UpdateWorkflowStatus(wfID, "archived", nil, nil); err != nil {
		jsonError(w, "archiving: "+err.Error(), http.StatusInternalServerError)
		return
	}

	jsonResp(w, http.StatusOK, map[string]string{"status": "archived"})
}

// --- Execution ---

func (a *API) handleRunWorkflow(w http.ResponseWriter, r *http.Request) {
	claims := a.auth.ExtractClaims(r)
	if claims == nil {
		jsonError(w, "authentication required", http.StatusUnauthorized)
		return
	}

	wfID := r.PathValue("id")
	wf, err := a.flowsDB.GetWorkflow(wfID)
	if err != nil {
		jsonError(w, "workflow not found", http.StatusNotFound)
		return
	}
	if wf.Status != "active" {
		jsonError(w, "workflow is not active", http.StatusBadRequest)
		return
	}

	var req struct {
		NodeID    string `json:"node_id"`
		PrePrompt string `json:"pre_prompt"`
		Body      string `json:"body"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if a.workflowEngine == nil {
		jsonError(w, "workflow engine not configured", http.StatusServiceUnavailable)
		return
	}

	userID := claims.UserID
	role := a.getUserRole(userID)
	go func() {
		if req.Body != "" {
			_, _ = a.workflowEngine.ExecuteWorkflowWithBody(r.Context(), wfID, req.NodeID, userID, role, req.PrePrompt, req.Body)
		} else {
			_, _ = a.workflowEngine.ExecuteWorkflow(r.Context(), wfID, req.NodeID, userID, role, req.PrePrompt)
		}
	}()

	jsonResp(w, http.StatusAccepted, map[string]string{
		"status":      "accepted",
		"workflow_id": wfID,
	})
}

func (a *API) handleGetWorkflowRun(w http.ResponseWriter, r *http.Request) {
	runID := r.PathValue("runId")
	run, err := a.flowsDB.GetWorkflowRun(runID)
	if err != nil {
		jsonError(w, "run not found", http.StatusNotFound)
		return
	}
	jsonResp(w, http.StatusOK, run)
}

func (a *API) handleGetStepRuns(w http.ResponseWriter, r *http.Request) {
	runID := r.PathValue("runId")
	steps, err := a.flowsDB.GetStepRuns(runID)
	if err != nil {
		jsonError(w, "step runs error: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if steps == nil {
		steps = []db.WorkflowStepRun{}
	}
	jsonResp(w, http.StatusOK, steps)
}

// --- Batch ---

func (a *API) handleBatchRun(w http.ResponseWriter, r *http.Request) {
	claims := a.auth.ExtractClaims(r)
	if claims == nil {
		jsonError(w, "authentication required", http.StatusUnauthorized)
		return
	}

	var req struct {
		WorkflowIDs []string `json:"workflow_ids"`
		NodeID      string   `json:"node_id"`
		PrePrompt   string   `json:"pre_prompt"`
		Body        string   `json:"body"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if len(req.WorkflowIDs) == 0 {
		jsonError(w, "workflow_ids required", http.StatusBadRequest)
		return
	}

	batchID := db.NewID()
	userID := claims.UserID
	role := a.getUserRole(userID)

	for _, wfID := range req.WorkflowIDs {
		go func(id string) {
			if req.Body != "" {
				_, _ = a.workflowEngine.ExecuteWorkflowWithBody(r.Context(), id, req.NodeID, userID, role, req.PrePrompt, req.Body)
			} else {
				_, _ = a.workflowEngine.ExecuteWorkflow(r.Context(), id, req.NodeID, userID, role, req.PrePrompt)
			}
		}(wfID)
	}

	jsonResp(w, http.StatusAccepted, map[string]interface{}{
		"batch_id":       batchID,
		"workflow_count": len(req.WorkflowIDs),
		"status":         "accepted",
	})
}

func (a *API) handleGetBatch(w http.ResponseWriter, r *http.Request) {
	batchID := r.PathValue("batchId")
	runs, err := a.flowsDB.ListRunsByBatch(batchID)
	if err != nil {
		jsonError(w, "batch error: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if runs == nil {
		runs = []db.WorkflowRun{}
	}
	jsonResp(w, http.StatusOK, map[string]interface{}{
		"batch_id": batchID,
		"runs":     runs,
	})
}

// --- Models ---

func (a *API) handleListModels(w http.ResponseWriter, r *http.Request) {
	provider := r.URL.Query().Get("provider")
	availableOnly := r.URL.Query().Get("available") != "false"

	models, err := a.flowsDB.ListModels(provider, availableOnly)
	if err != nil {
		jsonError(w, "listing models: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if models == nil {
		models = []db.AvailableModel{}
	}
	jsonResp(w, http.StatusOK, models)
}

func (a *API) handleDiscoverModels(w http.ResponseWriter, r *http.Request) {
	claims := a.auth.ExtractClaims(r)
	if claims == nil || !a.isOperator(claims.UserID) {
		jsonError(w, "operator access required", http.StatusForbidden)
		return
	}

	if a.modelDiscovery == nil {
		jsonError(w, "model discovery not configured", http.StatusServiceUnavailable)
		return
	}

	go a.modelDiscovery.DiscoverAll(r.Context())

	jsonResp(w, http.StatusAccepted, map[string]string{"status": "discovery_started"})
}

// --- Criteria Lists ---

func (a *API) handleListCriteriaLists(w http.ResponseWriter, r *http.Request) {
	lists, err := a.flowsDB.ListCriteriaLists()
	if err != nil {
		jsonError(w, "listing criteria: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if lists == nil {
		lists = []db.CriteriaList{}
	}
	jsonResp(w, http.StatusOK, lists)
}

func (a *API) handleCreateCriteriaList(w http.ResponseWriter, r *http.Request) {
	claims := a.auth.ExtractClaims(r)
	if claims == nil {
		jsonError(w, "authentication required", http.StatusUnauthorized)
		return
	}
	role := a.getUserRole(claims.UserID)
	if role == "user" || role == "anon" {
		jsonError(w, "insufficient permissions", http.StatusForbidden)
		return
	}

	var req struct {
		Name        string   `json:"name"`
		Description string   `json:"description"`
		Items       []string `json:"items"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if req.Name == "" || len(req.Items) == 0 {
		jsonError(w, "name and items are required", http.StatusBadRequest)
		return
	}

	itemsJSON, _ := json.Marshal(req.Items)
	cl := &db.CriteriaList{
		ListID:      db.NewID(),
		Name:        req.Name,
		Description: req.Description,
		ItemsJSON:   string(itemsJSON),
		OwnerID:     claims.UserID,
	}

	if err := a.flowsDB.CreateCriteriaList(cl); err != nil {
		jsonError(w, "creating criteria list: "+err.Error(), http.StatusInternalServerError)
		return
	}

	jsonResp(w, http.StatusCreated, map[string]interface{}{"criteria_list": cl})
}

func (a *API) handleUpdateCriteriaList(w http.ResponseWriter, r *http.Request) {
	claims := a.auth.ExtractClaims(r)
	if claims == nil {
		jsonError(w, "authentication required", http.StatusUnauthorized)
		return
	}
	role := a.getUserRole(claims.UserID)
	if role == "user" || role == "anon" {
		jsonError(w, "insufficient permissions", http.StatusForbidden)
		return
	}

	listID := r.PathValue("id")

	var req struct {
		Name        string   `json:"name"`
		Description string   `json:"description"`
		Items       []string `json:"items"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, "invalid request body", http.StatusBadRequest)
		return
	}

	itemsJSON, _ := json.Marshal(req.Items)
	if err := a.flowsDB.UpdateCriteriaList(listID, req.Name, req.Description, string(itemsJSON)); err != nil {
		jsonError(w, "updating criteria list: "+err.Error(), http.StatusInternalServerError)
		return
	}

	jsonResp(w, http.StatusOK, map[string]string{"status": "updated"})
}

// --- Model Grants ---

func (a *API) handleListGrants(w http.ResponseWriter, r *http.Request) {
	claims := a.auth.ExtractClaims(r)
	if claims == nil || !a.isOperator(claims.UserID) {
		jsonError(w, "operator access required", http.StatusForbidden)
		return
	}

	grants, err := a.flowsDB.ListAllGrants()
	if err != nil {
		jsonError(w, "listing grants: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if grants == nil {
		grants = []db.ModelGrant{}
	}
	jsonResp(w, http.StatusOK, grants)
}

func (a *API) handleCreateGrant(w http.ResponseWriter, r *http.Request) {
	claims := a.auth.ExtractClaims(r)
	if claims == nil || !a.isOperator(claims.UserID) {
		jsonError(w, "operator access required", http.StatusForbidden)
		return
	}

	var req struct {
		GranteeType string `json:"grantee_type"`
		GranteeID   string `json:"grantee_id"`
		ModelID     string `json:"model_id"`
		StepType    string `json:"step_type"`
		Effect      string `json:"effect"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if req.GranteeType == "" || req.GranteeID == "" || req.ModelID == "" || req.StepType == "" {
		jsonError(w, "grantee_type, grantee_id, model_id and step_type are required", http.StatusBadRequest)
		return
	}
	if req.Effect == "" {
		req.Effect = "allow"
	}

	grant := &db.ModelGrant{
		GrantID:     db.NewID(),
		GranteeType: req.GranteeType,
		GranteeID:   req.GranteeID,
		ModelID:     req.ModelID,
		StepType:    req.StepType,
		Effect:      req.Effect,
		CreatedBy:   claims.UserID,
	}

	if err := a.flowsDB.CreateGrant(grant); err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			jsonError(w, "grant already exists for this combination", http.StatusConflict)
			return
		}
		jsonError(w, "creating grant: "+err.Error(), http.StatusInternalServerError)
		return
	}

	jsonResp(w, http.StatusCreated, grant)
}

func (a *API) handleDeleteGrant(w http.ResponseWriter, r *http.Request) {
	claims := a.auth.ExtractClaims(r)
	if claims == nil || !a.isOperator(claims.UserID) {
		jsonError(w, "operator access required", http.StatusForbidden)
		return
	}

	grantID := r.PathValue("id")
	if err := a.flowsDB.DeleteGrant(grantID); err != nil {
		jsonError(w, "deleting grant: "+err.Error(), http.StatusInternalServerError)
		return
	}

	jsonResp(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// --- v2: Provider Model Registration ---

func (a *API) handleCreateModel(w http.ResponseWriter, r *http.Request) {
	claims := a.auth.ExtractClaims(r)
	if claims == nil {
		jsonError(w, "authentication required", http.StatusUnauthorized)
		return
	}
	role := a.getUserRole(claims.UserID)
	if role != "provider" {
		jsonError(w, "provider access required", http.StatusForbidden)
		return
	}

	var req struct {
		ModelID          string  `json:"model_id"`
		Provider         string  `json:"provider"`
		ModelName        string  `json:"model_name"`
		DisplayName      *string `json:"display_name"`
		CapabilitiesJSON string  `json:"capabilities_json"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if req.ModelID == "" || req.Provider == "" || req.ModelName == "" {
		jsonError(w, "model_id, provider and model_name are required", http.StatusBadRequest)
		return
	}
	if req.CapabilitiesJSON == "" {
		req.CapabilitiesJSON = "{}"
	}

	ownerID := claims.UserID
	model := &db.AvailableModel{
		ModelID:          req.ModelID,
		Provider:         req.Provider,
		ModelName:        req.ModelName,
		DisplayName:      req.DisplayName,
		CapabilitiesJSON: req.CapabilitiesJSON,
		OwnerID:          &ownerID,
	}

	if err := a.flowsDB.CreateModel(model); err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			jsonError(w, "model_id already exists", http.StatusConflict)
			return
		}
		jsonError(w, "creating model: "+err.Error(), http.StatusInternalServerError)
		return
	}

	jsonResp(w, http.StatusCreated, model)
}

func (a *API) handleMyAllowedModels(w http.ResponseWriter, r *http.Request) {
	claims := a.auth.ExtractClaims(r)
	if claims == nil {
		jsonError(w, "authentication required", http.StatusUnauthorized)
		return
	}

	role := a.getUserRole(claims.UserID)
	models, err := a.flowsDB.ListAllowedModels(claims.UserID, role)
	if err != nil {
		jsonError(w, "listing allowed models: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if models == nil {
		models = []db.AvailableModel{}
	}
	jsonResp(w, http.StatusOK, models)
}

func (a *API) handleBulkGrants(w http.ResponseWriter, r *http.Request) {
	claims := a.auth.ExtractClaims(r)
	if claims == nil {
		jsonError(w, "authentication required", http.StatusUnauthorized)
		return
	}
	role := a.getUserRole(claims.UserID)
	if role != "provider" {
		jsonError(w, "provider access required", http.StatusForbidden)
		return
	}

	var req struct {
		ModelIDs          []string `json:"model_ids"`
		GrantOperatorIDs  []string `json:"grant_operator_ids"`
		RevokeOperatorIDs []string `json:"revoke_operator_ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if len(req.ModelIDs) == 0 {
		jsonError(w, "model_ids is required", http.StatusBadRequest)
		return
	}

	if err := a.flowsDB.BulkSetGrants(req.ModelIDs, req.GrantOperatorIDs, req.RevokeOperatorIDs, claims.UserID); err != nil {
		jsonError(w, "bulk grant operation: "+err.Error(), http.StatusInternalServerError)
		return
	}

	jsonResp(w, http.StatusOK, map[string]interface{}{
		"status":  "ok",
		"granted": len(req.GrantOperatorIDs),
		"revoked": len(req.RevokeOperatorIDs),
		"models":  len(req.ModelIDs),
	})
}

// --- v2: Operator Groups ---

func (a *API) handleListOperatorGroups(w http.ResponseWriter, r *http.Request) {
	claims := a.auth.ExtractClaims(r)
	if claims == nil {
		jsonError(w, "authentication required", http.StatusUnauthorized)
		return
	}
	role := a.getUserRole(claims.UserID)
	if role != "provider" {
		jsonError(w, "provider access required", http.StatusForbidden)
		return
	}

	groups, err := a.flowsDB.ListGroups(claims.UserID)
	if err != nil {
		jsonError(w, "listing groups: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if groups == nil {
		groups = []db.OperatorGroup{}
	}
	jsonResp(w, http.StatusOK, groups)
}

func (a *API) handleCreateOperatorGroup(w http.ResponseWriter, r *http.Request) {
	claims := a.auth.ExtractClaims(r)
	if claims == nil {
		jsonError(w, "authentication required", http.StatusUnauthorized)
		return
	}
	role := a.getUserRole(claims.UserID)
	if role != "provider" {
		jsonError(w, "provider access required", http.StatusForbidden)
		return
	}

	var req struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if req.Name == "" {
		jsonError(w, "name is required", http.StatusBadRequest)
		return
	}

	group := &db.OperatorGroup{
		GroupID:     db.NewID(),
		ProviderID:  claims.UserID,
		Name:        req.Name,
		Description: req.Description,
	}

	if err := a.flowsDB.CreateGroup(group); err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			jsonError(w, "group name already exists for this provider", http.StatusConflict)
			return
		}
		jsonError(w, "creating group: "+err.Error(), http.StatusInternalServerError)
		return
	}

	jsonResp(w, http.StatusCreated, group)
}

func (a *API) handleUpdateOperatorGroup(w http.ResponseWriter, r *http.Request) {
	claims := a.auth.ExtractClaims(r)
	if claims == nil {
		jsonError(w, "authentication required", http.StatusUnauthorized)
		return
	}
	role := a.getUserRole(claims.UserID)
	if role != "provider" {
		jsonError(w, "provider access required", http.StatusForbidden)
		return
	}

	groupID := r.PathValue("id")
	group, err := a.flowsDB.GetGroup(groupID)
	if err != nil {
		jsonError(w, "group not found", http.StatusNotFound)
		return
	}
	if group.ProviderID != claims.UserID && role != "operator" {
		jsonError(w, "not the group owner", http.StatusForbidden)
		return
	}

	var req struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if req.Name == "" {
		req.Name = group.Name
	}

	if err := a.flowsDB.UpdateGroup(groupID, req.Name, req.Description); err != nil {
		jsonError(w, "updating group: "+err.Error(), http.StatusInternalServerError)
		return
	}

	jsonResp(w, http.StatusOK, map[string]string{"status": "updated"})
}

func (a *API) handleDeleteOperatorGroup(w http.ResponseWriter, r *http.Request) {
	claims := a.auth.ExtractClaims(r)
	if claims == nil {
		jsonError(w, "authentication required", http.StatusUnauthorized)
		return
	}
	role := a.getUserRole(claims.UserID)
	if role != "provider" {
		jsonError(w, "provider access required", http.StatusForbidden)
		return
	}

	groupID := r.PathValue("id")
	group, err := a.flowsDB.GetGroup(groupID)
	if err != nil {
		jsonError(w, "group not found", http.StatusNotFound)
		return
	}
	if group.ProviderID != claims.UserID && role != "operator" {
		jsonError(w, "not the group owner", http.StatusForbidden)
		return
	}

	if err := a.flowsDB.DeleteGroup(groupID); err != nil {
		jsonError(w, "deleting group: "+err.Error(), http.StatusInternalServerError)
		return
	}

	jsonResp(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func (a *API) handleListGroupMembers(w http.ResponseWriter, r *http.Request) {
	claims := a.auth.ExtractClaims(r)
	if claims == nil {
		jsonError(w, "authentication required", http.StatusUnauthorized)
		return
	}
	role := a.getUserRole(claims.UserID)
	if role != "provider" {
		jsonError(w, "provider access required", http.StatusForbidden)
		return
	}

	groupID := r.PathValue("id")
	group, err := a.flowsDB.GetGroup(groupID)
	if err != nil {
		jsonError(w, "group not found", http.StatusNotFound)
		return
	}
	if group.ProviderID != claims.UserID && role != "operator" {
		jsonError(w, "not the group owner", http.StatusForbidden)
		return
	}

	members, err := a.flowsDB.ListMembers(groupID)
	if err != nil {
		jsonError(w, "listing members: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if members == nil {
		members = []db.OperatorGroupMember{}
	}
	jsonResp(w, http.StatusOK, members)
}

func (a *API) handleAddGroupMember(w http.ResponseWriter, r *http.Request) {
	claims := a.auth.ExtractClaims(r)
	if claims == nil {
		jsonError(w, "authentication required", http.StatusUnauthorized)
		return
	}
	role := a.getUserRole(claims.UserID)
	if role != "provider" {
		jsonError(w, "provider access required", http.StatusForbidden)
		return
	}

	groupID := r.PathValue("id")
	group, err := a.flowsDB.GetGroup(groupID)
	if err != nil {
		jsonError(w, "group not found", http.StatusNotFound)
		return
	}
	if group.ProviderID != claims.UserID && role != "operator" {
		jsonError(w, "not the group owner", http.StatusForbidden)
		return
	}

	var req struct {
		OperatorID string `json:"operator_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if req.OperatorID == "" {
		jsonError(w, "operator_id is required", http.StatusBadRequest)
		return
	}

	if err := a.flowsDB.AddMember(groupID, req.OperatorID); err != nil {
		if strings.Contains(err.Error(), "UNIQUE") || strings.Contains(err.Error(), "PRIMARY") {
			jsonError(w, "operator already in group", http.StatusConflict)
			return
		}
		jsonError(w, "adding member: "+err.Error(), http.StatusInternalServerError)
		return
	}

	jsonResp(w, http.StatusCreated, map[string]string{"status": "added"})
}

func (a *API) handleRemoveGroupMember(w http.ResponseWriter, r *http.Request) {
	claims := a.auth.ExtractClaims(r)
	if claims == nil {
		jsonError(w, "authentication required", http.StatusUnauthorized)
		return
	}
	role := a.getUserRole(claims.UserID)
	if role != "provider" {
		jsonError(w, "provider access required", http.StatusForbidden)
		return
	}

	groupID := r.PathValue("id")
	group, err := a.flowsDB.GetGroup(groupID)
	if err != nil {
		jsonError(w, "group not found", http.StatusNotFound)
		return
	}
	if group.ProviderID != claims.UserID && role != "operator" {
		jsonError(w, "not the group owner", http.StatusForbidden)
		return
	}

	operatorID := r.PathValue("operatorId")
	if err := a.flowsDB.RemoveMember(groupID, operatorID); err != nil {
		jsonError(w, "removing member: "+err.Error(), http.StatusInternalServerError)
		return
	}

	jsonResp(w, http.StatusOK, map[string]string{"status": "removed"})
}

func (a *API) handleListUngroupedOperators(w http.ResponseWriter, r *http.Request) {
	claims := a.auth.ExtractClaims(r)
	if claims == nil {
		jsonError(w, "authentication required", http.StatusUnauthorized)
		return
	}
	role := a.getUserRole(claims.UserID)
	if role != "provider" {
		jsonError(w, "provider access required", http.StatusForbidden)
		return
	}

	ungrouped, err := a.flowsDB.ListUngroupedOperators(a.db.DB, claims.UserID)
	if err != nil {
		jsonError(w, "listing ungrouped operators: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if ungrouped == nil {
		ungrouped = []string{}
	}
	jsonResp(w, http.StatusOK, ungrouped)
}
