package db

import "github.com/hazyhaar/pkg/idgen"

// NewID generates a 12-character base-36 ID using the canonical idgen package.
func NewID() string {
	return idgen.New()
}
