# e2e

Responsabilite: Suite de tests end-to-end pour horostracker — lance un subprocess reel, execute les scenarios via HTTP, verifie en base SQLite.
Depend de: `modernc.org/sqlite`, horostracker binary (compile avant execution)
Dependants: aucun (package de test)
Point d'entree: e2e_test.go (TestMain + ensureHarness)
Types cles:
- `TestHarness` — gestion du subprocess horostracker (port libre, temp dir, HTTP client TLS skip-verify, helpers Do/JSON/RawBody/Register/Login/AskQuestion/AnswerNode/GetNode)
- `DBAssert` — assertions directes sur les bases SQLite (nodes.db, flows.db, metrics.db) via connexions persistantes
- `ResultsDB` — logger de resultats de tests dans SQLite (test_results.db, pass/fail/skip avec request/response)
- `FixtureUser` — utilisateurs de test predefined (Alice, Bob, Carol, David, Eve, Operator, Provider, Researcher)
Fichiers:
- `harness.go` — TestHarness : spawn process, health check, HTTP helpers, graceful stop
- `fixtures.go` — utilisateurs, textes unicode/stress, payloads XSS/SQLi/injection, tags, questions/reponses
- `results.go` — ResultsDB : persistence des resultats de test en SQLite
- `dbassert.go` — DBAssert : AssertNodeExists, AssertNodeField, AssertRowCount, QueryFlowSteps, visibility helpers, clone helpers
- `assertions_test.go` — helpers d'assertion generiques
Suites de tests: auth, nodes, tree, search, social, visibility, softdelete, workflow, export, security, federation, llm, fivew1h, feedback, challenge, golden, sandbox, sources, admin_ui, admin_csp, features_ui, resolution_subtree, user
Invariants:
- Un seul subprocess partage par tous les tests (`sync.Once` via `ensureHarness`)
- Le harness genere un `config.toml` temporaire avec port libre et DBs dans temp dir
- Health check : poll `/api/bot/status` pendant 15s avec backoff exponentiel
- Stop : SIGTERM puis SIGKILL apres 5s + cleanup temp dir
- `Record(t, start, req, resp)` pour logger chaque test dans test_results.db
- Prerequis : binary compile (`CGO_ENABLED=0 go build -o horostracker .` depuis la racine du module)
- Tests LLM conditionnes par `ANTHROPIC_API_KEY` / `GEMINI_API_KEY` via `HasLLM()`, `HasAnthropic()`, `HasGemini()`
NE PAS:
- Lancer les tests sans avoir compile le binary horostracker d'abord
- Utiliser `t.TempDir()` pour le DataDir (cleanup premature casse le DBAssert partage)
- Modifier les fixtures Users sans adapter les tests qui en dependent
- Oublier `defer Record(t, start, req, resp)` dans les nouveaux tests
