# api

Responsabilité: HTTP handlers REST pour toutes les opérations : auth, nodes CRUD, search, votes, tags, bounties, profils, federation, forensic, replay, workflows, rate limiting.
Dépend de: `internal/auth`, `internal/config`, `internal/db`, `internal/llm`
Dépendants: `main.go` (montage des routes)
Point d'entrée: `api.go` (struct API), `v1.go` (routes /api/v1/)
Types clés: `API`, `RateLimiter`, `rateBucket`
Invariants: Tout endpoint authentifié passe par `auth.ExtractClaims`. Le rate limiter est par IP. Les body sont limités a 200KB.
NE PAS: Exposer des endpoints sans rate limiting sur les writes. Oublier le prefix /api/v1/ dans v1.go quand on ajoute une route.
