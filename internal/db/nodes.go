package db

import (
	"database/sql"
	"fmt"
	"regexp"
	"strings"
	"time"
)

type Node struct {
	ID             string    `json:"id"`
	ParentID       *string   `json:"parent_id,omitempty"`
	RootID         string    `json:"root_id"`
	Slug           *string   `json:"slug,omitempty"`
	NodeType       string    `json:"node_type"`
	Body           string    `json:"body"`
	AuthorID       string    `json:"author_id"`
	ModelID        *string   `json:"model_id,omitempty"`
	Score          int       `json:"score"`
	Temperature    string    `json:"temperature"`
	Status         string    `json:"status"`
	Metadata       string    `json:"metadata"`
	IsAccepted     bool      `json:"is_accepted"`
	IsCritical     bool      `json:"is_critical"`
	ChildCount     int       `json:"child_count"`
	ViewCount      int       `json:"view_count"`
	Depth          int       `json:"depth"`
	OriginInstance string    `json:"origin_instance"`
	Signature      string    `json:"signature"`
	BinaryHash     string    `json:"binary_hash"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
	Children       []*Node   `json:"children,omitempty"`
}

// nodeColumns is the standard SELECT column list for nodes.
const nodeColumns = `id, parent_id, root_id, slug, node_type, body, author_id, model_id,
	score, temperature, status, metadata, is_accepted, is_critical, child_count,
	view_count, depth, origin_instance, signature, binary_hash, created_at, updated_at`

// nodeColumnsQualified returns nodeColumns with table alias prefix (e.g. "n.id, n.parent_id, ...").
func nodeColumnsQualified(alias string) string {
	return alias + `.id, ` + alias + `.parent_id, ` + alias + `.root_id, ` + alias + `.slug, ` + alias + `.node_type, ` + alias + `.body, ` + alias + `.author_id, ` + alias + `.model_id,
	` + alias + `.score, ` + alias + `.temperature, ` + alias + `.status, ` + alias + `.metadata, ` + alias + `.is_accepted, ` + alias + `.is_critical, ` + alias + `.child_count,
	` + alias + `.view_count, ` + alias + `.depth, ` + alias + `.origin_instance, ` + alias + `.signature, ` + alias + `.binary_hash, ` + alias + `.created_at, ` + alias + `.updated_at`
}

var slugRe = regexp.MustCompile(`[^a-z0-9]+`)

// makeSlug generates a URL-friendly slug from text body + short unique suffix.
func makeSlug(body, id string) string {
	s := strings.ToLower(body)
	s = slugRe.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	if len(s) > 60 {
		// Cut at word boundary
		if i := strings.LastIndex(s[:60], "-"); i > 20 {
			s = s[:i]
		} else {
			s = s[:60]
		}
	}
	if s == "" {
		return id
	}
	// Append short ID suffix for uniqueness
	suffix := id
	if len(suffix) > 6 {
		suffix = suffix[:6]
	}
	return s + "-" + suffix
}

// scanNodeRows scans all rows into a slice of Node pointers.
func scanNodeRows(rows *sql.Rows) ([]*Node, error) {
	var results []*Node
	for rows.Next() {
		n, err := scanNode(rows)
		if err != nil {
			return nil, err
		}
		results = append(results, n)
	}
	return results, nil
}

// scanNode scans a node row into a Node struct. The row must match nodeColumns.
func scanNode(s interface{ Scan(...any) error }) (*Node, error) {
	n := &Node{}
	var parentID, slug, modelID sql.NullString
	err := s.Scan(
		&n.ID, &parentID, &n.RootID, &slug, &n.NodeType, &n.Body, &n.AuthorID, &modelID,
		&n.Score, &n.Temperature, &n.Status, &n.Metadata, &n.IsAccepted, &n.IsCritical, &n.ChildCount,
		&n.ViewCount, &n.Depth, &n.OriginInstance, &n.Signature, &n.BinaryHash, &n.CreatedAt, &n.UpdatedAt)
	if err != nil {
		return nil, err
	}
	if parentID.Valid {
		n.ParentID = &parentID.String
	}
	if slug.Valid {
		n.Slug = &slug.String
	}
	if modelID.Valid {
		n.ModelID = &modelID.String
	}
	return n, nil
}

type CreateNodeInput struct {
	ParentID *string  `json:"parent_id"`
	NodeType string   `json:"node_type"`
	Body     string   `json:"body"`
	AuthorID string   `json:"author_id"`
	ModelID  *string  `json:"model_id"`
	Metadata string   `json:"metadata"`
	Tags     []string `json:"tags"`
}

func (db *DB) CreateNode(input CreateNodeInput) (*Node, error) {
	id := NewID()

	var rootID string
	var depth int

	if input.ParentID != nil && *input.ParentID != "" {
		var parentRoot string
		var parentDepth int
		err := db.QueryRow("SELECT root_id, depth FROM nodes WHERE id = ?", *input.ParentID).Scan(&parentRoot, &parentDepth)
		if err != nil {
			return nil, fmt.Errorf("parent not found: %w", err)
		}
		rootID = parentRoot
		depth = parentDepth + 1
	} else {
		rootID = id
		depth = 0
	}

	if input.Metadata == "" {
		input.Metadata = "{}"
	}

	// Generate slug for question nodes (root-level)
	var slug *string
	if input.NodeType == "question" && (input.ParentID == nil || *input.ParentID == "") {
		s := makeSlug(input.Body, id)
		slug = &s
	}

	tx, err := db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	_, err = tx.Exec(`
		INSERT INTO nodes (id, parent_id, root_id, slug, node_type, body, author_id, model_id, metadata, depth, origin_instance)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 'local')`,
		id, input.ParentID, rootID, slug, input.NodeType, input.Body, input.AuthorID, input.ModelID, input.Metadata, depth)
	if err != nil {
		return nil, fmt.Errorf("inserting node: %w", err)
	}

	if input.ParentID != nil && *input.ParentID != "" {
		_, err = tx.Exec("UPDATE nodes SET child_count = child_count + 1, updated_at = datetime('now') WHERE id = ?", *input.ParentID)
		if err != nil {
			return nil, fmt.Errorf("updating parent child_count: %w", err)
		}
	}

	for _, tag := range input.Tags {
		_, err = tx.Exec("INSERT OR IGNORE INTO tags (node_id, tag) VALUES (?, ?)", id, tag)
		if err != nil {
			return nil, fmt.Errorf("inserting tag: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}

	return db.GetNode(id)
}

func (db *DB) GetNode(id string) (*Node, error) {
	return scanNode(db.QueryRow(`SELECT `+nodeColumns+` FROM nodes WHERE id = ?`, id))
}

// GetNodeBySlug returns a node by its URL slug.
func (db *DB) GetNodeBySlug(slug string) (*Node, error) {
	return scanNode(db.QueryRow(`SELECT `+nodeColumns+` FROM nodes WHERE slug = ?`, slug))
}

func (db *DB) GetTree(nodeID string, maxDepth int) (*Node, error) {
	root, err := db.GetNode(nodeID)
	if err != nil {
		return nil, err
	}

	rows, err := db.Query(`
		WITH RECURSIVE tree AS (
			SELECT `+nodeColumns+`, 0 as rel_depth
			FROM nodes WHERE id = ?
			UNION ALL
			SELECT n.id, n.parent_id, n.root_id, n.slug, n.node_type, n.body, n.author_id, n.model_id,
			       n.score, n.temperature, n.status, n.metadata, n.is_accepted, n.is_critical, n.child_count,
			       n.view_count, n.depth, n.origin_instance, n.signature, n.binary_hash, n.created_at, n.updated_at,
			       t.rel_depth + 1
			FROM nodes n JOIN tree t ON n.parent_id = t.id
			WHERE t.rel_depth < ?
		)
		SELECT `+nodeColumns+`
		FROM tree
		ORDER BY depth ASC, score DESC`, nodeID, maxDepth)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	nodeMap := make(map[string]*Node)
	nodeMap[root.ID] = root

	for rows.Next() {
		n, err := scanNode(rows)
		if err != nil {
			return nil, err
		}
		if n.ID == root.ID {
			continue
		}
		nodeMap[n.ID] = n
		if n.ParentID != nil {
			if parent, ok := nodeMap[*n.ParentID]; ok {
				parent.Children = append(parent.Children, n)
			}
		}
	}

	return root, nil
}

func (db *DB) Vote(userID, nodeID string, value int) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var existing int
	err = tx.QueryRow("SELECT value FROM votes WHERE user_id = ? AND node_id = ?", userID, nodeID).Scan(&existing)
	if err == nil {
		// Update existing vote
		if existing == value {
			return nil // same vote, no-op
		}
		_, err = tx.Exec("UPDATE votes SET value = ?, created_at = datetime('now') WHERE user_id = ? AND node_id = ?", value, userID, nodeID)
		if err != nil {
			return err
		}
		diff := value - existing
		_, err = tx.Exec("UPDATE nodes SET score = score + ?, updated_at = datetime('now') WHERE id = ?", diff, nodeID)
	} else if err == sql.ErrNoRows {
		_, err = tx.Exec("INSERT INTO votes (user_id, node_id, value) VALUES (?, ?, ?)", userID, nodeID, value)
		if err != nil {
			return err
		}
		_, err = tx.Exec("UPDATE nodes SET score = score + ?, updated_at = datetime('now') WHERE id = ?", value, nodeID)
	} else {
		return err
	}
	if err != nil {
		return err
	}
	return tx.Commit()
}

func (db *DB) Thank(fromUser, toNode, message string) error {
	if len(message) > 140 {
		message = message[:140]
	}
	_, err := db.Exec("INSERT OR IGNORE INTO thanks (from_user, to_node, message) VALUES (?, ?, ?)", fromUser, toNode, message)
	return err
}

func (db *DB) SearchNodes(query string, limit int) ([]*Node, error) {
	if limit <= 0 {
		limit = 20
	}
	rows, err := db.Query(`
		SELECT n.`+nodeColumnsQualified("n")+`
		FROM nodes_fts fts
		JOIN nodes n ON n.rowid = fts.rowid
		WHERE nodes_fts MATCH ?
		ORDER BY rank
		LIMIT ?`, query, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanNodeRows(rows)
}

func (db *DB) GetNodesByRoot(rootID string) ([]*Node, error) {
	rows, err := db.Query(`SELECT `+nodeColumns+` FROM nodes WHERE root_id = ? ORDER BY depth ASC, score DESC`, rootID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanNodeRows(rows)
}

func (db *DB) GetHotQuestions(limit int) ([]*Node, error) {
	if limit <= 0 {
		limit = 20
	}
	rows, err := db.Query(`
		SELECT `+nodeColumns+`
		FROM nodes
		WHERE node_type = 'question' AND parent_id IS NULL
		ORDER BY
			CASE temperature
				WHEN 'critical' THEN 4
				WHEN 'hot' THEN 3
				WHEN 'warm' THEN 2
				ELSE 1
			END DESC,
			score DESC,
			created_at DESC
		LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanNodeRows(rows)
}

func (db *DB) GetTagsForNode(nodeID string) ([]string, error) {
	rows, err := db.Query("SELECT tag FROM tags WHERE node_id = ?", nodeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tags []string
	for rows.Next() {
		var tag string
		if err := rows.Scan(&tag); err != nil {
			return nil, err
		}
		tags = append(tags, tag)
	}
	return tags, nil
}

func (db *DB) GetPopularTags(limit int) ([]TagCount, error) {
	if limit <= 0 {
		limit = 30
	}
	rows, err := db.Query(`
		SELECT tag, COUNT(*) as cnt
		FROM tags
		GROUP BY tag
		ORDER BY cnt DESC
		LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []TagCount
	for rows.Next() {
		var tc TagCount
		if err := rows.Scan(&tc.Tag, &tc.Count); err != nil {
			return nil, err
		}
		results = append(results, tc)
	}
	return results, nil
}

type TagCount struct {
	Tag   string `json:"tag"`
	Count int    `json:"count"`
}

// GetBounties returns active bounties, optionally filtered by tag.
func (db *DB) GetBounties(tag string, limit int) ([]Bounty, error) {
	if limit <= 0 {
		limit = 20
	}

	var rows *sql.Rows
	var err error

	if tag != "" {
		rows, err = db.Query(`
			SELECT b.id, b.node_id, b.sponsor_id, b.amount, b.currency, b.status,
			       b.winner_id, b.expires_at, b.psp_ref, b.created_at
			FROM bounties b
			JOIN tags t ON t.node_id = b.node_id
			WHERE b.status = 'active' AND t.tag = ?
			ORDER BY b.amount DESC
			LIMIT ?`, tag, limit)
	} else {
		rows, err = db.Query(`
			SELECT id, node_id, sponsor_id, amount, currency, status,
			       winner_id, expires_at, psp_ref, created_at
			FROM bounties
			WHERE status = 'active'
			ORDER BY amount DESC
			LIMIT ?`, limit)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []Bounty
	for rows.Next() {
		var b Bounty
		var winnerID, expiresAt, pspRef sql.NullString
		if err := rows.Scan(&b.ID, &b.NodeID, &b.SponsorID, &b.Amount, &b.Currency, &b.Status,
			&winnerID, &expiresAt, &pspRef, &b.CreatedAt); err != nil {
			return nil, err
		}
		if winnerID.Valid {
			b.WinnerID = &winnerID.String
		}
		if expiresAt.Valid {
			b.ExpiresAt = &expiresAt.String
		}
		if pspRef.Valid {
			b.PSPRef = &pspRef.String
		}
		results = append(results, b)
	}
	return results, nil
}

type Bounty struct {
	ID        string  `json:"id"`
	NodeID    string  `json:"node_id"`
	SponsorID string  `json:"sponsor_id"`
	Amount    int     `json:"amount"`
	Currency  string  `json:"currency"`
	Status    string  `json:"status"`
	WinnerID  *string `json:"winner_id,omitempty"`
	ExpiresAt *string `json:"expires_at,omitempty"`
	PSPRef    *string `json:"psp_ref,omitempty"`
	CreatedAt string  `json:"created_at"`
}

func (db *DB) CreateBounty(nodeID, sponsorID string, amount int) (*Bounty, error) {
	id := NewID()
	_, err := db.Exec(`
		INSERT INTO bounties (id, node_id, sponsor_id, amount, currency, status)
		VALUES (?, ?, ?, ?, 'credits', 'active')`, id, nodeID, sponsorID, amount)
	if err != nil {
		return nil, err
	}
	return &Bounty{
		ID:        id,
		NodeID:    nodeID,
		SponsorID: sponsorID,
		Amount:    amount,
		Currency:  "credits",
		Status:    "active",
	}, nil
}

// GetNodeThanks returns thanks for a given node
func (db *DB) GetNodeThanks(nodeID string) ([]ThankEntry, error) {
	rows, err := db.Query("SELECT from_user, to_node, message, created_at FROM thanks WHERE to_node = ? ORDER BY created_at DESC", nodeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []ThankEntry
	for rows.Next() {
		var t ThankEntry
		var msg sql.NullString
		if err := rows.Scan(&t.FromUser, &t.ToNode, &msg, &t.CreatedAt); err != nil {
			return nil, err
		}
		if msg.Valid {
			t.Message = msg.String
		}
		results = append(results, t)
	}
	return results, nil
}

type ThankEntry struct {
	FromUser  string `json:"from_user"`
	ToNode    string `json:"to_node"`
	Message   string `json:"message,omitempty"`
	CreatedAt string `json:"created_at"`
}
