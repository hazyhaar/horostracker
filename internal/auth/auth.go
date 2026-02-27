// CLAUDE:SUMMARY JWT authentication — password hashing (bcrypt), token generation/validation, claims extraction from HTTP requests
package auth

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
)

type Auth struct {
	secret []byte
	expiry time.Duration
}

type Claims struct {
	UserID string `json:"user_id"`
	Handle string `json:"handle"`
	jwt.RegisteredClaims
}

func New(secret string, expiryMinutes int) *Auth {
	return &Auth{
		secret: []byte(secret),
		expiry: time.Duration(expiryMinutes) * time.Minute,
	}
}

func (a *Auth) HashPassword(password string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return string(hash), nil
}

func (a *Auth) CheckPassword(hash, password string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}

func (a *Auth) GenerateToken(userID, handle string) (string, error) {
	claims := Claims{
		UserID: userID,
		Handle: handle,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(a.expiry)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(a.secret)
}

func (a *Auth) ValidateToken(tokenStr string) (*Claims, error) {
	token, err := jwt.ParseWithClaims(tokenStr, &Claims{}, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return a.secret, nil
	})
	if err != nil {
		return nil, err
	}
	claims, ok := token.Claims.(*Claims)
	if !ok || !token.Valid {
		return nil, fmt.Errorf("invalid token")
	}
	return claims, nil
}

// ExtractClaims reads the JWT from the Authorization header (Bearer token).
// Returns nil if no valid token is present (for public endpoints).
func (a *Auth) ExtractClaims(r *http.Request) *Claims {
	header := r.Header.Get("Authorization")
	if header == "" {
		return nil
	}
	parts := strings.SplitN(header, " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "bearer") {
		return nil
	}
	claims, err := a.ValidateToken(parts[1])
	if err != nil {
		return nil
	}
	return claims
}
