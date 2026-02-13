package db

import (
	"database/sql"
	"fmt"
	"time"
)

// Envelope is a persistent routing ticket for piece transit.
// It holds NO content — only routing metadata (source, targets, status).
type Envelope struct {
	ID             string           `json:"id"`
	BatchID        *string          `json:"batch_id,omitempty"`
	SourceType     string           `json:"source_type"`
	SourceUserID   *string          `json:"source_user_id,omitempty"`
	SourceNodeID   *string          `json:"source_node_id,omitempty"`
	SourceCallback *string          `json:"source_callback,omitempty"`
	PieceHash      string           `json:"piece_hash"`
	Status         string           `json:"status"`
	TargetCount    int              `json:"target_count"`
	DeliveredCount int              `json:"delivered_count"`
	Error          *string          `json:"error,omitempty"`
	ExpiresAt      time.Time        `json:"expires_at"`
	CreatedAt      time.Time        `json:"created_at"`
	UpdatedAt      time.Time        `json:"updated_at"`
	Targets        []EnvelopeTarget `json:"targets,omitempty"`
}

// EnvelopeTarget is a delivery destination for an envelope.
type EnvelopeTarget struct {
	ID          string     `json:"id"`
	EnvelopeID  string     `json:"envelope_id"`
	TargetType  string     `json:"target_type"`
	TargetConf  string     `json:"target_config"`
	Status      string     `json:"status"`
	DeliveredAt *time.Time `json:"delivered_at,omitempty"`
	Error       *string    `json:"error,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
}

// CreateEnvelopeInput is the input for creating a new envelope with targets.
type CreateEnvelopeInput struct {
	BatchID        *string              `json:"batch_id"`
	SourceType     string               `json:"source_type"`
	SourceUserID   *string              `json:"source_user_id"`
	SourceNodeID   *string              `json:"source_node_id"`
	SourceCallback *string              `json:"source_callback"`
	PieceHash      string               `json:"piece_hash"`
	TTLMinutes     int                  `json:"ttl_minutes"`
	Targets        []CreateTargetInput  `json:"targets"`
}

// CreateTargetInput is the input for a single delivery target.
type CreateTargetInput struct {
	TargetType string `json:"target_type"`
	TargetConf string `json:"target_config"`
}

// CreateEnvelope creates an envelope and its targets in a single transaction.
func (db *DB) CreateEnvelope(input CreateEnvelopeInput) (*Envelope, error) {
	if len(input.Targets) == 0 {
		return nil, fmt.Errorf("at least one target is required")
	}

	ttl := input.TTLMinutes
	if ttl <= 0 {
		ttl = 15
	}
	expiresAt := time.Now().UTC().Add(time.Duration(ttl) * time.Minute)

	id := NewID()

	tx, err := db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	_, err = tx.Exec(`
		INSERT INTO envelopes (id, batch_id, source_type, source_user_id, source_node_id,
			source_callback, piece_hash, status, target_count, delivered_count, expires_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, 'pending', ?, 0, ?)`,
		id, input.BatchID, input.SourceType, input.SourceUserID, input.SourceNodeID,
		input.SourceCallback, input.PieceHash, len(input.Targets), expiresAt)
	if err != nil {
		return nil, fmt.Errorf("inserting envelope: %w", err)
	}

	for _, t := range input.Targets {
		tid := NewID()
		_, err = tx.Exec(`
			INSERT INTO envelope_targets (id, envelope_id, target_type, target_config, status)
			VALUES (?, ?, ?, ?, 'pending')`, tid, id, t.TargetType, t.TargetConf)
		if err != nil {
			return nil, fmt.Errorf("inserting target: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}

	return db.GetEnvelope(id)
}

// GetEnvelope returns an envelope with all its targets.
func (db *DB) GetEnvelope(id string) (*Envelope, error) {
	e := &Envelope{}
	var batchID, userID, nodeID, callback, errMsg sql.NullString
	err := db.QueryRow(`
		SELECT id, batch_id, source_type, source_user_id, source_node_id, source_callback,
			piece_hash, status, target_count, delivered_count, error, expires_at, created_at, updated_at
		FROM envelopes WHERE id = ?`, id).Scan(
		&e.ID, &batchID, &e.SourceType, &userID, &nodeID, &callback,
		&e.PieceHash, &e.Status, &e.TargetCount, &e.DeliveredCount, &errMsg,
		&e.ExpiresAt, &e.CreatedAt, &e.UpdatedAt)
	if err != nil {
		return nil, err
	}
	if batchID.Valid {
		e.BatchID = &batchID.String
	}
	if userID.Valid {
		e.SourceUserID = &userID.String
	}
	if nodeID.Valid {
		e.SourceNodeID = &nodeID.String
	}
	if callback.Valid {
		e.SourceCallback = &callback.String
	}
	if errMsg.Valid {
		e.Error = &errMsg.String
	}

	targets, err := db.getEnvelopeTargets(id)
	if err != nil {
		return nil, err
	}
	e.Targets = targets

	return e, nil
}

func (db *DB) getEnvelopeTargets(envelopeID string) ([]EnvelopeTarget, error) {
	rows, err := db.Query(`
		SELECT id, envelope_id, target_type, target_config, status, delivered_at, error, created_at
		FROM envelope_targets WHERE envelope_id = ?
		ORDER BY created_at ASC`, envelopeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var targets []EnvelopeTarget
	for rows.Next() {
		var t EnvelopeTarget
		var deliveredAt sql.NullTime
		var errMsg sql.NullString
		if err := rows.Scan(&t.ID, &t.EnvelopeID, &t.TargetType, &t.TargetConf,
			&t.Status, &deliveredAt, &errMsg, &t.CreatedAt); err != nil {
			return nil, err
		}
		if deliveredAt.Valid {
			t.DeliveredAt = &deliveredAt.Time
		}
		if errMsg.Valid {
			t.Error = &errMsg.String
		}
		targets = append(targets, t)
	}
	return targets, nil
}

// UpdateEnvelopeStatus transitions the envelope status and updates the timestamp.
func (db *DB) UpdateEnvelopeStatus(id, status string, errMsg *string) error {
	_, err := db.Exec(`
		UPDATE envelopes SET status = ?, error = ?, updated_at = datetime('now')
		WHERE id = ?`, status, errMsg, id)
	return err
}

// DeliverTarget marks a single target as delivered and increments the envelope counter.
// If all targets are delivered, sets envelope status to 'delivered'.
// If some fail, sets 'partial'.
func (db *DB) DeliverTarget(envelopeID, targetID string) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	_, err = tx.Exec(`
		UPDATE envelope_targets SET status = 'delivered', delivered_at = datetime('now')
		WHERE id = ? AND envelope_id = ?`, targetID, envelopeID)
	if err != nil {
		return err
	}

	_, err = tx.Exec(`
		UPDATE envelopes SET delivered_count = delivered_count + 1, updated_at = datetime('now')
		WHERE id = ?`, envelopeID)
	if err != nil {
		return err
	}

	// Check if all targets are done
	var targetCount, deliveredCount, failedCount int
	err = tx.QueryRow(`SELECT target_count, delivered_count FROM envelopes WHERE id = ?`, envelopeID).
		Scan(&targetCount, &deliveredCount)
	if err != nil {
		return err
	}
	// delivered_count was just incremented above
	deliveredCount++

	err = tx.QueryRow(`SELECT COUNT(*) FROM envelope_targets WHERE envelope_id = ? AND status = 'failed'`, envelopeID).
		Scan(&failedCount)
	if err != nil {
		return err
	}

	if deliveredCount+failedCount >= targetCount {
		newStatus := "delivered"
		if failedCount > 0 {
			newStatus = "partial"
		}
		_, err = tx.Exec(`UPDATE envelopes SET status = ?, updated_at = datetime('now') WHERE id = ?`, newStatus, envelopeID)
		if err != nil {
			return err
		}
	}

	return tx.Commit()
}

// FailTarget marks a single target as failed and records the error.
func (db *DB) FailTarget(envelopeID, targetID, errMsg string) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	_, err = tx.Exec(`
		UPDATE envelope_targets SET status = 'failed', error = ?
		WHERE id = ? AND envelope_id = ?`, errMsg, targetID, envelopeID)
	if err != nil {
		return err
	}

	// Check if all targets are now resolved
	var targetCount, deliveredCount, failedCount int
	err = tx.QueryRow(`SELECT target_count, delivered_count FROM envelopes WHERE id = ?`, envelopeID).
		Scan(&targetCount, &deliveredCount)
	if err != nil {
		return err
	}

	err = tx.QueryRow(`SELECT COUNT(*) FROM envelope_targets WHERE envelope_id = ? AND status = 'failed'`, envelopeID).
		Scan(&failedCount)
	if err != nil {
		return err
	}

	if deliveredCount+failedCount >= targetCount {
		newStatus := "partial"
		if deliveredCount == 0 {
			newStatus = "failed"
		}
		_, err = tx.Exec(`UPDATE envelopes SET status = ?, updated_at = datetime('now') WHERE id = ?`, newStatus, envelopeID)
		if err != nil {
			return err
		}
	}

	return tx.Commit()
}

// ListEnvelopesByUser returns envelopes for a given user, most recent first.
func (db *DB) ListEnvelopesByUser(userID string, limit int) ([]*Envelope, error) {
	if limit <= 0 {
		limit = 20
	}
	rows, err := db.Query(`
		SELECT id, batch_id, source_type, source_user_id, source_node_id, source_callback,
			piece_hash, status, target_count, delivered_count, error, expires_at, created_at, updated_at
		FROM envelopes WHERE source_user_id = ?
		ORDER BY created_at DESC
		LIMIT ?`, userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return scanEnvelopeRows(rows)
}

// ListEnvelopesByBatch returns all envelopes in a batch.
func (db *DB) ListEnvelopesByBatch(batchID string) ([]*Envelope, error) {
	rows, err := db.Query(`
		SELECT id, batch_id, source_type, source_user_id, source_node_id, source_callback,
			piece_hash, status, target_count, delivered_count, error, expires_at, created_at, updated_at
		FROM envelopes WHERE batch_id = ?
		ORDER BY created_at ASC`, batchID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return scanEnvelopeRows(rows)
}

// ExpireEnvelopes marks all overdue envelopes as expired. Returns the count.
func (db *DB) ExpireEnvelopes() (int64, error) {
	res, err := db.Exec(`
		UPDATE envelopes SET status = 'expired', updated_at = datetime('now')
		WHERE status NOT IN ('delivered','expired','failed') AND expires_at < datetime('now')`)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func scanEnvelopeRows(rows *sql.Rows) ([]*Envelope, error) {
	var results []*Envelope
	for rows.Next() {
		e := &Envelope{}
		var batchID, userID, nodeID, callback, errMsg sql.NullString
		if err := rows.Scan(&e.ID, &batchID, &e.SourceType, &userID, &nodeID, &callback,
			&e.PieceHash, &e.Status, &e.TargetCount, &e.DeliveredCount, &errMsg,
			&e.ExpiresAt, &e.CreatedAt, &e.UpdatedAt); err != nil {
			return nil, err
		}
		if batchID.Valid {
			e.BatchID = &batchID.String
		}
		if userID.Valid {
			e.SourceUserID = &userID.String
		}
		if nodeID.Valid {
			e.SourceNodeID = &nodeID.String
		}
		if callback.Valid {
			e.SourceCallback = &callback.String
		}
		if errMsg.Valid {
			e.Error = &errMsg.String
		}
		results = append(results, e)
	}
	return results, nil
}
