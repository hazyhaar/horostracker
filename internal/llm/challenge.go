// CLAUDE:SUMMARY Adversarial challenge runner — executes confrontation/red-team/fidelity flows against nodes and scores results
package llm

import (
	"context"
	"fmt"
	"log/slog"
	"regexp"
	"strconv"
	"strings"

	"github.com/hazyhaar/horostracker/internal/db"
)

// ChallengeRunner executes adversarial flows against nodes and stores results.
type ChallengeRunner struct {
	flowEngine *FlowEngine
	database   *db.DB
	logger     *slog.Logger
}

// NewChallengeRunner creates a challenge execution runner.
func NewChallengeRunner(flowEngine *FlowEngine, database *db.DB, logger *slog.Logger) *ChallengeRunner {
	return &ChallengeRunner{flowEngine: flowEngine, database: database, logger: logger}
}

// ValidFlows returns the names of available adversarial flows.
func ValidFlows() []string {
	return []string{"confrontation", "red_team", "fidelity_benchmark", "adversarial_detection", "deep_dive", "safety_scoring"}
}

// IsValidFlow checks if a flow name exists.
func IsValidFlow(name string) bool {
	for _, f := range ValidFlows() {
		if f == name {
			return true
		}
	}
	return false
}

// ChallengeResult holds the outcome of a challenge execution.
type ChallengeResult struct {
	ChallengeID string       `json:"challenge_id"`
	FlowResult  *FlowResult  `json:"flow_result"`
	Score       float64      `json:"score"`
	Summary     string       `json:"summary"`
	Moderation  *db.ModerationScore `json:"moderation,omitempty"`
}

// RunChallenge executes a challenge: runs the specified flow and extracts scoring.
func (cr *ChallengeRunner) RunChallenge(ctx context.Context, challenge *db.Challenge) (*ChallengeResult, error) {
	// Resolve flow config
	flow, err := cr.resolveFlow(challenge.FlowName)
	if err != nil {
		cr.database.UpdateChallengeFailed(challenge.ID, err.Error())
		return nil, err
	}

	// Get the node
	node, err := cr.database.GetNode(challenge.NodeID)
	if err != nil {
		cr.database.UpdateChallengeFailed(challenge.ID, "node not found: "+err.Error())
		return nil, fmt.Errorf("getting node: %w", err)
	}

	// Build body: for question nodes, serialize the full tree
	body := node.Body
	if node.NodeType == "question" {
		tree, treeErr := cr.database.GetTree(node.ID, 50)
		if treeErr == nil {
			body = serializeTree(tree, 0)
		}
	}

	// Resolve target provider/model
	targetProvider := ""
	targetModel := ""
	if challenge.TargetProvider != nil {
		targetProvider = *challenge.TargetProvider
	}
	if challenge.TargetModel != nil {
		targetModel = *challenge.TargetModel
	}

	// Mark as running
	flowID := db.NewID()
	cr.database.UpdateChallengeRunning(challenge.ID, flowID)

	cr.logger.Info("running challenge",
		"challenge_id", challenge.ID,
		"flow", challenge.FlowName,
		"node_id", challenge.NodeID,
		"target_provider", targetProvider,
	)

	// Execute flow
	fctx := FlowContext{
		FlowID:         flowID,
		NodeID:         challenge.NodeID,
		Body:           body,
		TargetProvider: targetProvider,
		TargetModel:    targetModel,
	}

	flowResult, err := cr.flowEngine.Execute(ctx, flow, fctx)
	if err != nil {
		cr.database.UpdateChallengeFailed(challenge.ID, err.Error())
		return nil, fmt.Errorf("executing flow: %w", err)
	}

	// Extract score and summary from the last step
	score, summary := cr.extractScoring(flowResult)

	// Mark completed
	cr.database.UpdateChallengeCompleted(challenge.ID, score, summary)

	// Create moderation score from challenge result
	modScore := cr.buildModerationScore(challenge, flowResult, score)
	if modScore != nil {
		cr.database.InsertModerationScore(*modScore)
	}

	// Recalculate temperature
	cr.database.RecalculateRootTemperature(challenge.NodeID)

	return &ChallengeResult{
		ChallengeID: challenge.ID,
		FlowResult:  flowResult,
		Score:       score,
		Summary:     summary,
		Moderation:  modScore,
	}, nil
}

// resolveFlow finds a flow config by name.
func (cr *ChallengeRunner) resolveFlow(name string) (FlowConfig, error) {
	for _, f := range CoreFlows() {
		if f.Name == name {
			return f, nil
		}
	}
	return FlowConfig{}, fmt.Errorf("unknown flow: %s", name)
}

// scorePattern matches patterns like "score: 75", "Score: 85/100", "resistance score: 60"
var scorePattern = regexp.MustCompile(`(?i)(?:overall|resistance|fidelity|detection|confidence|deceptiveness)?\s*score[:\s]+(\d+)`)

// extractScoring pulls a numeric score and summary from flow results.
func (cr *ChallengeRunner) extractScoring(result *FlowResult) (float64, string) {
	if len(result.Steps) == 0 {
		return 0, "no steps completed"
	}

	// Use the last step's response for scoring
	lastStep := result.Steps[len(result.Steps)-1]
	if lastStep.Error != nil {
		return 0, "last step failed: " + lastStep.Error.Error()
	}
	if lastStep.Response == nil {
		return 0, "no response from last step"
	}

	content := lastStep.Response.Content

	// Extract numeric score
	var score float64
	matches := scorePattern.FindAllStringSubmatch(content, -1)
	if len(matches) > 0 {
		// Use the last score found (typically the "overall" one)
		lastMatch := matches[len(matches)-1]
		if v, err := strconv.ParseFloat(lastMatch[1], 64); err == nil {
			score = v
		}
	}

	// Build summary: first 500 chars of last response
	summary := content
	if len(summary) > 500 {
		// Cut at sentence boundary
		if idx := strings.LastIndex(summary[:500], "."); idx > 200 {
			summary = summary[:idx+1]
		} else {
			summary = summary[:500] + "..."
		}
	}

	return score, summary
}

// buildModerationScore creates a moderation assessment from challenge results.
func (cr *ChallengeRunner) buildModerationScore(challenge *db.Challenge, result *FlowResult, overallScore float64) *db.ModerationScore {
	if len(result.Steps) == 0 {
		return nil
	}

	lastStep := result.Steps[len(result.Steps)-1]
	if lastStep.Response == nil {
		return nil
	}

	evaluator := "flow:" + challenge.FlowName
	if lastStep.Provider != "" {
		evaluator += "/" + lastStep.Provider
	}

	ms := &db.ModerationScore{
		ID:           db.NewID(),
		NodeID:       challenge.NodeID,
		Evaluator:    evaluator,
		EvalSource:   "challenge",
		OverallScore: &overallScore,
		Flags:        "[]",
		ChallengeID:  &challenge.ID,
	}

	// Extract dimension-specific scores from content
	content := lastStep.Response.Content
	if v := extractDimensionScore(content, "completeness", "factual"); v >= 0 {
		ms.FactualScore = &v
	}
	if v := extractDimensionScore(content, "source", "attribution"); v >= 0 {
		ms.SourceScore = &v
	}
	if v := extractDimensionScore(content, "argument", "reasoning", "balance"); v >= 0 {
		ms.ArgumentScore = &v
	}

	return ms
}

// extractDimensionScore finds a score for a named dimension in LLM output.
func extractDimensionScore(content string, keywords ...string) float64 {
	lower := strings.ToLower(content)
	for _, kw := range keywords {
		pattern := regexp.MustCompile(`(?i)` + kw + `[^:]*[:\s]+(\d+)`)
		if m := pattern.FindStringSubmatch(lower); len(m) > 1 {
			if v, err := strconv.ParseFloat(m[1], 64); err == nil {
				return v
			}
		}
	}
	return -1
}
