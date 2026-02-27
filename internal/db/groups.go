// CLAUDE:SUMMARY Operator groups DB — CRUD for provider-scoped groups and group membership management
package db

import (
	"database/sql"
	"time"
)

// OperatorGroup represents a provider-scoped group for bulk grant management.
type OperatorGroup struct {
	GroupID     string    `json:"group_id"`
	ProviderID  string    `json:"provider_id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	CreatedAt   time.Time `json:"created_at"`
}

// OperatorGroupMember represents membership of an operator in a group.
type OperatorGroupMember struct {
	GroupID    string    `json:"group_id"`
	OperatorID string    `json:"operator_id"`
	AddedAt    time.Time `json:"added_at"`
}

// CreateGroup inserts a new operator group scoped to a provider.
func (db *FlowsDB) CreateGroup(g *OperatorGroup) error {
	_, err := db.Exec(`
		INSERT INTO operator_groups (group_id, provider_id, name, description)
		VALUES (?, ?, ?, ?)`,
		g.GroupID, g.ProviderID, g.Name, g.Description)
	return err
}

// UpdateGroup renames or modifies a group.
func (db *FlowsDB) UpdateGroup(groupID, name, description string) error {
	_, err := db.Exec(`
		UPDATE operator_groups SET name = ?, description = ?
		WHERE group_id = ?`,
		name, description, groupID)
	return err
}

// DeleteGroup removes a group (CASCADE deletes members).
func (db *FlowsDB) DeleteGroup(groupID string) error {
	_, err := db.Exec(`DELETE FROM operator_groups WHERE group_id = ?`, groupID)
	return err
}

// GetGroup retrieves a single group by ID.
func (db *FlowsDB) GetGroup(groupID string) (*OperatorGroup, error) {
	g := &OperatorGroup{}
	err := db.QueryRow(`
		SELECT group_id, provider_id, name, COALESCE(description,''), created_at
		FROM operator_groups WHERE group_id = ?`, groupID).Scan(
		&g.GroupID, &g.ProviderID, &g.Name, &g.Description, &g.CreatedAt)
	if err != nil {
		return nil, err
	}
	return g, nil
}

// ListGroups returns all groups owned by a provider.
func (db *FlowsDB) ListGroups(providerID string) ([]OperatorGroup, error) {
	rows, err := db.Query(`
		SELECT group_id, provider_id, name, COALESCE(description,''), created_at
		FROM operator_groups WHERE provider_id = ?
		ORDER BY name`, providerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var groups []OperatorGroup
	for rows.Next() {
		var g OperatorGroup
		if err := rows.Scan(&g.GroupID, &g.ProviderID, &g.Name, &g.Description, &g.CreatedAt); err != nil {
			return nil, err
		}
		groups = append(groups, g)
	}
	return groups, rows.Err()
}

// AddMember adds an operator to a group.
func (db *FlowsDB) AddMember(groupID, operatorID string) error {
	_, err := db.Exec(`
		INSERT INTO operator_group_members (group_id, operator_id)
		VALUES (?, ?)`,
		groupID, operatorID)
	return err
}

// RemoveMember removes an operator from a group.
func (db *FlowsDB) RemoveMember(groupID, operatorID string) error {
	_, err := db.Exec(`
		DELETE FROM operator_group_members WHERE group_id = ? AND operator_id = ?`,
		groupID, operatorID)
	return err
}

// ListMembers returns all members of a group.
func (db *FlowsDB) ListMembers(groupID string) ([]OperatorGroupMember, error) {
	rows, err := db.Query(`
		SELECT group_id, operator_id, added_at
		FROM operator_group_members WHERE group_id = ?
		ORDER BY added_at`, groupID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var members []OperatorGroupMember
	for rows.Next() {
		var m OperatorGroupMember
		if err := rows.Scan(&m.GroupID, &m.OperatorID, &m.AddedAt); err != nil {
			return nil, err
		}
		members = append(members, m)
	}
	return members, rows.Err()
}

// ListUngroupedOperators returns operator user IDs that are not in any group
// owned by the given provider. Requires access to the nodes DB for user roles.
func (db *FlowsDB) ListUngroupedOperators(nodesDB *sql.DB, providerID string) ([]string, error) {
	rows, err := nodesDB.Query(`
		SELECT id FROM users WHERE role = 'operator'
		ORDER BY handle`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var allOps []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		allOps = append(allOps, id)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Get IDs that are in at least one group of this provider
	grouped := make(map[string]bool)
	gRows, err := db.Query(`
		SELECT DISTINCT ogm.operator_id
		FROM operator_group_members ogm
		JOIN operator_groups og ON ogm.group_id = og.group_id
		WHERE og.provider_id = ?`, providerID)
	if err != nil {
		return nil, err
	}
	defer gRows.Close()
	for gRows.Next() {
		var id string
		if err := gRows.Scan(&id); err != nil {
			return nil, err
		}
		grouped[id] = true
	}
	if err := gRows.Err(); err != nil {
		return nil, err
	}

	var ungrouped []string
	for _, id := range allOps {
		if !grouped[id] {
			ungrouped = append(ungrouped, id)
		}
	}
	return ungrouped, nil
}
