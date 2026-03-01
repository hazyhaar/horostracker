// CLAUDE:SUMMARY Content safety scorer — pattern-based content scoring (exact/substring/regex) with severity flagging
package db

import (
	"database/sql"
	"regexp"
	"strings"
	"time"
)

// SafetyResult represents a safety scoring result.
type SafetyResult struct {
	Score    float64  `json:"score"`
	Severity string   `json:"severity"`
	Flags    []string `json:"flags"`
	ScorerID string   `json:"scorer_id"`
}

// ScoreContent runs the local horostracker-v1 scorer on content.
// Returns a SafetyResult with score between 0.0 (dangerous) and 1.0 (safe).
func (db *DB) ScoreContent(content string) *SafetyResult {
	result := &SafetyResult{
		Score:    1.0,
		Severity: "info",
		ScorerID: "horostracker-v1",
	}

	lower := strings.ToLower(content)

	// Check against safety_patterns in DB
	rows, err := db.Query("SELECT pattern, pattern_type, severity FROM safety_patterns WHERE list_type IN ('block','flag')")
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var pattern, patternType, severity string
			if err := rows.Scan(&pattern, &patternType, &severity); err != nil {
				continue
			}
			matched := false
			switch patternType {
			case "exact":
				matched = strings.EqualFold(pattern, lower)
			case "substring":
				matched = strings.Contains(lower, strings.ToLower(pattern))
			case "regex":
				if re, err := regexp.Compile("(?i)" + pattern); err == nil {
					matched = re.MatchString(content)
				}
			}
			if matched {
				result.Flags = append(result.Flags, "pattern:"+pattern)
				result.Score -= severityPenalty(severity)
				if severityRank(severity) > severityRank(result.Severity) {
					result.Severity = severity
				}
			}
		}
	}

	// Built-in heuristics: prompt injection detection
	injectionPatterns := []string{
		"ignore all previous",
		"ignore your instructions",
		"system prompt",
		"output the",
		"disregard",
		"you are now",
		"pretend to be",
		"jailbreak",
	}
	for _, p := range injectionPatterns {
		if strings.Contains(lower, p) {
			result.Flags = append(result.Flags, "injection:"+p)
			result.Score -= 0.3
			if severityRank("medium") > severityRank(result.Severity) {
				result.Severity = "medium"
			}
		}
	}

	// Clamp score
	if result.Score < 0 {
		result.Score = 0
	}

	// Determine severity from score if still "info"
	if result.Score < 0.3 && severityRank(result.Severity) < severityRank("high") {
		result.Severity = "high"
	} else if result.Score < 0.7 && result.Severity == "info" {
		result.Severity = "low"
	}

	if result.Flags == nil {
		result.Flags = []string{}
	}

	return result
}

// SaveSafetyScore persists a safety score to the database.
func (db *DB) SaveSafetyScore(nodeID string, result *SafetyResult) error {
	id := NewID()
	flagsJSON := "[]"
	if len(result.Flags) > 0 {
		parts := make([]string, len(result.Flags))
		for i, f := range result.Flags {
			parts[i] = `"` + strings.ReplaceAll(f, `"`, `\"`) + `"`
		}
		flagsJSON = "[" + strings.Join(parts, ",") + "]"
	}
	_, err := db.Exec(
		`INSERT INTO safety_scores (id, node_id, scorer_id, score, severity, flags) VALUES (?, ?, ?, ?, ?, ?)`,
		id, nodeID, result.ScorerID, result.Score, result.Severity, flagsJSON)
	return err
}

// GetSafetyTimeline returns the safety scoring consensus for a node.
func (db *DB) GetSafetyTimeline(nodeID string) (map[string]interface{}, error) {
	rows, err := db.Query(
		`SELECT id, scorer_id, score, severity, flags, created_at FROM safety_scores WHERE node_id = ? ORDER BY created_at ASC`, nodeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var scores []map[string]interface{}
	var totalScore float64
	for rows.Next() {
		var id, scorerID, severity, flags string
		var score float64
		var createdAt time.Time
		if err := rows.Scan(&id, &scorerID, &score, &severity, &flags, &createdAt); err != nil {
			continue
		}
		totalScore += score
		scores = append(scores, map[string]interface{}{
			"id":         id,
			"scorer_id":  scorerID,
			"score":      score,
			"severity":   severity,
			"flags":      flags,
			"created_at": createdAt,
		})
	}

	avgScore := 0.0
	if len(scores) > 0 {
		avgScore = totalScore / float64(len(scores))
	}

	return map[string]interface{}{
		"node_id":      nodeID,
		"scores_count": float64(len(scores)),
		"avg_score":    avgScore,
		"scores":       scores,
	}, nil
}

// CreateSafetyPattern creates a new safety pattern.
func (db *DB) CreateSafetyPattern(pattern, patternType, listType, severity, language, category, description, createdBy string) (int64, error) {
	res, err := db.Exec(
		`INSERT INTO safety_patterns (pattern, pattern_type, list_type, severity, language, category, description, created_by) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		pattern, patternType, listType, severity, language, category, description, createdBy)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// SafetyPattern represents a row from safety_patterns.
type SafetyPattern struct {
	ID          int64  `json:"id"`
	Pattern     string `json:"pattern"`
	PatternType string `json:"pattern_type"`
	ListType    string `json:"list_type"`
	Severity    string `json:"severity"`
	Language    string `json:"language"`
	Category    string `json:"category"`
	Description string `json:"description"`
	VotesUp     int    `json:"votes_up"`
	VotesDown   int    `json:"votes_down"`
	CreatedBy   string `json:"created_by"`
	CreatedAt   string `json:"created_at"`
}

// ListSafetyPatterns returns all safety patterns.
func (db *DB) ListSafetyPatterns() ([]SafetyPattern, error) {
	rows, err := db.Query(`SELECT id, pattern, pattern_type, list_type, severity, COALESCE(language,''), COALESCE(category,''), COALESCE(description,''), votes_up, votes_down, COALESCE(created_by,''), created_at FROM safety_patterns ORDER BY id ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var patterns []SafetyPattern
	for rows.Next() {
		var p SafetyPattern
		if err := rows.Scan(&p.ID, &p.Pattern, &p.PatternType, &p.ListType, &p.Severity, &p.Language, &p.Category, &p.Description, &p.VotesUp, &p.VotesDown, &p.CreatedBy, &p.CreatedAt); err != nil {
			continue
		}
		patterns = append(patterns, p)
	}
	if patterns == nil {
		patterns = []SafetyPattern{}
	}
	return patterns, nil
}

// VoteSafetyPattern increments votes_up or votes_down for a pattern.
func (db *DB) VoteSafetyPattern(patternID int64, up bool) (int, int, error) {
	col := "votes_down"
	if up {
		col = "votes_up"
	}
	_, err := db.Exec("UPDATE safety_patterns SET "+col+" = "+col+" + 1 WHERE id = ?", patternID)
	if err != nil {
		return 0, 0, err
	}
	var votesUp, votesDown int
	err = db.QueryRow("SELECT votes_up, votes_down FROM safety_patterns WHERE id = ?", patternID).Scan(&votesUp, &votesDown)
	if err != nil {
		return 0, 0, err
	}
	return votesUp, votesDown, nil
}

// GetSafetyLeaderboard returns scorers ranked by number of scores produced.
func (db *DB) GetSafetyLeaderboard() ([]map[string]interface{}, error) {
	rows, err := db.Query(`
		SELECT scorer_id, COUNT(*) as scores_count, AVG(score) as avg_score
		FROM safety_scores
		GROUP BY scorer_id
		ORDER BY scores_count DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var leaderboard []map[string]interface{}
	for rows.Next() {
		var scorerID string
		var count int
		var avgScore float64
		if err := rows.Scan(&scorerID, &count, &avgScore); err != nil {
			continue
		}
		leaderboard = append(leaderboard, map[string]interface{}{
			"scorer_id":    scorerID,
			"scores_count": count,
			"avg_score":    avgScore,
		})
	}
	if leaderboard == nil {
		leaderboard = []map[string]interface{}{}
	}
	return leaderboard, nil
}

// GetClonesForNode returns all clones of a given source node.
func (db *DB) GetClonesForNode(sourceID string) ([]map[string]interface{}, error) {
	rows, err := db.Query(`
		SELECT nc.clone_id, n.node_type, COALESCE(n.visibility,'public'), n.created_at
		FROM node_clones nc
		JOIN nodes n ON n.id = nc.clone_id
		WHERE nc.source_id = ?`, sourceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var clones []map[string]interface{}
	for rows.Next() {
		var cloneID, nodeType, visibility string
		var createdAt time.Time
		if err := rows.Scan(&cloneID, &nodeType, &visibility, &createdAt); err != nil {
			continue
		}
		clones = append(clones, map[string]interface{}{
			"clone_id":   cloneID,
			"node_type":  nodeType,
			"visibility": visibility,
			"created_at": createdAt,
		})
	}
	if clones == nil {
		clones = []map[string]interface{}{}
	}
	return clones, nil
}

// GetClonesForTree returns all clones in a tree (clones of the root node + descendants).
func (db *DB) GetClonesForTree(rootID string) ([]map[string]interface{}, error) {
	rows, err := db.Query(`
		SELECT nc.clone_id, nc.source_id, n.node_type, COALESCE(n.visibility,'public'), n.created_at
		FROM node_clones nc
		JOIN nodes n ON n.id = nc.clone_id
		WHERE nc.source_id IN (SELECT id FROM nodes WHERE root_id = ?)`, rootID)
	if err != nil {
		if err == sql.ErrNoRows {
			return []map[string]interface{}{}, nil
		}
		return nil, err
	}
	defer rows.Close()

	var clones []map[string]interface{}
	for rows.Next() {
		var cloneID, sourceID, nodeType, visibility string
		var createdAt time.Time
		if err := rows.Scan(&cloneID, &sourceID, &nodeType, &visibility, &createdAt); err != nil {
			continue
		}
		clones = append(clones, map[string]interface{}{
			"clone_id":   cloneID,
			"source_id":  sourceID,
			"node_type":  nodeType,
			"visibility": visibility,
			"created_at": createdAt,
		})
	}
	if clones == nil {
		clones = []map[string]interface{}{}
	}
	return clones, nil
}

func severityPenalty(s string) float64 {
	switch s {
	case "critical":
		return 0.5
	case "high":
		return 0.4
	case "medium":
		return 0.3
	case "low":
		return 0.15
	default:
		return 0.05
	}
}

func severityRank(s string) int {
	switch s {
	case "critical":
		return 5
	case "high":
		return 4
	case "medium":
		return 3
	case "low":
		return 2
	case "info":
		return 1
	default:
		return 0
	}
}
