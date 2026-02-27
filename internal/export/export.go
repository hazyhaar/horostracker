// CLAUDE:SUMMARY JSONL dataset export of proof trees with author anonymization and metadata
// Package export provides JSONL dataset export with anonymization.
package export

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/hazyhaar/horostracker/internal/db"
)

// TreeExport is a self-contained export of a proof tree for dataset consumption.
type TreeExport struct {
	ExportedAt string         `json:"exported_at"`
	Version    string         `json:"export_version"`
	Tree       ExportNode     `json:"tree"`
	Metadata   ExportMetadata `json:"metadata"`
}

// ExportNode is an anonymized node in the export.
type ExportNode struct {
	ID          string       `json:"id"`
	ParentID    *string      `json:"parent_id,omitempty"`
	NodeType    string       `json:"node_type"`
	Body        string       `json:"body"`
	AuthorID    string       `json:"author_id"` // anonymized
	ModelID     *string      `json:"model_id,omitempty"`
	Score       int          `json:"score"`
	Temperature string       `json:"temperature"`
	Status      string       `json:"status"`
	Depth       int          `json:"depth"`
	CreatedAt   time.Time    `json:"created_at"`
	Tags        []string     `json:"tags,omitempty"`
	Sources     []ExportSource `json:"sources,omitempty"`
	Children    []ExportNode `json:"children,omitempty"`
}

// ExportSource is a source reference in the export.
type ExportSource struct {
	URL         string  `json:"url"`
	Title       string  `json:"title,omitempty"`
	Domain      string  `json:"domain,omitempty"`
	TrustScore  float64 `json:"trust_score"`
}

// ExportMetadata carries tree-level metadata.
type ExportMetadata struct {
	RootID      string   `json:"root_id"`
	TotalNodes  int      `json:"total_nodes"`
	MaxDepth    int      `json:"max_depth"`
	Tags        []string `json:"tags"`
	Temperature string   `json:"temperature"`
	HasBounty   bool     `json:"has_bounty"`
}

// CorrectedGarbageSet is a structured demolition of a false claim.
type CorrectedGarbageSet struct {
	ExportedAt       string                `json:"exported_at"`
	Version          string                `json:"export_version"`
	OriginalClaim    string                `json:"original_claim"`
	ClaimFormulations []string             `json:"claim_formulations"`
	WhyCredible      string                `json:"why_credible"`
	DemolitionTree   ExportNode            `json:"demolition_tree"`
	DeceptionMechanisms []string           `json:"deception_mechanisms"`
	Resolution       *string               `json:"resolution,omitempty"`
	Metadata         CorrectedGSMetadata   `json:"metadata"`
}

// CorrectedGSMetadata carries metadata for a corrected garbage set.
type CorrectedGSMetadata struct {
	TotalNodes       int    `json:"total_nodes"`
	ObjectionCount   int    `json:"objection_count"`
	EvidenceCount    int    `json:"evidence_count"`
	AdversarialRounds int   `json:"adversarial_rounds"`
	Temperature      string `json:"temperature"`
}

// Exporter produces JSONL exports from the database.
type Exporter struct {
	database *db.DB
}

// NewExporter creates a dataset exporter.
func NewExporter(database *db.DB) *Exporter {
	return &Exporter{database: database}
}

// ExportTree writes a single tree as a JSON object (one line in JSONL).
func (e *Exporter) ExportTree(w io.Writer, rootID string) error {
	tree, err := e.database.GetTree(rootID, 100)
	if err != nil {
		return fmt.Errorf("getting tree: %w", err)
	}

	// Create anonymization map for this export
	anonMap := newAnonMap()
	tags, _ := e.database.GetTagsForNode(rootID)

	export := TreeExport{
		ExportedAt: time.Now().UTC().Format(time.RFC3339),
		Version:    "1.0",
		Tree:       anonymizeNode(tree, anonMap, e.database),
		Metadata: ExportMetadata{
			RootID:     rootID,
			TotalNodes: countNodes(tree),
			MaxDepth:   maxDepth(tree, 0),
			Tags:       tags,
			Temperature: tree.Temperature,
		},
	}

	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	return enc.Encode(export)
}

// ExportCorrectedGarbageSet writes a corrected garbage set for a demolished claim.
func (e *Exporter) ExportCorrectedGarbageSet(w io.Writer, rootID string, resolution *string) error {
	tree, err := e.database.GetTree(rootID, 100)
	if err != nil {
		return fmt.Errorf("getting tree: %w", err)
	}

	anonMap := newAnonMap()

	var objCount, evCount int
	countTypes(tree, &objCount, &evCount)

	// Query real adversarial challenge count from DB
	advCount, _ := e.database.CountChallengesForNode(rootID)

	cgs := CorrectedGarbageSet{
		ExportedAt:    time.Now().UTC().Format(time.RFC3339),
		Version:       "1.0",
		OriginalClaim: tree.Body,
		DemolitionTree: anonymizeNode(tree, anonMap, e.database),
		Resolution:    resolution,
		Metadata: CorrectedGSMetadata{
			TotalNodes:        countNodes(tree),
			ObjectionCount:    objCount,
			EvidenceCount:     evCount,
			AdversarialRounds: advCount,
			Temperature:       tree.Temperature,
		},
	}

	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	return enc.Encode(cgs)
}

// ExportAllTrees writes all root questions as JSONL (one tree per line).
func (e *Exporter) ExportAllTrees(w io.Writer) error {
	questions, err := e.database.GetHotQuestions(1000)
	if err != nil {
		return err
	}
	for _, q := range questions {
		if err := e.ExportTree(w, q.ID); err != nil {
			return err
		}
	}
	return nil
}

// anonymizeNode converts a db.Node tree to an export tree with anonymized author IDs.
func anonymizeNode(node *db.Node, anonMap *anonMap, database *db.DB) ExportNode {
	en := ExportNode{
		ID:          node.ID,
		ParentID:    node.ParentID,
		NodeType:    node.NodeType,
		Body:        node.Body,
		AuthorID:    anonMap.get(node.AuthorID),
		ModelID:     node.ModelID,
		Score:       node.Score,
		Temperature: node.Temperature,
		Status:      node.Status,
		Depth:       node.Depth,
		CreatedAt:   node.CreatedAt,
	}

	// Add tags
	if tags, err := database.GetTagsForNode(node.ID); err == nil && len(tags) > 0 {
		en.Tags = tags
	}

	// Recurse children
	for _, child := range node.Children {
		en.Children = append(en.Children, anonymizeNode(child, anonMap, database))
	}

	return en
}

func countNodes(node *db.Node) int {
	n := 1
	for _, child := range node.Children {
		n += countNodes(child)
	}
	return n
}

func maxDepth(node *db.Node, current int) int {
	max := current
	for _, child := range node.Children {
		d := maxDepth(child, current+1)
		if d > max {
			max = d
		}
	}
	return max
}

func countTypes(node *db.Node, objections, evidence *int) {
	switch node.NodeType {
	case "objection":
		*objections++
	case "evidence":
		*evidence++
	}
	for _, child := range node.Children {
		countTypes(child, objections, evidence)
	}
}

// anonMap maps real author IDs to randomized stable IDs within one export.
type anonMap struct {
	mapping map[string]string
	salt    string
}

func newAnonMap() *anonMap {
	salt := make([]byte, 16)
	rand.Read(salt)
	return &anonMap{
		mapping: make(map[string]string),
		salt:    hex.EncodeToString(salt),
	}
}

func (m *anonMap) get(realID string) string {
	if anon, ok := m.mapping[realID]; ok {
		return anon
	}
	hash := sha256.Sum256([]byte(m.salt + realID))
	anon := "anon_" + hex.EncodeToString(hash[:6])
	m.mapping[realID] = anon
	return anon
}

