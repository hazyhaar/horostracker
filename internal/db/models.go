// CLAUDE:SUMMARY LLM model catalogue DB — upsert, list, and query available models with provider/capability metadata
package db

import (
	"database/sql"
	"strings"
	"time"
)

// AvailableModel represents an LLM model in the discovery catalogue.
type AvailableModel struct {
	ModelID          string     `json:"model_id"`
	Provider         string     `json:"provider"`
	ModelName        string     `json:"model_name"`
	DisplayName      *string    `json:"display_name,omitempty"`
	ContextWindow    *int       `json:"context_window,omitempty"`
	IsAvailable      bool       `json:"is_available"`
	LastCheckAt      *time.Time `json:"last_check_at,omitempty"`
	LastError        *string    `json:"last_error,omitempty"`
	CapabilitiesJSON string     `json:"capabilities_json"`
	DiscoveredAt     time.Time  `json:"discovered_at"`
	OwnerID          *string    `json:"owner_id,omitempty"`
}

// UpsertModel inserts or updates a model in the catalogue.
func (db *FlowsDB) UpsertModel(m *AvailableModel) error {
	_, err := db.Exec(`
		INSERT INTO available_models (model_id, provider, model_name, display_name, context_window,
			is_available, last_check_at, last_error, capabilities_json)
		VALUES (?, ?, ?, ?, ?, ?, datetime('now'), ?, ?)
		ON CONFLICT(model_id) DO UPDATE SET
			is_available = excluded.is_available,
			last_check_at = datetime('now'),
			last_error = excluded.last_error,
			display_name = COALESCE(excluded.display_name, available_models.display_name),
			context_window = COALESCE(excluded.context_window, available_models.context_window),
			capabilities_json = excluded.capabilities_json`,
		m.ModelID, m.Provider, m.ModelName, m.DisplayName, m.ContextWindow,
		boolToInt(m.IsAvailable), m.LastError, m.CapabilitiesJSON)
	return err
}

// ListModels returns models filtered by provider and availability.
func (db *FlowsDB) ListModels(provider string, availableOnly bool) ([]AvailableModel, error) {
	query := `SELECT model_id, provider, model_name, display_name, context_window,
		is_available, last_check_at, last_error, capabilities_json, discovered_at, owner_id
		FROM available_models WHERE 1=1`
	var args []interface{}

	if provider != "" {
		query += ` AND provider = ?`
		args = append(args, provider)
	}
	if availableOnly {
		query += ` AND is_available = 1`
	}
	query += ` ORDER BY provider, model_name`

	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return scanModels(rows)
}

func scanModels(rows *sql.Rows) ([]AvailableModel, error) {
	var models []AvailableModel
	for rows.Next() {
		var m AvailableModel
		var displayName, lastError, ownerID sql.NullString
		var contextWindow sql.NullInt64
		var lastCheckAt sql.NullTime
		var isAvail int
		if err := rows.Scan(&m.ModelID, &m.Provider, &m.ModelName, &displayName, &contextWindow,
			&isAvail, &lastCheckAt, &lastError, &m.CapabilitiesJSON, &m.DiscoveredAt, &ownerID); err != nil {
			return nil, err
		}
		m.IsAvailable = isAvail == 1
		if displayName.Valid {
			m.DisplayName = &displayName.String
		}
		if contextWindow.Valid {
			cw := int(contextWindow.Int64)
			m.ContextWindow = &cw
		}
		if lastCheckAt.Valid {
			m.LastCheckAt = &lastCheckAt.Time
		}
		if lastError.Valid {
			m.LastError = &lastError.String
		}
		if ownerID.Valid {
			m.OwnerID = &ownerID.String
		}
		models = append(models, m)
	}
	return models, rows.Err()
}

// MarkModelUnavailable sets a model as unavailable with an error message.
func (db *FlowsDB) MarkModelUnavailable(modelID, errorMsg string) error {
	_, err := db.Exec(`
		UPDATE available_models SET is_available = 0, last_error = ?, last_check_at = datetime('now')
		WHERE model_id = ?`, errorMsg, modelID)
	return err
}

// GetModelsByProvider returns available model IDs for a provider.
func (db *FlowsDB) GetModelsByProvider(provider string) ([]string, error) {
	rows, err := db.Query(`
		SELECT model_id FROM available_models WHERE provider = ? AND is_available = 1
		ORDER BY model_name`, provider)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// MarkAllUnavailableForProvider marks all models for a provider as unavailable.
func (db *FlowsDB) MarkAllUnavailableForProvider(provider string) error {
	_, err := db.Exec(`
		UPDATE available_models SET is_available = 0, last_check_at = datetime('now')
		WHERE provider = ?`, provider)
	return err
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// --- Model Grants ---

// ModelGrant represents a granular access control rule for provider/model usage.
type ModelGrant struct {
	GrantID     string    `json:"grant_id"`
	GranteeType string    `json:"grantee_type"`
	GranteeID   string    `json:"grantee_id"`
	ModelID     string    `json:"model_id"`
	StepType    string    `json:"step_type"`
	Effect      string    `json:"effect"`
	CreatedBy   string    `json:"created_by"`
	CreatedAt   time.Time `json:"created_at"`
}

// CreateGrant inserts a new model grant.
func (db *FlowsDB) CreateGrant(g *ModelGrant) error {
	_, err := db.Exec(`
		INSERT INTO model_grants (grant_id, grantee_type, grantee_id, model_id, step_type, effect, created_by)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		g.GrantID, g.GranteeType, g.GranteeID, g.ModelID, g.StepType, g.Effect, g.CreatedBy)
	return err
}

// DeleteGrant removes a model grant by ID.
func (db *FlowsDB) DeleteGrant(grantID string) error {
	_, err := db.Exec(`DELETE FROM model_grants WHERE grant_id = ?`, grantID)
	return err
}

// ListGrants returns grants filtered by grantee type and ID.
func (db *FlowsDB) ListGrants(granteeType, granteeID string) ([]ModelGrant, error) {
	rows, err := db.Query(`
		SELECT grant_id, grantee_type, grantee_id, model_id, step_type, effect, created_by, created_at
		FROM model_grants WHERE grantee_type = ? AND grantee_id = ?
		ORDER BY created_at`, granteeType, granteeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanGrants(rows)
}

// ListAllGrants returns all model grants.
func (db *FlowsDB) ListAllGrants() ([]ModelGrant, error) {
	rows, err := db.Query(`
		SELECT grant_id, grantee_type, grantee_id, model_id, step_type, effect, created_by, created_at
		FROM model_grants ORDER BY grantee_type, grantee_id, created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanGrants(rows)
}

func scanGrants(rows *sql.Rows) ([]ModelGrant, error) {
	var grants []ModelGrant
	for rows.Next() {
		var g ModelGrant
		if err := rows.Scan(&g.GrantID, &g.GranteeType, &g.GranteeID,
			&g.ModelID, &g.StepType, &g.Effect, &g.CreatedBy, &g.CreatedAt); err != nil {
			return nil, err
		}
		grants = append(grants, g)
	}
	return grants, rows.Err()
}

// ModelExists returns true if the model_id exists in available_models.
func (db *FlowsDB) ModelExists(modelID string) bool {
	var count int
	_ = db.QueryRow(`SELECT COUNT(*) FROM available_models WHERE model_id = ?`, modelID).Scan(&count)
	return count > 0
}

// ModelIsAvailable returns true if the model exists and is_available = 1.
func (db *FlowsDB) ModelIsAvailable(modelID string) bool {
	var count int
	_ = db.QueryRow(`SELECT COUNT(*) FROM available_models WHERE model_id = ? AND is_available = 1`, modelID).Scan(&count)
	return count > 0
}

// CreateModel inserts a provider-owned model into the catalogue.
func (db *FlowsDB) CreateModel(m *AvailableModel) error {
	_, err := db.Exec(`
		INSERT INTO available_models (model_id, provider, model_name, display_name, context_window,
			is_available, capabilities_json, owner_id)
		VALUES (?, ?, ?, ?, ?, 1, ?, ?)`,
		m.ModelID, m.Provider, m.ModelName, m.DisplayName, m.ContextWindow,
		m.CapabilitiesJSON, m.OwnerID)
	return err
}

// ListModelsByOwner returns models owned by a specific provider.
func (db *FlowsDB) ListModelsByOwner(ownerID string) ([]AvailableModel, error) {
	rows, err := db.Query(`
		SELECT model_id, provider, model_name, display_name, context_window,
			is_available, last_check_at, last_error, capabilities_json, discovered_at, owner_id
		FROM available_models WHERE owner_id = ?
		ORDER BY provider, model_name`, ownerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanModels(rows)
}

// ListAllowedModels returns models that a user is explicitly granted access to,
// plus auto-discovered models (owner_id IS NULL) that remain accessible to all.
func (db *FlowsDB) ListAllowedModels(userID, role string) ([]AvailableModel, error) {
	rows, err := db.Query(`
		SELECT DISTINCT am.model_id, am.provider, am.model_name, am.display_name, am.context_window,
			am.is_available, am.last_check_at, am.last_error, am.capabilities_json, am.discovered_at, am.owner_id
		FROM available_models am
		WHERE am.is_available = 1
		  AND (
		    am.owner_id IS NULL
		    OR EXISTS (
		      SELECT 1 FROM model_grants mg
		      WHERE mg.model_id = am.model_id
		        AND mg.effect = 'allow'
		        AND (
		          (mg.grantee_type = 'user' AND mg.grantee_id = ?)
		          OR (mg.grantee_type = 'role' AND mg.grantee_id = ?)
		        )
		    )
		  )
		ORDER BY am.provider, am.model_name`, userID, role)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanModels(rows)
}

// BulkSetGrants creates grants for grantOperatorIDs and removes grants for revokeOperatorIDs
// across all specified modelIDs, using step_type='*' and effect='allow'.
func (db *FlowsDB) BulkSetGrants(modelIDs, grantOperatorIDs, revokeOperatorIDs []string, createdBy string) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	// Revoke: delete existing grants for revoke list
	for _, opID := range revokeOperatorIDs {
		for _, modelID := range modelIDs {
			_, _ = tx.Exec(`DELETE FROM model_grants
				WHERE grantee_type = 'user' AND grantee_id = ? AND model_id = ? AND step_type = '*'`,
				opID, modelID)
		}
	}

	// Grant: upsert grants for grant list
	for _, opID := range grantOperatorIDs {
		for _, modelID := range modelIDs {
			grantID := NewID()
			_, _ = tx.Exec(`INSERT INTO model_grants (grant_id, grantee_type, grantee_id, model_id, step_type, effect, created_by)
				VALUES (?, 'user', ?, ?, '*', 'allow', ?)
				ON CONFLICT(grantee_type, grantee_id, model_id, step_type) DO NOTHING`,
				grantID, opID, modelID, createdBy)
		}
	}

	return tx.Commit()
}

// CheckModelGrant evaluates the grant hierarchy for a user+role+model+stepType combination.
// Returns (allowed bool, explicit bool) where explicit indicates a grant was found.
// If no grant matches at all, explicit is false and the caller should fall back to AllowedStepTypes.
func (db *FlowsDB) CheckModelGrant(userID, role, modelID, stepType string) (allowed bool, explicit bool) {
	// Extract provider from model_id (format: "provider/model_name")
	provider := ""
	if idx := strings.Index(modelID, "/"); idx > 0 {
		provider = modelID[:idx]
	}

	// Priority levels (highest first):
	// 1. user + specific model + specific step_type
	// 2. user + specific model + wildcard step_type
	// 3. user + provider wildcard + specific step_type
	// 4. user + provider wildcard + wildcard step_type
	// 5. role + specific model + specific step_type
	// 6. role + specific model + wildcard step_type
	// 7. role + provider wildcard + specific step_type
	// 8. role + provider wildcard + wildcard step_type

	type candidate struct {
		granteeType string
		granteeID   string
		modelID     string
		stepType    string
	}

	candidates := []candidate{
		{"user", userID, modelID, stepType},
		{"user", userID, modelID, "*"},
	}
	if provider != "" {
		candidates = append(candidates,
			candidate{"user", userID, provider + "/*", stepType},
			candidate{"user", userID, provider + "/*", "*"},
		)
	}
	candidates = append(candidates,
		candidate{"user", userID, "*", stepType},
		candidate{"user", userID, "*", "*"},
	)

	if role != "" {
		candidates = append(candidates,
			candidate{"role", role, modelID, stepType},
			candidate{"role", role, modelID, "*"},
		)
		if provider != "" {
			candidates = append(candidates,
				candidate{"role", role, provider + "/*", stepType},
				candidate{"role", role, provider + "/*", "*"},
			)
		}
		candidates = append(candidates,
			candidate{"role", role, "*", stepType},
			candidate{"role", role, "*", "*"},
		)
	}

	for _, c := range candidates {
		var effect string
		err := db.QueryRow(`
			SELECT effect FROM model_grants
			WHERE grantee_type = ? AND grantee_id = ? AND model_id = ? AND step_type = ?`,
			c.granteeType, c.granteeID, c.modelID, c.stepType).Scan(&effect)
		if err == nil {
			return effect == "allow", true
		}
	}

	// No grant matched at all
	return false, false
}
