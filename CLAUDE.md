# horostracker

Responsabilité: Plateforme d'observabilité distribuée — tracking d'événements, dashboards, replay de séquences pour l'écosystème HOROS.
Module: `github.com/hazyhaar/horostracker`
Repo: `github.com/hazyhaar/horostracker` (privé)
État: Actif, buildé (Feb 2026), pas encore déployé en production

## Index

| Fichier/Dir | Rôle |
|-------------|------|
| `main.go` | Entry point |
| `internal/` | Business logic (handlers, storage, config) |
| `static/` | Dashboard frontend |
| `e2e/` | Tests end-to-end |
| `config.toml` | Configuration TOML |

Dépend de: `github.com/hazyhaar/pkg` (MCP QUIC, observability)

## Build / Test

```bash
CGO_ENABLED=0 go build -o bin/horostracker .
go test ./... -count=1
```

## Invariants

- Config via **TOML** (exception HOROS — pas env vars)

## NE PAS

- Convertir en env vars (TOML est un choix délibéré pour ce service)
