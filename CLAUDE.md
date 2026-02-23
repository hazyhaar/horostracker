# CLAUDE.md — horostracker

## Ce que c'est

Plateforme d'observabilité distribuée : tracking d'événements, dashboards, replay de séquences. Conçu pour superviser l'écosystème HOROS.

**Module** : `github.com/hazyhaar/horostracker`
**Repo** : `github.com/hazyhaar/horostracker` (privé)
**État** : Actif, buildé (Feb 2026), pas encore déployé en production

## Structure

```
horostracker/
├── main.go                # Entry point
├── internal/              # Business logic
├── static/                # Dashboard frontend
├── config.toml            # Configuration TOML (exception HOROS : pas env vars)
├── config.example.toml    # Template config
├── e2e/                   # Tests end-to-end
└── docs/                  # Documentation
```

## Build / Test

```bash
CGO_ENABLED=0 go build -o bin/horostracker .
go test ./... -count=1
```

## Particularités

- Utilise **TOML** pour la config (pas env vars — exception dans l'écosystème HOROS)
- Dépend de `github.com/hazyhaar/pkg` (MCP QUIC, observability)
- Binary pré-buildé présent (21M)
