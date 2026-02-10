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
}

func (db *DB) CreateUser(input CreateUserInput) (*User, error) {
	id := NewID()
	var emailPtr *string
	if input.Email != "" {
		emailPtr = &input.Email
	}
	_, err := db.Exec(`
		INSERT INTO users (id, handle, email, password_hash)
		VALUES (?, ?, ?, ?)`, id, input.Handle, emailPtr, input.PasswordHash)
	if err != nil {
		return nil, fmt.Errorf("creating user: %w", err)
	}
	return &User{
		ID:     id,
		Handle: input.Handle,
		Email:  emailPtr,
	}, nil
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

// TouchLastSeen updates the user's last_seen_at timestamp.
func (db *DB) TouchLastSeen(userID string) error {
	_, err := db.Exec("UPDATE users SET last_seen_at = datetime('now') WHERE id = ?", userID)
	return err
}
