// CLAUDE:SUMMARY User DB operations — create, lookup by ID/handle/email, reputation tracking, bot account support
package db

import (
	"database/sql"
	"fmt"
	"time"
)

type User struct {
	ID                   string     `json:"id"`
	Handle               string     `json:"handle"`
	Email                *string    `json:"email,omitempty"`
	Role                 string     `json:"role"`
	IsBot                bool       `json:"is_bot"`
	Reputation           int        `json:"reputation"`
	HonorRate            float64    `json:"honor_rate"`
	Credits              int        `json:"credits"`
	BountytreescoreTotal int        `json:"bountytreescore_total"`
	BountytreescoreTags  string     `json:"bountytreescore_tags"`
	CreatedAt            time.Time  `json:"created_at"`
	LastSeenAt           *time.Time `json:"last_seen_at,omitempty"`
}

type CreateUserInput struct {
	Handle       string
	Email        string
	PasswordHash string
	IsBot        bool
}

func (db *DB) CreateUser(input CreateUserInput) (*User, error) {
	id := NewID()
	var emailPtr *string
	if input.Email != "" {
		emailPtr = &input.Email
	}
	bot := 0
	if input.IsBot {
		bot = 1
	}
	_, err := db.Exec(`
		INSERT INTO users (id, handle, email, password_hash, is_bot)
		VALUES (?, ?, ?, ?, ?)`, id, input.Handle, emailPtr, input.PasswordHash, bot)
	if err != nil {
		return nil, fmt.Errorf("creating user: %w", err)
	}
	return &User{
		ID:     id,
		Handle: input.Handle,
		Email:  emailPtr,
		IsBot:  input.IsBot,
		Role:   "user",
	}, nil
}

// EnsureBotUser creates the bot user if it doesn't exist. Returns the user ID.
func (db *DB) EnsureBotUser(handle, passwordHash string) (string, error) {
	user, _, err := db.GetUserByHandle(handle)
	if err == nil {
		return user.ID, nil
	}
	if err != sql.ErrNoRows {
		return "", err
	}
	u, err := db.CreateUser(CreateUserInput{
		Handle:       handle,
		PasswordHash: passwordHash,
		IsBot:        true,
	})
	if err != nil {
		return "", err
	}
	return u.ID, nil
}

// AddCredits adds credits to a user's balance and logs the transaction.
func (db *DB) AddCredits(userID string, amount int, reason, refType, refID string) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	var balance int
	err = tx.QueryRow("SELECT credits FROM users WHERE id = ?", userID).Scan(&balance)
	if err != nil {
		return err
	}
	newBalance := balance + amount

	_, err = tx.Exec("UPDATE users SET credits = ? WHERE id = ?", newBalance, userID)
	if err != nil {
		return err
	}

	ledgerID := NewID()
	_, err = tx.Exec(`INSERT INTO credit_ledger (id, user_id, amount, balance, reason, ref_type, ref_id)
		VALUES (?, ?, ?, ?, ?, ?, ?)`, ledgerID, userID, amount, newBalance, reason, refType, refID)
	if err != nil {
		return err
	}

	return tx.Commit()
}

// DebitCredits removes credits; returns error if insufficient balance.
func (db *DB) DebitCredits(userID string, amount int, reason, refType, refID string) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	var balance int
	err = tx.QueryRow("SELECT credits FROM users WHERE id = ?", userID).Scan(&balance)
	if err != nil {
		return err
	}
	if balance < amount {
		return fmt.Errorf("insufficient credits: have %d, need %d", balance, amount)
	}
	newBalance := balance - amount

	_, err = tx.Exec("UPDATE users SET credits = ? WHERE id = ?", newBalance, userID)
	if err != nil {
		return err
	}

	ledgerID := NewID()
	_, err = tx.Exec(`INSERT INTO credit_ledger (id, user_id, amount, balance, reason, ref_type, ref_id)
		VALUES (?, ?, ?, ?, ?, ?, ?)`, ledgerID, userID, -amount, newBalance, reason, refType, refID)
	if err != nil {
		return err
	}

	return tx.Commit()
}

// IncrementViewCount bumps a node's view count.
func (db *DB) IncrementViewCount(nodeID string) {
	_, _ = db.Exec("UPDATE nodes SET view_count = view_count + 1 WHERE id = ?", nodeID)
}

func (db *DB) GetUserByHandle(handle string) (*User, string, error) {
	u := &User{}
	var email sql.NullString
	var lastSeen sql.NullTime
	var passwordHash string
	err := db.QueryRow(`
		SELECT id, handle, email, password_hash, role, is_bot, reputation, honor_rate, credits,
		       bountytreescore_total, bountytreescore_tags, created_at, last_seen_at
		FROM users WHERE handle = ?`, handle).Scan(
		&u.ID, &u.Handle, &email, &passwordHash, &u.Role, &u.IsBot, &u.Reputation,
		&u.HonorRate, &u.Credits, &u.BountytreescoreTotal, &u.BountytreescoreTags, &u.CreatedAt, &lastSeen)
	if err != nil {
		return nil, "", err
	}
	if email.Valid {
		u.Email = &email.String
	}
	if lastSeen.Valid {
		u.LastSeenAt = &lastSeen.Time
	}
	return u, passwordHash, nil
}

func (db *DB) GetUserByID(id string) (*User, error) {
	u := &User{}
	var email sql.NullString
	var lastSeen sql.NullTime
	err := db.QueryRow(`
		SELECT id, handle, email, role, is_bot, reputation, honor_rate, credits,
		       bountytreescore_total, bountytreescore_tags, created_at, last_seen_at
		FROM users WHERE id = ?`, id).Scan(
		&u.ID, &u.Handle, &email, &u.Role, &u.IsBot, &u.Reputation,
		&u.HonorRate, &u.Credits, &u.BountytreescoreTotal, &u.BountytreescoreTags, &u.CreatedAt, &lastSeen)
	if err != nil {
		return nil, err
	}
	if email.Valid {
		u.Email = &email.String
	}
	if lastSeen.Valid {
		u.LastSeenAt = &lastSeen.Time
	}
	return u, nil
}

// ListUsersByRole returns users with the specified role.
func (db *DB) ListUsersByRole(role string) ([]User, error) {
	rows, err := db.Query(`
		SELECT id, handle, email, role, is_bot, reputation, honor_rate, credits,
		       bountytreescore_total, bountytreescore_tags, created_at, last_seen_at
		FROM users WHERE role = ? ORDER BY handle`, role)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var users []User
	for rows.Next() {
		var u User
		var email sql.NullString
		var lastSeen sql.NullTime
		if err := rows.Scan(&u.ID, &u.Handle, &email, &u.Role, &u.IsBot, &u.Reputation,
			&u.HonorRate, &u.Credits, &u.BountytreescoreTotal, &u.BountytreescoreTags,
			&u.CreatedAt, &lastSeen); err != nil {
			return nil, err
		}
		if email.Valid {
			u.Email = &email.String
		}
		if lastSeen.Valid {
			u.LastSeenAt = &lastSeen.Time
		}
		users = append(users, u)
	}
	return users, rows.Err()
}

// TouchLastSeen updates the user's last_seen_at timestamp.
func (db *DB) TouchLastSeen(userID string) error {
	_, err := db.Exec("UPDATE users SET last_seen_at = datetime('now') WHERE id = ?", userID)
	return err
}
