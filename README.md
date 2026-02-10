# horostracker

Proof-tree search engine. Open source. Decentralized-ready.

## Quick start

```bash
# Build from source (requires Go 1.21+ and CGO)
CGO_ENABLED=1 go build -tags fts5 -trimpath -ldflags="-s -w" -o horostracker .

# Run
./horostracker serve
```

The instance runs on `:8080`. SQLite database created automatically in `data/nodes.db`.

## Configure LLM API keys (optional)

```bash
cp config.example.toml config.toml
# Edit config.toml — add your API keys
./horostracker serve --config config.toml
```

Without API keys, the instance runs in human-only mode (no LLM responses).

## Build from source

```bash
git clone https://github.com/hazyhaar/horostracker
cd horostracker
CGO_ENABLED=1 go build -tags fts5 -trimpath -ldflags="-s -w" -o horostracker .
sha256sum horostracker
```

## Architecture

One binary. One SQLite. No external dependencies.
Federation-ready (`origin_instance` + `signature` on every node). Not activated by default. Each instance is autonomous.

## API

| Method | Endpoint | Description |
|--------|----------|-------------|
| POST | `/api/register` | Create account |
| POST | `/api/login` | Get JWT token |
| POST | `/api/ask` | Post a question |
| POST | `/api/answer` | Post answer/evidence/objection/etc. |
| GET | `/api/tree/{id}` | Get question tree |
| POST | `/api/search` | Full-text search |
| GET | `/api/questions` | Hot questions feed |
| GET | `/api/tags` | Popular tags |
| GET | `/api/bounties` | Active bounties |
| POST | `/api/bounty` | Create a bounty |
| POST | `/api/vote` | Upvote/downvote a node |
| POST | `/api/thank` | Thank a contributor |
| GET | `/api/user/{handle}` | User profile |
| GET | `/api/me` | Current user |

## License

AGPL-3.0
