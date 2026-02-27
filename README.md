# horostracker

Proof-tree search engine. Single binary. Three SQLites. Federation-ready.

horostracker transforms propositions into structured argumentation trees built from two primitives: **pieces** (factual material — documents, data, citations) and **claims** (propositions — affirmations, objections, syntheses). LLM flows stress-test claims through multi-model confrontation, red-teaming, and adversarial detection. The result: **Resolutions** (structured dialogues between argumentative lines) and **corrected garbage sets** (demolished false claims with deception classification) — high-value datasets for LLM training.

## Quick start

```bash
# Build (pure Go, no CGO required)
CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o horostracker .

# Run
./horostracker serve
```

The instance starts on `:8080` (TCP + UDP). Three SQLite databases are created automatically in `data/`.

```bash
# With config file
cp config.example.toml config.toml
./horostracker serve --config config.toml

# Override listen address
./horostracker serve --addr :9090
```

Without LLM API keys, the instance runs in **human-only mode** — all core features work, but Resolution generation, bot answers, and adversarial challenges require at least one provider.

## Architecture

```
horostracker (single binary, ~13 MB)
├── TCP :8080 ─── HTTP/1.1 + HTTP/2 (TLS)
│   ├── REST API (39 endpoints)
│   └── Static SPA (vanilla HTML/JS/CSS)
└── UDP :8080 ─── QUIC
    ├── HTTP/3
    └── MCP-over-QUIC (ALPN: horos-mcp-v1)

data/
├── nodes.db    ─── Core data (nodes, users, votes, bounties, challenges, ...)
├── flows.db    ─── LLM forensic traces (every prompt/response persisted)
└── metrics.db  ─── HTTP, MCP, and LLM call metrics
```

### Key design choices

- **Pure Go SQLite** via `modernc.org/sqlite` — no CGO, FTS5 included natively, `FROM scratch` Docker
- **Single binary** — no external services, no Redis, no Postgres
- **Three databases** — separation of concerns (core data vs. LLM traces vs. metrics)
- **Dual transport** — TCP and UDP on the same port, ALPN demux for MCP
- **Federation-ready** — `origin_instance`, `signature`, `binary_hash` on every node. Disabled by default

### Source layout

```
main.go                     CLI entry point (serve, version, help)
internal/
  config/                   TOML config loading with defaults
  db/                       SQLite layer (schema, nodes, users, challenges, nanoid)
    schema.go               22 tables, FTS5, triggers, indexes
    nodes.go                Node CRUD, tree CTE, FTS5 search, slug generation
    users.go                User management, bot creation, credit economy
    challenges.go           Adversarial challenge lifecycle, temperature calc
    flows.go                flows.db — LLM forensic traces
    metrics.go              metrics.db — HTTP/MCP/LLM metrics
  auth/                     bcrypt + JWT (HS256)
  api/                      HTTP handlers (Go 1.22+ method routing)
    api.go                  Core routes (auth, nodes, votes, bounties, users)
    resolution.go           Resolution generation + multi-format rendering
    export.go               JSONL dataset export with anonymization
    challenges.go           Adversarial challenge endpoints + leaderboard
    bot.go                  @horostracker bot auto-answer
    federation.go           Federation identity, status, content hashing
  llm/                      Multi-provider LLM client
    client.go               Provider-agnostic client with fallback chain
    openai.go               OpenAI-compatible provider (8+ services)
    anthropic.go            Anthropic Messages API
    gemini.go               Google Gemini API
    providers.go            Auto-configuration from API keys
    flow.go                 Thinking flow engine with template rendering
    flows_core.go           5 built-in adversarial flows
    resolution.go           Resolution generator + multi-format renderer
    challenge.go            ChallengeRunner (flow → score → moderation)
  mcp/                      MCP server (9 core tools) + seed (6 dynamic)
  export/                   JSONL exporter with anonymization
pkg/
  chassis/                  Unified TCP+QUIC server
  mcpquic/                  MCP-over-QUIC protocol
  mcprt/                    MCP tool registry with hot-reload
  trace/                    SQL query tracing (async persistence)
  audit/                    Audit logging (async batch flush)
  kit/                      Endpoint/transport abstractions
static/                     Vanilla HTML/JS/CSS SPA
```

## Data model

### Node types

Every content unit is a **node** in a proof tree. The ontology uses two primitives only:

| Type | Description |
|------|-------------|
| `piece` | Factual material — document, extract, data, testimony, URL, citation. Anchors claims to verifiable sources. |
| `claim` | Proposition — affirmation, objection, nuance, synthesis. Always linked to a parent piece or claim. |

Bot-generated responses are claims with `model_id` set. Resolutions are claims with `{"is_resolution": true}` in metadata. The rhetorical function (support, attack, nuance) emerges from the score and tree position, not from the type.

### Temperature

Nodes have a dynamic temperature reflecting controversy level:

| Temperature | Trigger |
|-------------|---------|
| `cold` | Default — low activity |
| `warm` | 3+ children OR 5+ votes OR any opposing claim (negative score) |
| `hot` | 5+ children AND 10+ votes, OR completed challenge |
| `critical` | 10+ children AND 20+ votes, OR 3+ challenges |

Temperature is recalculated automatically on relevant events (new children, votes, challenges).

### Dual economy

- **Humans** use honor-based promises (`honor_rate` as escrow reputation)
- **Bots** use credits (`credit_ledger` with full audit trail)

## API reference

### Authentication

| Method | Endpoint | Auth | Description |
|--------|----------|------|-------------|
| POST | `/api/register` | - | Create account (handle, email, password) |
| POST | `/api/login` | - | Get JWT token |

### Nodes

| Method | Endpoint | Auth | Description |
|--------|----------|------|-------------|
| POST | `/api/ask` | JWT | Create root claim (body, tags). With LLM: decomposes into thesis/antithesis claims. |
| POST | `/api/answer` | JWT | Add child node (parent_id, body, node_type: `piece` or `claim`) |
| GET | `/api/tree/{id}` | - | Get proof tree (optional `?depth=N`) |
| GET | `/api/node/{id}` | - | Get single node |
| GET | `/api/q/{slug}` | - | Get node by URL slug |
| POST | `/api/search` | - | Full-text search (FTS5) |
| GET | `/api/questions` | - | Hot questions feed |

### Social

| Method | Endpoint | Auth | Description |
|--------|----------|------|-------------|
| POST | `/api/vote` | JWT | Vote +1 or -1 on a node |
| POST | `/api/thank` | JWT | Thank a contributor |
| GET | `/api/tags` | - | Popular tags with counts |

### Bounties

| Method | Endpoint | Auth | Description |
|--------|----------|------|-------------|
| GET | `/api/bounties` | - | Active bounties (optional `?tag=`) |
| POST | `/api/bounty` | JWT | Create bounty (node_id, amount) |

### Users

| Method | Endpoint | Auth | Description |
|--------|----------|------|-------------|
| GET | `/api/user/{handle}` | - | Public profile |
| GET | `/api/me` | JWT | Current user profile |

### Resolution

| Method | Endpoint | Auth | Description |
|--------|----------|------|-------------|
| POST | `/api/resolution/{id}` | JWT | Generate Resolution from tree via LLM |
| GET | `/api/resolution/{id}` | - | List resolutions for a tree |
| POST | `/api/render/{id}` | JWT | Render resolution to format (article/faq/thread/summary) |
| GET | `/api/renders/{id}` | - | List renders for a resolution |

### Adversarial challenges

| Method | Endpoint | Auth | Description |
|--------|----------|------|-------------|
| POST | `/api/challenge/{nodeID}` | JWT | Submit adversarial challenge (flow_name, target_provider/model) |
| POST | `/api/challenge/{id}/run` | JWT | Execute challenge flow |
| GET | `/api/challenges/{nodeID}` | - | List challenges for a node |
| GET | `/api/challenge/{id}` | - | Get challenge details + results |
| GET | `/api/moderation/{nodeID}` | - | Multi-criteria moderation scores |
| GET | `/api/leaderboard/adversarial` | - | Challenge leaderboard |
| GET | `/api/flows` | - | List available thinking flows |

### Bot

| Method | Endpoint | Auth | Description |
|--------|----------|------|-------------|
| POST | `/api/bot/answer/{nodeID}` | JWT | Trigger bot LLM answer (debits credits) |
| GET | `/api/bot/status` | - | Bot status (enabled, credits, LLM availability) |

### Dataset export

| Method | Endpoint | Auth | Description |
|--------|----------|------|-------------|
| GET | `/api/export/tree/{id}` | - | Export tree as JSONL |
| GET | `/api/export/garbage/{id}` | - | Export corrected garbage set as JSONL |
| GET | `/api/export/all` | - | Export all trees as JSONL |

### Federation

| Method | Endpoint | Auth | Description |
|--------|----------|------|-------------|
| GET | `/api/federation/identity` | - | Instance identity (protocol handshake) |
| GET | `/api/federation/status` | - | Federation status (peer count, node counts) |
| GET | `/api/node/{id}/hash` | - | Content-addressable SHA-256 hash |

## Thinking flows

Five built-in adversarial flows stress-test claims across multiple LLM providers:

| Flow | Steps | Purpose |
|------|-------|---------|
| `confrontation` | respond → object → synthesize → judge | Multi-model debate on a claim |
| `red_team` | build_case → demolish → classify | Build then demolish an argument, classify deception |
| `fidelity_benchmark` | generate_resolution → evaluate_fidelity | Compare Resolution quality against source |
| `adversarial_detection` | fabricate → detect → score | Create fake source, test detection |
| `deep_dive` | initial_analysis → deepen → consolidate | Iteratively investigate weak points |

Each step is persisted to `flows.db` with full prompt/response/token/latency forensics.

## LLM providers

Configure providers via `config.toml`. Only providers with API keys are activated; the fallback chain tries providers in order.

| Provider | API style | Models |
|----------|-----------|--------|
| Gemini | Google Gemini | gemini-2.0-flash, gemini-1.5-pro |
| Mistral | OpenAI-compat | mistral-large-latest, mistral-small-latest, codestral-latest |
| Groq | OpenAI-compat | llama-3.3-70b-versatile, llama-3.1-8b-instant |
| OpenRouter | OpenAI-compat | deepseek/deepseek-chat, qwen/qwen-2.5-72b-instruct |
| Anthropic | Messages API | claude-sonnet-4-5-20250929, claude-haiku-4-5-20251001 |
| HuggingFace | OpenAI-compat | meta-llama/Llama-3.3-70B-Instruct |

Provider/model routing: `"groq/llama-3.3-70b-versatile"` routes directly to that provider.

## MCP tools

15 tools available via MCP-over-QUIC (ALPN `horos-mcp-v1`):

**Core (9):** `ask_question`, `answer_node`, `get_tree`, `get_node`, `search_nodes`, `vote`, `list_questions`, `list_bounties`, `get_tags`

**Dynamic (6, hot-reloadable from SQLite):** `instance_stats`, `recent_activity`, `audit_recent`, `slow_queries`, `temperature_distribution`, `top_contributors`

Dynamic tools are stored in `mcp_tools_registry` and can be updated without restart (PRAGMA data_version polling).

## Configuration

Full reference: [`config.example.toml`](config.example.toml)

```toml
[server]
addr = ":8080"
cert_file = ""          # empty = self-signed dev cert
key_file = ""

[database]
path = "data/nodes.db"
flows_path = "data/flows.db"
metrics_path = "data/metrics.db"

[auth]
jwt_secret = "change-me-in-production"
token_expiry_min = 1440

[instance]
id = "local"
name = "horostracker-local"

[bot]
handle = "horostracker"
enabled = true
credit_per_day = 1000
default_provider = ""
default_model = ""

[federation]
enabled = false
instance_url = ""
signature_algorithm = "Ed25519"
verify_signatures = true
peer_instances = []

[llm]
gemini_api_key = ""
mistral_api_key = ""
openrouter_api_key = ""
groq_api_key = ""
anthropic_api_key = ""
huggingface_api_key = ""
```

## Database schema

**nodes.db** (18 tables):
`nodes`, `nodes_fts`, `users`, `votes`, `thanks`, `tags`, `bounties`, `bounty_contributions`, `sources`, `credit_ledger`, `reputation_events`, `challenges`, `moderation_scores`, `renders`, `audit_log`, `sql_traces`, `mcp_tools_registry`, `mcp_tools_history`

**flows.db** (3 tables):
`flow_steps`, `llm_responses`, `llm_evals`

**metrics.db** (4 tables):
`http_requests`, `mcp_calls`, `llm_calls`, `daily_stats`

## Dataset export

Two export formats, both JSONL with per-export anonymization (salt + SHA-256):

**Tree export** — full proof tree with anonymized author IDs, tags, sources, temperature, scores.

**Corrected garbage set** — structured demolition of a false claim:
- Original claim + credible-sounding formulations
- Full demolition tree (opposing claims, pieces, counter-arguments)
- Deception mechanism classification
- Resolution (if available)
- Adversarial challenge count from DB

## Build

```bash
# Static binary, no CGO
CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o horostracker .

# Verify
sha256sum horostracker
./horostracker version
```

Produces a ~13 MB static binary. Runs on any Linux/macOS/Windows.

## License

AGPL-3.0
