# mcp

Responsabilite: Serveur MCP enregistrant les outils core horostracker (ask, answer, vote, search, bounties, tree, tags) accessibles via QUIC.
Depend de: `internal/db`, `github.com/hazyhaar/pkg/audit`, `github.com/hazyhaar/pkg/kit`, `github.com/modelcontextprotocol/go-sdk/mcp`
Dependants: `main.go`
Point d'entree: `server.go` (NewServer)
Types cles: pas de types publics, fonctions register* internes
Invariants: Les tools qui modifient des donnees passent par audit.Middleware. Les arguments MCP sont deserialises via decodeArgs (json.RawMessage -> map[string]any).
NE PAS: Enregistrer un outil write sans audit middleware. Utiliser des types custom pour les arguments MCP (rester sur map[string]any).
