# auth

Responsabilite: JWT authentication avec bcrypt password hashing, generation/validation de tokens, extraction de claims depuis les HTTP requests.
Depend de: `github.com/golang-jwt/jwt/v5`, `golang.org/x/crypto/bcrypt`
Dependants: `internal/api`
Point d'entree: `auth.go`
Types cles: `Auth`, `Claims`
Invariants: Le signing method est toujours HS256. Le secret JWT ne doit jamais etre expose. Les tokens ont une expiration configurable.
NE PAS: Utiliser un autre signing method que HMAC. Stocker le secret JWT en clair dans les logs.
