package db

import (
	"database/sql"
	"fmt"
	"time"
)

type User struct {
	ID                   string    `json:"id"`
	Handle               string    `json:"handle"`
	Email                *string   `json:"email,omitempty"`
	Reputation           int       `json:"reputation"`
	BountytreescoreTotal int       `json:"bountytreescore_total"`
	BountytreescoreTags  string    `json:"bountytreescore_tags"`
	CreatedAt            time.Time `json:"created_at"`
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
	var passwordHash string
	err := db.QueryRow(`
		SELECT id, handle, email, password_hash, reputation, bountytreescore_total, bountytreescore_tags, created_at
		FROM users WHERE handle = ?`, handle).Scan(
		&u.ID, &u.Handle, &email, &passwordHash, &u.Reputation,
		&u.BountytreescoreTotal, &u.BountytreescoreTags, &u.CreatedAt)
	if err != nil {
		return nil, "", err
	}
	if email.Valid {
		u.Email = &email.String
	}
	return u, passwordHash, nil
}

func (db *DB) GetUserByID(id string) (*User, error) {
	u := &User{}
	var email sql.NullString
	err := db.QueryRow(`
		SELECT id, handle, email, reputation, bountytreescore_total, bountytreescore_tags, created_at
		FROM users WHERE id = ?`, id).Scan(
		&u.ID, &u.Handle, &email, &u.Reputation,
		&u.BountytreescoreTotal, &u.BountytreescoreTags, &u.CreatedAt)
	if err != nil {
		return nil, err
	}
	if email.Valid {
		u.Email = &email.String
	}
	return u, nil
}
