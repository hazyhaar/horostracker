# llm

Responsabilite: Client LLM multi-provider avec fallback chain, flow engine (attacker/defender/judge), resolution engine, challenge runner, replay engine, workflow engine, model discovery, et cost tracking.
Depend de: `internal/config`, `internal/db`
Dependants: `internal/api`, `internal/mcp`
Point d'entree: `client.go` (Client), `providers.go` (NewFromConfig)
Types cles: `Client`, `Provider`, `Request`, `Response`, `FlowEngine`, `FlowStep`, `ResolutionEngine`, `ChallengeRunner`, `ReplayEngine`, `WorkflowEngine`, `ModelDiscovery`
Invariants: Le fallback chain essaie chaque provider dans l'ordre configure. Le model format "provider/model" route directement au provider. Seuls les providers avec API keys configurees sont actives.
NE PAS: Ajouter un provider sans implementer l'interface Provider. Ignorer les erreurs du fallback chain (toutes doivent etre loguees). Hardcoder des API keys.
