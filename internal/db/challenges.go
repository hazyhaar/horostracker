// CLAUDE:SUMMARY Challenge and moderation DB operations — CRUD for adversarial challenges, moderation scores, and leaderboard queries
package db

import (
	"database/sql"
	"time"
)

// Challenge represents an adversarial challenge run against a node.
type Challenge struct {
	ID             string     `json:"id"`
	NodeID         string     `json:"node_id"`
	FlowName       string     `json:"flow_name"`
	Status         string     `json:"status"`
	RequestedBy    string     `json:"requested_by"`
	TargetProvider *string    `json:"target_provider,omitempty"`
	TargetModel    *string    `json:"target_model,omitempty"`
	Score          *float64   `json:"score,omitempty"`
	Summary        *string    `json:"summary,omitempty"`
	FlowID         *string    `json:"flow_id,omitempty"`
	Error          *string    `json:"error,omitempty"`
	StartedAt      *time.Time `json:"started_at,omitempty"`
	CompletedAt    *time.Time `json:"completed_at,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
}

// ModerationScore holds multi-criteria moderation assessment for a node.
type ModerationScore struct {
	ID            string   `json:"id"`
	NodeID        string   `json:"node_id"`
	Evaluator     string   `json:"evaluator"`
	EvalSource    string   `json:"eval_source"`
	FactualScore  *float64 `json:"factual_score,omitempty"`
	SourceScore   *float64 `json:"source_score,omitempty"`
	ArgumentScore *float64 `json:"argument_score,omitempty"`
	CivilityScore *float64 `json:"civility_score,omitempty"`
	OverallScore  *float64 `json:"overall_score,omitempty"`
	Flags         string   `json:"flags"`
	Notes         *string  `json:"notes,omitempty"`
	ChallengeID   *string  `json:"challenge_id,omitempty"`
	CreatedAt     time.Time `json:"created_at"`
}

// CreateChallenge inserts a new adversarial challenge.
func (db *DB) CreateChallenge(nodeID, flowName, requestedBy string, targetProvider, targetModel *string) (*Challenge, error) {
	id := NewID()
	_, err := db.Exec(`
		INSERT INTO challenges (id, node_id, flow_name, status, requested_by, target_provider, target_model)
		VALUES (?, ?, ?, 'pending', ?, ?, ?)`,
		id, nodeID, flowName, requestedBy, targetProvider, targetModel)
	if err != nil {
		return nil, err
	}
	return db.GetChallenge(id)
}

// GetChallenge returns a challenge by ID.
func (db *DB) GetChallenge(id string) (*Challenge, error) {
	return scanChallenge(db.QueryRow(`
		SELECT id, node_id, flow_name, status, requested_by, target_provider, target_model,
			score, summary, flow_id, error, started_at, completed_at, created_at
		FROM challenges WHERE id = ?`, id))
}

// GetChallengesForNode returns all challenges for a node.
func (db *DB) GetChallengesForNode(nodeID string, limit int) ([]*Challenge, error) {
	if limit <= 0 {
		limit = 20
	}
	rows, err := db.Query(`
		SELECT id, node_id, flow_name, status, requested_by, target_provider, target_model,
			score, summary, flow_id, error, started_at, completed_at, created_at
		FROM challenges WHERE node_id = ?
		ORDER BY created_at DESC
		LIMIT ?`, nodeID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanChallengeRows(rows)
}

// UpdateChallengeRunning marks a challenge as running with a flow ID.
func (db *DB) UpdateChallengeRunning(id, flowID string) error {
	_, err := db.Exec(`
		UPDATE challenges SET status = 'running', flow_id = ?, started_at = datetime('now')
		WHERE id = ?`, flowID, id)
	return err
}

// UpdateChallengeCompleted marks a challenge as completed with score and summary.
func (db *DB) UpdateChallengeCompleted(id string, score float64, summary string) error {
	_, err := db.Exec(`
		UPDATE challenges SET status = 'completed', score = ?, summary = ?, completed_at = datetime('now')
		WHERE id = ?`, score, summary, id)
	return err
}

// UpdateChallengeFailed marks a challenge as failed.
func (db *DB) UpdateChallengeFailed(id, errMsg string) error {
	_, err := db.Exec(`
		UPDATE challenges SET status = 'failed', error = ?, completed_at = datetime('now')
		WHERE id = ?`, errMsg, id)
	return err
}

// CountChallengesForNode returns completed challenge count for a node.
func (db *DB) CountChallengesForNode(nodeID string) (int, error) {
	var count int
	err := db.QueryRow(`SELECT COUNT(*) FROM challenges WHERE node_id = ? AND status = 'completed'`, nodeID).Scan(&count)
	return count, err
}

// InsertModerationScore stores a multi-criteria moderation score.
func (db *DB) InsertModerationScore(ms ModerationScore) error {
	if ms.ID == "" {
		ms.ID = NewID()
	}
	_, err := db.Exec(`
		INSERT INTO moderation_scores (id, node_id, evaluator, eval_source,
			factual_score, source_score, argument_score, civility_score, overall_score,
			flags, notes, challenge_id)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		ms.ID, ms.NodeID, ms.Evaluator, ms.EvalSource,
		ms.FactualScore, ms.SourceScore, ms.ArgumentScore, ms.CivilityScore, ms.OverallScore,
		ms.Flags, ms.Notes, ms.ChallengeID)
	return err
}

// GetModerationScores returns moderation scores for a node.
func (db *DB) GetModerationScores(nodeID string) ([]*ModerationScore, error) {
	rows, err := db.Query(`
		SELECT id, node_id, evaluator, eval_source,
			factual_score, source_score, argument_score, civility_score, overall_score,
			flags, notes, challenge_id, created_at
		FROM moderation_scores WHERE node_id = ?
		ORDER BY created_at DESC`, nodeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []*ModerationScore
	for rows.Next() {
		ms := &ModerationScore{}
		var factual, source, argument, civility, overall sql.NullFloat64
		var notes, challengeID sql.NullString
		if err := rows.Scan(
			&ms.ID, &ms.NodeID, &ms.Evaluator, &ms.EvalSource,
			&factual, &source, &argument, &civility, &overall,
			&ms.Flags, &notes, &challengeID, &ms.CreatedAt,
		); err != nil {
			return nil, err
		}
		if factual.Valid {
			ms.FactualScore = &factual.Float64
		}
		if source.Valid {
			ms.SourceScore = &source.Float64
		}
		if argument.Valid {
			ms.ArgumentScore = &argument.Float64
		}
		if civility.Valid {
			ms.CivilityScore = &civility.Float64
		}
		if overall.Valid {
			ms.OverallScore = &overall.Float64
		}
		if notes.Valid {
			ms.Notes = &notes.String
		}
		if challengeID.Valid {
			ms.ChallengeID = &challengeID.String
		}
		results = append(results, ms)
	}
	return results, nil
}

// RecalculateTemperature recalculates a node's temperature based on activity.
// Rules:
//   - cold: < 3 children, < 5 votes, no challenges
//   - warm: 3+ children OR 5+ votes
//   - hot:  5+ children AND 10+ votes OR any completed challenge
//   - critical: 10+ children AND 20+ votes AND score divergence OR 3+ challenges
func (db *DB) RecalculateTemperature(nodeID string) (string, error) {
	var childCount, score, viewCount int
	err := db.QueryRow(`SELECT child_count, score, view_count FROM nodes WHERE id = ?`, nodeID).
		Scan(&childCount, &score, &viewCount)
	if err != nil {
		return "", err
	}

	// Count total votes (abs values)
	var voteCount int
	db.QueryRow(`SELECT COUNT(*) FROM votes WHERE node_id = ?`, nodeID).Scan(&voteCount)

	// Count objections
	var objectionCount int
	db.QueryRow(`SELECT COUNT(*) FROM nodes WHERE parent_id = ? AND node_type = 'objection'`, nodeID).Scan(&objectionCount)

	// Count completed challenges
	challengeCount, _ := db.CountChallengesForNode(nodeID)

	temp := "cold"
	switch {
	case (childCount >= 10 && voteCount >= 20) || challengeCount >= 3 || (objectionCount >= 5 && voteCount >= 15):
		temp = "critical"
	case (childCount >= 5 && voteCount >= 10) || challengeCount >= 1 || (objectionCount >= 3 && voteCount >= 5):
		temp = "hot"
	case childCount >= 3 || voteCount >= 5 || objectionCount >= 1:
		temp = "warm"
	}

	_, err = db.Exec(`UPDATE nodes SET temperature = ?, updated_at = datetime('now') WHERE id = ?`, temp, nodeID)
	if err != nil {
		return "", err
	}
	return temp, nil
}

// RecalculateRootTemperature recalculates the root node's temperature.
// Called after tree-level events (new child, challenge, etc.).
func (db *DB) RecalculateRootTemperature(nodeID string) (string, error) {
	var rootID string
	err := db.QueryRow(`SELECT root_id FROM nodes WHERE id = ?`, nodeID).Scan(&rootID)
	if err != nil {
		return "", err
	}
	return db.RecalculateTemperature(rootID)
}

// ChallengeLeaderboard returns users ranked by challenge contribution.
type ChallengeLeaderEntry struct {
	UserID          string  `json:"user_id"`
	Handle          string  `json:"handle"`
	ChallengesRun   int     `json:"challenges_run"`
	AvgScore        float64 `json:"avg_score"`
	TotalCompleted  int     `json:"total_completed"`
}

func (db *DB) GetChallengeLeaderboard(limit int) ([]ChallengeLeaderEntry, error) {
	if limit <= 0 {
		limit = 20
	}
	rows, err := db.Query(`
		SELECT c.requested_by, u.handle,
			COUNT(*) as total,
			COALESCE(AVG(c.score), 0) as avg_score,
			SUM(CASE WHEN c.status = 'completed' THEN 1 ELSE 0 END) as completed
		FROM challenges c
		JOIN users u ON u.id = c.requested_by
		GROUP BY c.requested_by
		ORDER BY completed DESC, avg_score DESC
		LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []ChallengeLeaderEntry
	for rows.Next() {
		var e ChallengeLeaderEntry
		if err := rows.Scan(&e.UserID, &e.Handle, &e.ChallengesRun, &e.AvgScore, &e.TotalCompleted); err != nil {
			return nil, err
		}
		results = append(results, e)
	}
	return results, nil
}

func scanChallenge(s interface{ Scan(...any) error }) (*Challenge, error) {
	c := &Challenge{}
	var targetProvider, targetModel, summary, flowID, errMsg sql.NullString
	var score sql.NullFloat64
	var startedAt, completedAt sql.NullTime
	err := s.Scan(
		&c.ID, &c.NodeID, &c.FlowName, &c.Status, &c.RequestedBy,
		&targetProvider, &targetModel, &score, &summary, &flowID, &errMsg,
		&startedAt, &completedAt, &c.CreatedAt,
	)
	if err != nil {
		return nil, err
	}
	if targetProvider.Valid {
		c.TargetProvider = &targetProvider.String
	}
	if targetModel.Valid {
		c.TargetModel = &targetModel.String
	}
	if score.Valid {
		c.Score = &score.Float64
	}
	if summary.Valid {
		c.Summary = &summary.String
	}
	if flowID.Valid {
		c.FlowID = &flowID.String
	}
	if errMsg.Valid {
		c.Error = &errMsg.String
	}
	if startedAt.Valid {
		c.StartedAt = &startedAt.Time
	}
	if completedAt.Valid {
		c.CompletedAt = &completedAt.Time
	}
	return c, nil
}

func scanChallengeRows(rows *sql.Rows) ([]*Challenge, error) {
	var results []*Challenge
	for rows.Next() {
		c, err := scanChallenge(rows)
		if err != nil {
			return nil, err
		}
		results = append(results, c)
	}
	return results, nil
}
