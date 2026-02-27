# config

Responsabilite: Chargement de la configuration TOML pour le serveur, la base de donnees, l'auth, les providers LLM, le bot, la federation et l'instance.
Depend de: `github.com/BurntSushi/toml`
Dependants: `main.go`, `internal/api`, `internal/llm`
Point d'entree: `config.go`
Types cles: `Config`, `ServerConfig`, `DatabaseConfig`, `AuthConfig`, `LLMConfig`, `BotConfig`, `FederationConfig`, `InstanceConfig`
Invariants: Config via TOML uniquement (exception dans l'ecosysteme HOROS, pas env vars). DefaultConfig() fournit des valeurs par defaut sensibles.
NE PAS: Convertir en env vars (choix delibere pour ce service). Ajouter des champs sans default dans DefaultConfig().
