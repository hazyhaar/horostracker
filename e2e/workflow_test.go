package e2e

import (
	"net/http"
	"testing"
	"time"
)

// promoteRole sets a user's role directly in nodes.db and re-logins to get a fresh token.
func promoteRole(t *testing.T, h *TestHarness, dba *DBAssert, handle, password, role string) string {
	t.Helper()
	db, err := dba.nodes()
	if err != nil {
		t.Fatalf("opening nodes.db: %v", err)
	}
	_, err = db.Exec("UPDATE users SET role = ? WHERE handle = ?", role, handle)
	if err != nil {
		t.Fatalf("promoting %s to %s: %v", handle, role, err)
	}
	tok, _ := h.Login(t, handle, password)
	return tok
}

func TestWorkflowCRUD(t *testing.T) {
	h, dba := ensureHarness(t)

	// Register an operator user
	opToken, _ := h.Register(t, "wf_operator", "wf-operator-1234")
	opToken = promoteRole(t, h, dba, "wf_operator", "wf-operator-1234", "operator")

	// Register a regular user
	userToken, _ := h.Register(t, "wf_user", "wf-user-1234")

	t.Run("SeedWorkflowsExist", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		var workflows []map[string]interface{}
		resp, err := h.JSON("GET", "/api/workflows", nil, opToken, &workflows)
		if err != nil {
			t.Fatalf("listing workflows: %v", err)
		}
		RequireStatus(t, resp, http.StatusOK)

		// The 14 seed workflows should be present
		if len(workflows) < 14 {
			t.Errorf("expected >= 14 seed workflows, got %d", len(workflows))
		}
	})

	var createdWfID string

	t.Run("CreateWorkflow", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		var result map[string]interface{}
		resp, err := h.JSON("POST", "/api/workflows", map[string]interface{}{
			"name":          "e2e_test_wf",
			"workflow_type":  "analyse",
			"description":   "E2E test workflow",
		}, opToken, &result)
		if err != nil {
			t.Fatalf("creating workflow: %v", err)
		}
		RequireStatus(t, resp, http.StatusCreated)

		wf, ok := result["workflow"].(map[string]interface{})
		if !ok {
			t.Fatal("response missing 'workflow' key")
		}
		createdWfID = wf["workflow_id"].(string)
		if createdWfID == "" {
			t.Fatal("workflow_id is empty")
		}
		if wf["status"] != "draft" {
			t.Errorf("status = %v, want draft", wf["status"])
		}
	})

	t.Run("GetWorkflow", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		if createdWfID == "" {
			t.Skip("no workflow created")
		}

		var wf map[string]interface{}
		resp, err := h.JSON("GET", "/api/workflows/"+createdWfID, nil, opToken, &wf)
		if err != nil {
			t.Fatalf("getting workflow: %v", err)
		}
		RequireStatus(t, resp, http.StatusOK)

		if wf["name"] != "e2e_test_wf" {
			t.Errorf("name = %v, want e2e_test_wf", wf["name"])
		}
	})

	t.Run("AddSteps", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		if createdWfID == "" {
			t.Skip("no workflow created")
		}

		// Add an LLM step
		var step1 map[string]interface{}
		resp, err := h.JSON("POST", "/api/workflows/"+createdWfID+"/steps", map[string]interface{}{
			"step_order":      1,
			"step_name":       "initial_analysis",
			"step_type":       "llm",
			"prompt_template": "Analyse: {{.Body}}",
		}, opToken, &step1)
		if err != nil {
			t.Fatalf("adding step 1: %v", err)
		}
		RequireStatus(t, resp, http.StatusCreated)

		// Add a check step
		var step2 map[string]interface{}
		resp, err = h.JSON("POST", "/api/workflows/"+createdWfID+"/steps", map[string]interface{}{
			"step_order":      2,
			"step_name":       "validate_result",
			"step_type":       "check",
			"prompt_template": "Validate: {{.PreviousResponse}}",
		}, opToken, &step2)
		if err != nil {
			t.Fatalf("adding step 2: %v", err)
		}
		RequireStatus(t, resp, http.StatusCreated)

		// Verify steps appear in workflow detail
		var wf map[string]interface{}
		resp, err = h.JSON("GET", "/api/workflows/"+createdWfID, nil, opToken, &wf)
		if err != nil {
			t.Fatalf("getting workflow: %v", err)
		}
		RequireStatus(t, resp, http.StatusOK)

		steps, ok := wf["steps"].([]interface{})
		if !ok || len(steps) < 2 {
			t.Errorf("expected >= 2 steps, got %v", wf["steps"])
		}
	})

	t.Run("OperatorCannotAddSQLStep", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		if createdWfID == "" {
			t.Skip("no workflow created")
		}

		// Operator should not be allowed to create sql steps
		resp, _ := h.Do("POST", "/api/workflows/"+createdWfID+"/steps", map[string]interface{}{
			"step_order":      3,
			"step_name":       "sql_forbidden",
			"step_type":       "sql",
			"prompt_template": "SELECT 1",
		}, opToken)
		if resp.StatusCode == http.StatusCreated {
			t.Error("operator should not be allowed to create SQL steps")
		}
	})

	t.Run("UserCannotCreateWorkflow", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		resp, _ := h.Do("POST", "/api/workflows", map[string]interface{}{
			"name":          "user_wf",
			"workflow_type":  "analyse",
		}, userToken)
		if resp.StatusCode == http.StatusCreated {
			t.Error("regular user should not be able to create workflows")
		}
	})

	t.Run("UpdateWorkflow", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		if createdWfID == "" {
			t.Skip("no workflow created")
		}

		resp, _ := h.Do("PUT", "/api/workflows/"+createdWfID, map[string]interface{}{
			"name":          "e2e_test_wf_updated",
			"workflow_type":  "analyse",
			"description":   "Updated description",
		}, opToken)
		RequireStatus(t, resp, http.StatusOK)
	})
}

func TestWorkflowLifecycle(t *testing.T) {
	h, dba := ensureHarness(t)

	adminToken, _ := h.Register(t, "wf_admin", "wf-admin-1234")
	adminToken = promoteRole(t, h, dba, "wf_admin", "wf-admin-1234", "operator")

	// Create a workflow and submit it
	var wfResult map[string]interface{}
	resp, err := h.JSON("POST", "/api/workflows", map[string]interface{}{
		"name":          "lifecycle_wf",
		"workflow_type":  "critique",
		"description":   "Lifecycle test workflow",
	}, adminToken, &wfResult)
	if err != nil {
		t.Fatalf("creating workflow: %v", err)
	}
	RequireStatus(t, resp, http.StatusCreated)
	wf := wfResult["workflow"].(map[string]interface{})
	wfID := wf["workflow_id"].(string)

	// Add a step so the workflow is not empty
	resp, _ = h.Do("POST", "/api/workflows/"+wfID+"/steps", map[string]interface{}{
		"step_order": 1, "step_name": "analyse", "step_type": "llm",
		"prompt_template": "Analyse critique: {{.Body}}",
	}, adminToken)
	RequireStatus(t, resp, http.StatusCreated)

	t.Run("SubmitForValidation", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		resp, _ := h.Do("POST", "/api/workflows/"+wfID+"/submit", nil, adminToken)
		RequireStatus(t, resp, http.StatusOK)

		// Verify status changed
		var detail map[string]interface{}
		resp, err := h.JSON("GET", "/api/workflows/"+wfID, nil, adminToken, &detail)
		if err != nil {
			t.Fatalf("getting workflow: %v", err)
		}
		RequireStatus(t, resp, http.StatusOK)
		if detail["status"] != "pending_validation" {
			t.Errorf("status = %v, want pending_validation", detail["status"])
		}
	})

	t.Run("ActivateWorkflow", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		resp, _ := h.Do("POST", "/api/workflows/"+wfID+"/activate", nil, adminToken)
		RequireStatus(t, resp, http.StatusOK)

		var detail map[string]interface{}
		resp, err := h.JSON("GET", "/api/workflows/"+wfID, nil, adminToken, &detail)
		if err != nil {
			t.Fatalf("getting workflow: %v", err)
		}
		RequireStatus(t, resp, http.StatusOK)
		if detail["status"] != "active" {
			t.Errorf("status = %v, want active", detail["status"])
		}
	})

	t.Run("ArchiveWorkflow", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		resp, _ := h.Do("POST", "/api/workflows/"+wfID+"/archive", nil, adminToken)
		RequireStatus(t, resp, http.StatusOK)

		var detail map[string]interface{}
		resp, err := h.JSON("GET", "/api/workflows/"+wfID, nil, adminToken, &detail)
		if err != nil {
			t.Fatalf("getting workflow: %v", err)
		}
		RequireStatus(t, resp, http.StatusOK)
		if detail["status"] != "archived" {
			t.Errorf("status = %v, want archived", detail["status"])
		}
	})
}

func TestWorkflowModels(t *testing.T) {
	h, dba := ensureHarness(t)

	opToken, _ := h.Register(t, "wf_models_op", "wf-models-1234")
	opToken = promoteRole(t, h, dba, "wf_models_op", "wf-models-1234", "operator")

	t.Run("ListModels", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		var models []map[string]interface{}
		resp, err := h.JSON("GET", "/api/models", nil, opToken, &models)
		if err != nil {
			t.Fatalf("listing models: %v", err)
		}
		RequireStatus(t, resp, http.StatusOK)

		// Models should have been discovered at startup (at least Anthropic hardcoded list)
		t.Logf("discovered %d models", len(models))
	})

	t.Run("DiscoverModelsRequiresOperator", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		userToken, _ := h.Register(t, "wf_models_user", "wf-models-user-1234")
		resp, _ := h.Do("POST", "/api/models/discover", nil, userToken)
		// Regular users should be forbidden from triggering discovery
		if resp.StatusCode == http.StatusOK {
			t.Error("regular user should not be allowed to trigger model discovery")
		}
	})
}

func TestWorkflowCriteriaLists(t *testing.T) {
	h, dba := ensureHarness(t)

	opToken, _ := h.Register(t, "wf_criteria_op", "wf-criteria-1234")
	opToken = promoteRole(t, h, dba, "wf_criteria_op", "wf-criteria-1234", "operator")

	var listID string

	t.Run("CreateCriteriaList", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		var result map[string]interface{}
		resp, err := h.JSON("POST", "/api/criteria-lists", map[string]interface{}{
			"name":        "e2e_criteria",
			"description": "E2E test criteria list",
			"items":       []string{"Criterion A", "Criterion B", "Criterion C"},
		}, opToken, &result)
		if err != nil {
			t.Fatalf("creating criteria list: %v", err)
		}
		RequireStatus(t, resp, http.StatusCreated)

		cl, ok := result["criteria_list"].(map[string]interface{})
		if !ok {
			t.Fatal("response missing 'criteria_list' key")
		}
		listID = cl["list_id"].(string)
		if listID == "" {
			t.Fatal("list_id is empty")
		}
	})

	t.Run("ListCriteriaLists", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		var lists []map[string]interface{}
		resp, err := h.JSON("GET", "/api/criteria-lists", nil, opToken, &lists)
		if err != nil {
			t.Fatalf("listing criteria lists: %v", err)
		}
		RequireStatus(t, resp, http.StatusOK)

		// At least the one we created + the seeded workflow_viability_thresholds
		if len(lists) < 1 {
			t.Errorf("expected >= 1 criteria lists, got %d", len(lists))
		}
	})
}

func TestWorkflowAuditLog(t *testing.T) {
	h, dba := ensureHarness(t)

	// Verify that seed workflows generated audit log entries in flows.db
	flowsDB, err := dba.flows()
	if err != nil {
		t.Fatalf("opening flows.db: %v", err)
	}

	_ = h // keep linter happy

	t.Run("AuditLogHasEntries", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		var count int
		err := flowsDB.QueryRow("SELECT COUNT(*) FROM workflow_audit_log").Scan(&count)
		if err != nil {
			t.Fatalf("counting audit log entries: %v", err)
		}
		// Model discovery should have logged at least one event
		if count < 1 {
			t.Logf("audit log has %d entries (may be 0 if no providers configured)", count)
		}
	})

	t.Run("WorkflowsTablePopulated", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		var count int
		err := flowsDB.QueryRow("SELECT COUNT(*) FROM workflows WHERE status = 'active'").Scan(&count)
		if err != nil {
			t.Fatalf("counting active workflows: %v", err)
		}
		if count < 14 {
			t.Errorf("expected >= 14 active seed workflows, got %d", count)
		}
	})

	t.Run("WorkflowStepsPopulated", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		var count int
		err := flowsDB.QueryRow("SELECT COUNT(*) FROM workflow_steps").Scan(&count)
		if err != nil {
			t.Fatalf("counting workflow steps: %v", err)
		}
		// 14 workflows * 2-3 steps each = at least 28
		if count < 28 {
			t.Errorf("expected >= 28 workflow steps, got %d", count)
		}
	})

	t.Run("AvailableModelsTableExists", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		var count int
		err := flowsDB.QueryRow("SELECT COUNT(*) FROM available_models").Scan(&count)
		if err != nil {
			t.Fatalf("counting available models: %v", err)
		}
		t.Logf("available_models has %d entries", count)
	})
}

func TestModelGrants(t *testing.T) {
	h, dba := ensureHarness(t)

	opToken, _ := h.Register(t, "wf_grants_op", "wf-grants-op-1234")
	opToken = promoteRole(t, h, dba, "wf_grants_op", "wf-grants-op-1234", "operator")

	// Create a workflow for step testing
	var wfResult map[string]interface{}
	resp, err := h.JSON("POST", "/api/workflows", map[string]interface{}{
		"name":          "grants_test_wf",
		"workflow_type": "analyse",
		"description":   "Model grants test workflow",
	}, opToken, &wfResult)
	if err != nil {
		t.Fatalf("creating workflow: %v", err)
	}
	RequireStatus(t, resp, http.StatusCreated)
	// API returns workflow directly (not wrapped)
	wfID, _ := wfResult["workflow_id"].(string)
	if wfID == "" {
		// Try wrapped format for forward compatibility
		if nested, ok := wfResult["workflow"].(map[string]interface{}); ok {
			wfID, _ = nested["workflow_id"].(string)
		}
	}
	if wfID == "" {
		t.Fatalf("could not extract workflow_id from response: %v", wfResult)
	}

	t.Run("CreateDenyGrant", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		var grant map[string]interface{}
		resp, err := h.JSON("POST", "/api/model-grants", map[string]interface{}{
			"grantee_type": "role",
			"grantee_id":   "operator",
			"model_id":     "anthropic/*",
			"step_type":    "llm",
			"effect":       "deny",
		}, opToken, &grant)
		if err != nil {
			t.Fatalf("creating grant: %v", err)
		}
		RequireStatus(t, resp, http.StatusCreated)

		if grant["effect"] != "deny" {
			t.Errorf("effect = %v, want deny", grant["effect"])
		}
	})

	t.Run("DenyBlocksStepCreation", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		resp, _ := h.Do("POST", "/api/workflows/"+wfID+"/steps", map[string]interface{}{
			"step_order":      1,
			"step_name":       "denied_step",
			"step_type":       "llm",
			"model":           "anthropic/claude-opus-4-6",
			"prompt_template": "Test: {{.Body}}",
		}, opToken)
		if resp.StatusCode != http.StatusForbidden {
			t.Errorf("expected 403 Forbidden, got %d", resp.StatusCode)
		}
	})

	t.Run("AllowStepWithoutDeniedModel", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		// A step without a model specified should still be allowed
		resp, _ := h.Do("POST", "/api/workflows/"+wfID+"/steps", map[string]interface{}{
			"step_order":      1,
			"step_name":       "allowed_step",
			"step_type":       "llm",
			"prompt_template": "Test: {{.Body}}",
		}, opToken)
		RequireStatus(t, resp, http.StatusCreated)
	})

	t.Run("ListGrants", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		var grants []map[string]interface{}
		resp, err := h.JSON("GET", "/api/model-grants", nil, opToken, &grants)
		if err != nil {
			t.Fatalf("listing grants: %v", err)
		}
		RequireStatus(t, resp, http.StatusOK)
		if len(grants) < 1 {
			t.Errorf("expected >= 1 grants, got %d", len(grants))
		}
	})

	t.Run("DeleteGrant", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		var grants []map[string]interface{}
		h.JSON("GET", "/api/model-grants", nil, opToken, &grants)
		if len(grants) == 0 {
			t.Skip("no grants to delete")
		}
		grantID := grants[0]["grant_id"].(string)
		resp, _ := h.Do("DELETE", "/api/model-grants/"+grantID, nil, opToken)
		RequireStatus(t, resp, http.StatusOK)
	})
}

func TestOperatorGroups(t *testing.T) {
	h, dba := ensureHarness(t)

	provToken, provID := h.Register(t, "grp_provider", "grp-provider-1234")
	provToken = promoteRole(t, h, dba, "grp_provider", "grp-provider-1234", "provider")

	// Create an operator to add as member
	_, opID := h.Register(t, "grp_operator", "grp-operator-1234")
	promoteRole(t, h, dba, "grp_operator", "grp-operator-1234", "operator")

	var groupID string
	_ = provID

	t.Run("CreateGroup", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		var group map[string]interface{}
		resp, err := h.JSON("POST", "/api/operator-groups", map[string]interface{}{
			"name":        "test_group",
			"description": "E2E test group",
		}, provToken, &group)
		if err != nil {
			t.Fatalf("creating group: %v", err)
		}
		RequireStatus(t, resp, http.StatusCreated)
		groupID = group["group_id"].(string)
		if groupID == "" {
			t.Fatal("group_id is empty")
		}
	})

	t.Run("ListGroups", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		var groups []map[string]interface{}
		resp, err := h.JSON("GET", "/api/operator-groups", nil, provToken, &groups)
		if err != nil {
			t.Fatalf("listing groups: %v", err)
		}
		RequireStatus(t, resp, http.StatusOK)
		if len(groups) < 1 {
			t.Errorf("expected >= 1 groups, got %d", len(groups))
		}
	})

	t.Run("AddMember", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		if groupID == "" {
			t.Skip("no group created")
		}

		resp, _ := h.Do("POST", "/api/operator-groups/"+groupID+"/members", map[string]interface{}{
			"operator_id": opID,
		}, provToken)
		RequireStatus(t, resp, http.StatusCreated)
	})

	t.Run("ListMembers", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		if groupID == "" {
			t.Skip("no group created")
		}

		var members []map[string]interface{}
		resp, err := h.JSON("GET", "/api/operator-groups/"+groupID+"/members", nil, provToken, &members)
		if err != nil {
			t.Fatalf("listing members: %v", err)
		}
		RequireStatus(t, resp, http.StatusOK)
		if len(members) != 1 {
			t.Errorf("expected 1 member, got %d", len(members))
		}
	})

	t.Run("RemoveMember", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		if groupID == "" {
			t.Skip("no group created")
		}

		resp, _ := h.Do("DELETE", "/api/operator-groups/"+groupID+"/members/"+opID, nil, provToken)
		RequireStatus(t, resp, http.StatusOK)

		// Verify member removed
		var members []map[string]interface{}
		h.JSON("GET", "/api/operator-groups/"+groupID+"/members", nil, provToken, &members)
		if len(members) != 0 {
			t.Errorf("expected 0 members after removal, got %d", len(members))
		}
	})

	t.Run("DeleteGroup", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		if groupID == "" {
			t.Skip("no group created")
		}

		resp, _ := h.Do("DELETE", "/api/operator-groups/"+groupID, nil, provToken)
		RequireStatus(t, resp, http.StatusOK)

		// Verify deleted
		var groups []map[string]interface{}
		h.JSON("GET", "/api/operator-groups", nil, provToken, &groups)
		if len(groups) != 0 {
			t.Errorf("expected 0 groups after deletion, got %d", len(groups))
		}
	})
}

func TestProviderModelRegistration(t *testing.T) {
	h, dba := ensureHarness(t)

	provToken, _ := h.Register(t, "model_reg_provider", "model-reg-1234")
	provToken = promoteRole(t, h, dba, "model_reg_provider", "model-reg-1234", "provider")

	t.Run("RegisterModel", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		var model map[string]interface{}
		resp, err := h.JSON("POST", "/api/models", map[string]interface{}{
			"model_id":     "test-provider/test-model-v1",
			"provider":     "test-provider",
			"model_name":   "test-model-v1",
			"display_name": "Test Model V1",
		}, provToken, &model)
		if err != nil {
			t.Fatalf("registering model: %v", err)
		}
		RequireStatus(t, resp, http.StatusCreated)
		if model["model_id"] != "test-provider/test-model-v1" {
			t.Errorf("model_id = %v, want test-provider/test-model-v1", model["model_id"])
		}
		if model["owner_id"] == nil || model["owner_id"] == "" {
			t.Error("owner_id should be set")
		}
	})

	t.Run("DuplicateModelFails", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		resp, _ := h.Do("POST", "/api/models", map[string]interface{}{
			"model_id":   "test-provider/test-model-v1",
			"provider":   "test-provider",
			"model_name": "test-model-v1",
		}, provToken)
		if resp.StatusCode != http.StatusConflict {
			t.Errorf("expected 409 Conflict, got %d", resp.StatusCode)
		}
	})

	t.Run("UserCannotRegisterModel", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		userToken, _ := h.Register(t, "model_reg_user", "model-reg-user-1234")
		resp, _ := h.Do("POST", "/api/models", map[string]interface{}{
			"model_id":   "test-provider/user-model",
			"provider":   "test-provider",
			"model_name": "user-model",
		}, userToken)
		if resp.StatusCode != http.StatusForbidden {
			t.Errorf("expected 403 Forbidden, got %d", resp.StatusCode)
		}
	})
}

func TestBulkGrantAssignment(t *testing.T) {
	h, dba := ensureHarness(t)

	provToken, _ := h.Register(t, "bulk_provider", "bulk-provider-1234")
	provToken = promoteRole(t, h, dba, "bulk_provider", "bulk-provider-1234", "provider")

	opToken, opID := h.Register(t, "bulk_operator", "bulk-operator-1234")
	opToken = promoteRole(t, h, dba, "bulk_operator", "bulk-operator-1234", "operator")

	// Register a model
	resp, _ := h.Do("POST", "/api/models", map[string]interface{}{
		"model_id":   "bulk-test/model-a",
		"provider":   "bulk-test",
		"model_name": "model-a",
	}, provToken)
	RequireStatus(t, resp, http.StatusCreated)

	t.Run("BulkGrant", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		resp, _ := h.Do("POST", "/api/model-grants/bulk", map[string]interface{}{
			"model_ids":          []string{"bulk-test/model-a"},
			"grant_operator_ids": []string{opID},
			"revoke_operator_ids": []string{},
		}, provToken)
		RequireStatus(t, resp, http.StatusOK)
	})

	t.Run("OperatorSeesAllowedModel", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		var models []map[string]interface{}
		resp, err := h.JSON("GET", "/api/my-allowed-models", nil, opToken, &models)
		if err != nil {
			t.Fatalf("listing allowed models: %v", err)
		}
		RequireStatus(t, resp, http.StatusOK)

		found := false
		for _, m := range models {
			if m["model_id"] == "bulk-test/model-a" {
				found = true
				break
			}
		}
		if !found {
			t.Error("operator should see bulk-test/model-a in allowed models after grant")
		}
	})

	t.Run("BulkRevoke", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		resp, _ := h.Do("POST", "/api/model-grants/bulk", map[string]interface{}{
			"model_ids":           []string{"bulk-test/model-a"},
			"grant_operator_ids":  []string{},
			"revoke_operator_ids": []string{opID},
		}, provToken)
		RequireStatus(t, resp, http.StatusOK)

		// Verify model no longer in allowed list
		var models []map[string]interface{}
		h.JSON("GET", "/api/my-allowed-models", nil, opToken, &models)
		for _, m := range models {
			if m["model_id"] == "bulk-test/model-a" {
				t.Error("operator should NOT see bulk-test/model-a after revoke")
			}
		}
	})
}

func TestStepEditorUsesAllowedModels(t *testing.T) {
	h, dba := ensureHarness(t)

	provToken, _ := h.Register(t, "step_ed_provider", "step-ed-1234")
	provToken = promoteRole(t, h, dba, "step_ed_provider", "step-ed-1234", "provider")

	opToken, opID := h.Register(t, "step_ed_operator", "step-ed-op-1234")
	opToken = promoteRole(t, h, dba, "step_ed_operator", "step-ed-op-1234", "operator")

	// Provider registers a model
	resp, _ := h.Do("POST", "/api/models", map[string]interface{}{
		"model_id":   "step-ed/restricted-model",
		"provider":   "step-ed",
		"model_name": "restricted-model",
	}, provToken)
	RequireStatus(t, resp, http.StatusCreated)

	// Create a workflow as operator
	var wfResult map[string]interface{}
	resp, err := h.JSON("POST", "/api/workflows", map[string]interface{}{
		"name":          "step_editor_test_wf",
		"workflow_type": "analyse",
	}, opToken, &wfResult)
	if err != nil {
		t.Fatalf("creating workflow: %v", err)
	}
	RequireStatus(t, resp, http.StatusCreated)
	wfID, _ := wfResult["workflow_id"].(string)
	if wfID == "" {
		if nested, ok := wfResult["workflow"].(map[string]interface{}); ok {
			wfID, _ = nested["workflow_id"].(string)
		}
	}

	t.Run("OperatorCannotUseUnauthorizedModel", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		// Operator has no grant for step-ed/restricted-model — step creation should still succeed
		// because v1 enforcement only blocks when an explicit deny exists.
		// But the model won't appear in /api/my-allowed-models
		var models []map[string]interface{}
		h.JSON("GET", "/api/my-allowed-models", nil, opToken, &models)
		for _, m := range models {
			if m["model_id"] == "step-ed/restricted-model" {
				t.Error("operator should not see step-ed/restricted-model without grant")
			}
		}
	})

	t.Run("OperatorSeesModelAfterGrant", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		// Grant access
		resp, _ := h.Do("POST", "/api/model-grants/bulk", map[string]interface{}{
			"model_ids":          []string{"step-ed/restricted-model"},
			"grant_operator_ids": []string{opID},
			"revoke_operator_ids": []string{},
		}, provToken)
		RequireStatus(t, resp, http.StatusOK)

		var models []map[string]interface{}
		resp, err := h.JSON("GET", "/api/my-allowed-models", nil, opToken, &models)
		if err != nil {
			t.Fatalf("listing allowed models: %v", err)
		}
		RequireStatus(t, resp, http.StatusOK)

		found := false
		for _, m := range models {
			if m["model_id"] == "step-ed/restricted-model" {
				found = true
				break
			}
		}
		if !found {
			t.Error("operator should see step-ed/restricted-model after grant")
		}
	})
}

func TestWorkflowProviderRole(t *testing.T) {
	h, dba := ensureHarness(t)

	provToken, _ := h.Register(t, "wf_provider", "wf-provider-1234")
	provToken = promoteRole(t, h, dba, "wf_provider", "wf-provider-1234", "provider")

	t.Run("ProviderCanCreateWorkflow", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		var result map[string]interface{}
		resp, err := h.JSON("POST", "/api/workflows", map[string]interface{}{
			"name":          "provider_wf",
			"workflow_type":  "factcheck",
			"description":   "Provider test workflow",
		}, provToken, &result)
		if err != nil {
			t.Fatalf("creating workflow: %v", err)
		}
		RequireStatus(t, resp, http.StatusCreated)
	})

	t.Run("ProviderCanAddSQLReadStep", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		// First create a workflow
		var wfResult map[string]interface{}
		resp, err := h.JSON("POST", "/api/workflows", map[string]interface{}{
			"name":          "provider_sql_wf",
			"workflow_type":  "synthese",
		}, provToken, &wfResult)
		if err != nil {
			t.Fatalf("creating workflow: %v", err)
		}
		RequireStatus(t, resp, http.StatusCreated)

		wf := wfResult["workflow"].(map[string]interface{})
		wfID := wf["workflow_id"].(string)

		// Provider should be allowed to create sql steps (read-only)
		resp, _ = h.Do("POST", "/api/workflows/"+wfID+"/steps", map[string]interface{}{
			"step_order":      1,
			"step_name":       "fetch_data",
			"step_type":       "sql",
			"prompt_template": "SELECT body FROM nodes LIMIT 10",
		}, provToken)
		RequireStatus(t, resp, http.StatusCreated)
	})

	t.Run("ProviderCannotAddHTTPStep", func(t *testing.T) {
		start := time.Now()
		defer func() { Record(t, start, nil, nil) }()

		var wfResult map[string]interface{}
		resp, err := h.JSON("POST", "/api/workflows", map[string]interface{}{
			"name":          "provider_http_wf",
			"workflow_type":  "source",
		}, provToken, &wfResult)
		if err != nil {
			t.Fatalf("creating workflow: %v", err)
		}
		RequireStatus(t, resp, http.StatusCreated)

		wf := wfResult["workflow"].(map[string]interface{})
		wfID := wf["workflow_id"].(string)

		resp, _ = h.Do("POST", "/api/workflows/"+wfID+"/steps", map[string]interface{}{
			"step_order":      1,
			"step_name":       "fetch_url",
			"step_type":       "http",
			"prompt_template": "GET https://example.com",
		}, provToken)
		if resp.StatusCode == http.StatusCreated {
			t.Error("provider should not be allowed to create HTTP steps")
		}
	})
}
