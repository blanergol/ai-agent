# agent-core

> **English** | [Русский](README.ru.md)

`agent-core` is a production-oriented framework for building business AI agents in Go.

The project already includes:

- an executable agent runtime with the `observe -> plan -> act -> reflect -> stop` loop;
- a strict JSON action planner (`tool | final | noop`);
- a tool registry with security policies, retry/backoff, caching, and deduplication;
- memory management (short-term + long-term), state/snapshot, and guardrails;
- HTTP API and CLI entry points;
- extensibility through skills and MCP servers;
- baseline telemetry (logs/tracing/metrics/artifacts) and sensitive-data redaction.

---

## 1) What This Framework Is For

`agent-core` solves a common business-agent workflow:

1. receive a user task;
2. select the next safe action (tool call or final response);
3. execute the action with limits and error handling;
4. persist relevant context;
5. return a validated result with diagnostics.

Core idea: split the agent into small, testable components with strict contracts.

---

## 2) Quick Start

### Requirements

- Go `1.25.1` (see `go.mod`)
- for `openai` provider: `OPENAI_API_KEY` or `AGENT_CORE_LLM_OPENAI_API_KEY`
- for `openrouter` provider: `OPENROUTER_API_KEY` or `AGENT_CORE_LLM_OPENROUTER_API_KEY`

### Install and test

```bash
go mod tidy
go test ./...
```

### Example 1. Single run via CLI

```powershell
$env:AGENT_CORE_LLM_PROVIDER="openai"
$env:AGENT_CORE_LLM_MODEL="gpt-4o-mini"
$env:OPENAI_API_KEY="<secret>"
go run ./cmd/agent-core run --input "What is the current UTC time?"
```

OpenRouter example:

```powershell
$env:AGENT_CORE_LLM_PROVIDER="openrouter"
$env:AGENT_CORE_LLM_MODEL="openai/gpt-4o-mini"
$env:OPENROUTER_API_KEY="<secret>"
$env:AGENT_CORE_LLM_OPENROUTER_HTTP_REFERER="http://localhost"
$env:AGENT_CORE_LLM_OPENROUTER_APP_TITLE="agent-core-local"
go run ./cmd/agent-core run --input "What is the current UTC time?"
```

Useful `run` flags:

- `--provider` (`openai|openrouter|ollama|lmstudio`)
- `--model`
- `--debug`
- `--input` (if empty, agent reads stdin)
- `--user-sub`
- `--session-id`
- `--correlation-id`

### Example 2. HTTP server

```powershell
go run ./cmd/agent-core serve --addr ":8080" --first-only=true
```

Enable built-in Web UI (single page for manual testing):

```powershell
$env:AGENT_CORE_WEB_UI_ENABLED="true"
go run ./cmd/agent-core serve --addr ":8080" --first-only=false
```

After startup, UI is available at `http://localhost:8080/` (also `http://localhost:8080/ui`).

Request example:

```bash
curl -X POST http://localhost:8080/v1/agent/run \
  -H "Content-Type: application/json" \
  -d '{"input":"What is the current UTC time?"}'
```

`serve` flags:

- `--addr` (default `:8080`)
- `--first-only` (default `true`)
- `--shutdown-timeout-ms` (default `5000`)
- `--provider`, `--model`, `--debug`

---

## 3) API Contract

### `GET /` and `GET /ui` (optional)

Routes are available only when `AGENT_CORE_WEB_UI_ENABLED=true`.

UI layout:

- top area: chat messages (agent replies + user prompts);
- middle: auto-resizing prompt textarea + `Send` button;
- bottom: request status and minimal technical details (`HTTP`, `steps`, `tool_calls`, `stop_reason`, `session_id`, `correlation_id`).

### `GET /healthz`

Response:

```json
{"status":"ok"}
```

### `POST /v1/agent/run`

Request body:

```json
{
  "input": "task text",
  "user_sub": "optional",
  "session_id": "optional",
  "correlation_id": "optional"
}
```

Identifier notes:

- `session_id` — stable dialog/request-chain id.
- `correlation_id` — id of a specific request within `session_id`.
- `user_sub` — stable user identifier (prefer pseudonym, not email). With Langfuse enabled, this maps to trace `userId`.

Successful response:

```json
{
  "final_response": "response text",
  "steps": 2,
  "tool_calls": 1,
  "stop_reason": "planner_done",
  "session_id": "9a89...",
  "correlation_id": "7fd1...",
  "api_version": "v1"
}
```

Errors:

- body: `{"error":"..."}` (no internal stack/cause leak)
- status codes map from typed errors (`BAD_REQUEST`, `VALIDATION`, `RATE_LIMIT`, `TRANSIENT`, etc.)

`first-only=true` behavior:

- server accepts only the first **successful** request;
- next request returns `409 Conflict` with error: `"first request already processed"`;
- after first success, graceful shutdown is initiated.

---

## 4) Runtime Architecture

### High-level flow

1. Input (`CLI`/`HTTP`) -> `RunInput`.
2. Session context is created/restored (`session_id`, `correlation_id`).
3. `memory.BuildContext()` prepares planner observation.
4. `planner.Plan()` returns a JSON action.
5. `guardrails.ValidateAction()` applies safety checks.
6. `agent.act()` executes `tool` or completes `final`.
7. Step outputs are written to memory/state/telemetry.
8. Final output is validated (`output policy + optional json schema`).
9. `RunResult` is returned (`api_version=v1`) and runtime snapshot is persisted.

### Stop conditions

Loop stops when one of the following is true:

- planner returned `final` or `done=true`;
- guardrail limits reached (`steps/time/tool_calls/output_size`);
- the same effective action repeated more than 3 times (`repeated_action_detected`);
- planner/tool/output-validation recovery retries were exhausted.

---

## 5) Codebase Map

### Entry point

- `cmd/agent-core/main.go`
  - runtime dependency wiring;
  - CLI commands `run` and `serve`;
  - wiring chain: `config -> llm -> memory -> tools -> skills -> mcp -> planner -> guardrails -> agent`.
- `cmd/agent-core/server.go`
  - HTTP API (`/healthz`, `/v1/agent/run`) + Web UI routes;
  - JSON decoding with body-size limit and `DisallowUnknownFields`.
- `cmd/agent-core/webui.go`
  - single-page built-in UI for manual agent testing;
  - posts directly to `/v1/agent/run`.

### Configuration

- `config/config.go`
  - typed `Config` + `DefaultConfig()`;
  - env loading, derived defaults, validation;
  - parsing for `AGENT_CORE_MCP_SERVERS` and `AGENT_CORE_AGENT_TOOL_ERROR_FALLBACK`.

### Agent and runtime contracts

- `internal/agent/agent.go`
  - main loop, planner retries, tool error policy, output validation.
- `internal/agent/events.go`
  - observable events (`run_started`, `step_planned`, `tool_failed`, ...).
- `internal/agent/snapshot.go`, `kv_snapshot_store.go`
  - `SnapshotStore` contract and `state.Store` implementation.
- `internal/agent/contract_notes.go`
  - changelog notes for public contract `v1`.

### Planner

- `internal/planner/planner.go`
  - strict JSON action schema;
  - `ChatJSON` + semantic validation + tool-policy validation + retries.
- `internal/planner/tool_policy.go`
  - tool-selection policy from current tool catalog.

### Tools

- `internal/tools/registry.go`
  - allowlist/denylist;
  - timeout + concurrency limiter;
  - retry/backoff;
  - dedup for mutating calls;
  - read-cache (local + shared backplane);
  - input/output JSON schema validation.
- `internal/tools/kv.go`, `time_now.go`, `http_get.go`
  - built-in tools.

### LLM layer

- `internal/llm/factory.go`
  - provider selection (`openai`, `openrouter`, `ollama`, `lmstudio`).
- `internal/llm/langchain_provider.go`
  - model calls via `langchaingo`;
  - circuit breaker, retry/backoff, cache, stream;
  - token-usage metrics (`provider_native` or `estimated`).

### Memory, state, guardrails

- `internal/memory/memory.go`
  - short-term window + long-term recall;
  - token-budget trimming;
  - write policy + redaction.
- `internal/state/store.go`, `session_scope.go`
  - thread-safe KV, atomic file persistence, session namespace.
- `internal/guardrails/guardrails.go`
  - limits for steps, time, tool calls, tool output bytes.

### Security and output validation

- `internal/output/*`
  - policy validator + schema validator + composition.
- `internal/redact/redact.go`
  - masking of keys, bearer/JWT, email, private keys.
- `internal/apperrors/errors.go`
  - typed errors and HTTP mapping.

### Integrations and extensibility

- `internal/skills/*`
  - skill registry, built-in `ops` skill.
- `internal/mcp/mcp.go`
  - import remote MCP tools as `mcp.<server>.<tool>`.
- `internal/cache/backplane.go`
  - `InMemoryBackplane` and `FileBackplane` for shared cache.

### Telemetry

- `internal/telemetry/*`
  - session/correlation context;
  - context-aware logger;
  - tracer/metrics interfaces;
  - debug artifacts sink.

---

## 6) Built-in Tools and Skills

### Built-in tools

1. `time.now`
- returns UTC in `RFC3339`;
- read-only + safe retry.

2. `kv.put`
- writes JSON value to current-session state (`session:<session_id>:<key>`);
- mutating, marked as safe-retry.

3. `kv.get`
- reads JSON value from current-session state;
- returns `null` if key is missing.

4. `http.get` (from `ops` skill)
- only `http/https`;
- allowlisted domains only;
- blocks internal/private IPs (including DNS-resolved private ranges);
- response-size limit + timeout;
- read-only + safe-retry + cache TTL.

### Skills

Current built-in skill:

- `ops` (`internal/skills/ops.go`)
  - registers `time.now` and `http.get`;
  - adds planner prompt hint.

Active skills are configured via `AGENT_CORE_SKILLS` (CSV).

---

## 7) Configuration (ENV)

Baseline template: [`.env.example`](./.env.example)

Important variables are grouped below. For complete behavior, check `config/DefaultConfig()` and `applyEnv()`.

### Runtime mode

- `AGENT_CORE_MODE=dev|test|prod`
  - `test` automatically enables:
    - `AGENT_CORE_AGENT_DETERMINISTIC=true`
    - `AGENT_CORE_LLM_DISABLE_JITTER=true`

### LLM

- `AGENT_CORE_LLM_PROVIDER=openai|openrouter|ollama|lmstudio`
- `AGENT_CORE_LLM_MODEL=...`
- `AGENT_CORE_LLM_BASE_URL=...` (`openrouter` defaults to `https://openrouter.ai/api/v1`)
- `AGENT_CORE_LLM_OPENAI_API_KEY=...` (fallback: `OPENAI_API_KEY`)
- `AGENT_CORE_LLM_OPENROUTER_API_KEY=...` (fallback: `OPENROUTER_API_KEY`, then `OPENAI_API_KEY`)
- `AGENT_CORE_LLM_OPENROUTER_HTTP_REFERER=...` (optional, `HTTP-Referer` header)
- `AGENT_CORE_LLM_OPENROUTER_APP_TITLE=...` (optional, `X-Title` header)
- `AGENT_CORE_LLM_TEMPERATURE`
- `AGENT_CORE_LLM_TOP_P`
- `AGENT_CORE_LLM_SEED`
- `AGENT_CORE_LLM_MAX_OUTPUT_TOKENS`
- `AGENT_CORE_LLM_TIMEOUT_MS`
- `AGENT_CORE_LLM_MAX_RETRIES`
- `AGENT_CORE_LLM_RETRY_BASE_MS`
- `AGENT_CORE_LLM_MAX_PARALLEL`
- `AGENT_CORE_LLM_CIRCUIT_BREAKER_FAILURES`
- `AGENT_CORE_LLM_CIRCUIT_BREAKER_COOLDOWN_MS`
- `AGENT_CORE_LLM_DISABLE_JITTER`
- `AGENT_CORE_LLM_CACHE_TTL_MS` (shared/local chat cache, `0` disables)

### Memory

- `AGENT_CORE_MEMORY_SHORT_TERM_MAX_MESSAGES`
- `AGENT_CORE_MEMORY_RECALL_TOP_K`
- `AGENT_CORE_MEMORY_TOKEN_BUDGET`

### Planner

- `AGENT_CORE_PLANNER_MAX_STEPS`
- `AGENT_CORE_PLANNER_MAX_PLANNING_RETRIES`
- `AGENT_CORE_PLANNER_ACTION_JSON_RETRIES`

### Tools

- `AGENT_CORE_TOOLS_ALLOWLIST` (CSV)
- `AGENT_CORE_TOOLS_DENYLIST` (CSV)
- `AGENT_CORE_TOOLS_DEFAULT_TIMEOUT_MS`
- `AGENT_CORE_TOOLS_MAX_EXECUTION_RETRIES`
- `AGENT_CORE_TOOLS_RETRY_BASE_MS`
- `AGENT_CORE_TOOLS_MAX_PARALLEL`
- `AGENT_CORE_TOOLS_DEDUP_TTL_MS`
- `AGENT_CORE_TOOLS_MAX_OUTPUT_BYTES`
- `AGENT_CORE_TOOLS_HTTP_ALLOW_DOMAINS` (CSV)
- `AGENT_CORE_TOOLS_HTTP_MAX_BODY_BYTES`
- `AGENT_CORE_TOOLS_HTTP_TIMEOUT_MS`
- `AGENT_CORE_TOOLS_HTTP_READ_CACHE_TTL_MS`

### MCP

- `AGENT_CORE_MCP_ENABLED=true|false`
- `AGENT_CORE_MCP_SERVERS`
  - JSON example:
    ```json
    [{"name":"local","base_url":"http://localhost:8787","enabled":true,"token":""}]
    ```
  - KV example:
    ```text
    name=local,base_url=http://localhost:8787,enabled=true
    ```
- secret fallback: `MCP_TOKEN_<SERVER_NAME>`

### State

- `AGENT_CORE_STATE_PERSIST_PATH` (persisted KV state file)
- `AGENT_CORE_STATE_CACHE_BACKPLANE_DIR` (shared file cache directory)
- `AGENT_CORE_STATE_TIMEOUT_MS` (snapshot operation timeout)

If `CACHE_BACKPLANE_DIR` is empty but `PERSIST_PATH` is set, cache-backplane path can be derived automatically.

### Agent / Guardrails

- `AGENT_CORE_AGENT_MAX_STEP_DURATION_MS`
- `AGENT_CORE_AGENT_CONTINUE_ON_TOOL_ERROR`
- `AGENT_CORE_AGENT_TOOL_ERROR_MODE=continue|fail`
- `AGENT_CORE_AGENT_TOOL_ERROR_FALLBACK` (per-tool override, e.g. `ticket.create=fail,http.get=continue`)
- `AGENT_CORE_AGENT_MAX_INPUT_CHARS`
- `AGENT_CORE_AGENT_DETERMINISTIC`
- `AGENT_CORE_GUARDRAILS_MAX_STEPS`
- `AGENT_CORE_GUARDRAILS_MAX_TOOL_CALLS`
- `AGENT_CORE_GUARDRAILS_MAX_TIME_MS`
- `AGENT_CORE_GUARDRAILS_MAX_TOOL_OUTPUT_BYTES`

### Output validation

- `AGENT_CORE_OUTPUT_MAX_CHARS`
- `AGENT_CORE_OUTPUT_FORBIDDEN_SUBSTRINGS` (CSV)
- `AGENT_CORE_OUTPUT_VALIDATION_RETRIES`
- `AGENT_CORE_OUTPUT_JSON_SCHEMA`

### Auth / Logging / Skills

- `AGENT_CORE_AUTH_USER_AUTH_HEADER`
- `AGENT_CORE_LOGGING_DEBUG`
- `AGENT_CORE_LOGGING_VERBOSE_TRACING`
- `AGENT_CORE_LOGGING_DEBUG_ARTIFACTS`
- `AGENT_CORE_LOGGING_DEBUG_ARTIFACTS_MAX_CHARS`
- `AGENT_CORE_SKILLS` (CSV)

### Web UI

- `AGENT_CORE_WEB_UI_ENABLED=true|false`

### Langfuse

- `AGENT_CORE_LANGFUSE_ENABLED`
- `AGENT_CORE_LANGFUSE_HOST`
- `AGENT_CORE_LANGFUSE_PUBLIC_KEY`
- `AGENT_CORE_LANGFUSE_SECRET_KEY`
- `AGENT_CORE_LANGFUSE_TIMEOUT_MS`
- `AGENT_CORE_LANGFUSE_SERVICE_NAME`
- `AGENT_CORE_LANGFUSE_SERVICE_VERSION`
- `AGENT_CORE_LANGFUSE_ENVIRONMENT`
- `AGENT_CORE_LANGFUSE_MODEL_PRICES` (optional fallback pricing JSON for cost details)

---

## 8) Memory, State, and Cache

### Memory

`memory.Manager` combines:

- short-term memory (recent message window);
- long-term memory (simple recall by token overlap);
- context assembly with token-budget trimming.

Memory stores:

- user messages;
- tool results (as untrusted data);
- assistant final messages;
- system corrections (for output validation retries).

### State

`state.Store` is a thread-safe KV store with optional file persistence.

Key patterns:

- per-session keys: `session:<session_id>:<key>`;
- global/runtime keys for internal metadata.

Snapshots:

- runtime snapshots persist short-term context + guardrail state;
- on next run with same `session_id`, runtime can resume safely.

### Cache backplane

`cache.Backplane` enables cache sharing between instances:

- local mode: in-memory only;
- shared mode: file backplane (`AGENT_CORE_STATE_CACHE_BACKPLANE_DIR`).

Used for:

- tool read-cache (read-only tools);
- dedup cache for mutating tools with idempotency scope;
- optional LLM response cache.

---

## 9) Telemetry and Observability

### Logs

Structured JSON logs include:

- `session_id`
- `correlation_id`
- span lifecycle events (`span_start`, `span_end`, `span_event`)
- normalized error text (redacted)

### Tracing

Runtime emits spans for:

- agent lifecycle (`agent.run`, `agent.plan`, `agent.act`)
- memory operations
- tool executions
- LLM calls (`llm.chat`, `llm.chat_stream`)

Langfuse/OpenTelemetry attributes include:

- `langfuse.session.id` from runtime `session_id`
- `langfuse.user.id` from runtime `user_sub`
- generation metadata (`model.name`, parameters, usage, cost, completion start time)

### Metrics (through interface)

Examples:

- `agent.run`
- `tool.calls`, `tool.retries`, `tool.latency_ms`
- `llm.calls`, `llm.tokens`, `llm.tokens.total`
- `memory.write`, `memory.recall`, `memory.context.tokens`

### Debug artifacts

Optional (`AGENT_CORE_LOGGING_DEBUG_ARTIFACTS=true`) artifacts include:

- prompt payloads;
- response payloads;
- state/error payloads;

with redaction and `MAX_CHARS` truncation.

---

## 10) Security and Guardrails

### What is enforced

- allowlist/denylist tool policy;
- limits on steps/time/tool calls/output size;
- timeout and retry policies;
- output schema validation;
- redaction in logs and artifacts;
- private-IP/domain bypass protection in `http.get`;
- treating tool output as untrusted data in memory and planner prompts.

### Minimal threat model covered by framework

- prompt injection from user/tool/memory;
- data exfiltration via logs/output;
- unsafe retries for mutating tools;
- SSRF in HTTP tools;
- cascading failures from external LLM providers (retry + circuit breaker).

---

## 11) Extending the Framework

### 11.1 Add a new tool

Steps:

1. Implement `tools.Tool`.
2. (Optional) Implement `RetryPolicy` and `CachePolicy`.
3. Register tool in `buildRuntime` or inside a skill.
4. Add tool name to `AGENT_CORE_TOOLS_ALLOWLIST`.

Example:

```go
type CRMGetCustomerTool struct {
    client *crm.Client
}

func (t *CRMGetCustomerTool) Name() string { return "crm.get_customer" }
func (t *CRMGetCustomerTool) Description() string { return "Reads customer profile by id" }
func (t *CRMGetCustomerTool) InputSchema() string {
    return `{"type":"object","required":["customer_id"],"properties":{"customer_id":{"type":"string","minLength":1}},"additionalProperties":false}`
}
func (t *CRMGetCustomerTool) OutputSchema() string {
    return `{"type":"object","required":["customer_id","tier","status"],"properties":{"customer_id":{"type":"string"},"tier":{"type":"string"},"status":{"type":"string"}},"additionalProperties":true}`
}
func (t *CRMGetCustomerTool) IsReadOnly() bool  { return true }
func (t *CRMGetCustomerTool) IsSafeRetry() bool { return true }

func (t *CRMGetCustomerTool) Execute(ctx context.Context, args json.RawMessage) (tools.ToolResult, error) {
    var in struct {
        CustomerID string `json:"customer_id"`
    }
    if err := json.Unmarshal(args, &in); err != nil {
        return tools.ToolResult{}, err
    }
    profile, err := t.client.GetCustomer(ctx, in.CustomerID)
    if err != nil {
        return tools.ToolResult{}, err
    }
    raw, err := json.Marshal(profile)
    if err != nil {
        return tools.ToolResult{}, err
    }
    return tools.ToolResult{Output: string(raw)}, nil
}
```

### 11.2 Add a new skill

A `skill` = tool package + prompt additions.

```go
type SupportSkill struct{}

func (s *SupportSkill) Name() string { return "support" }
func (s *SupportSkill) Register(reg *tools.Registry) error {
    if err := reg.Register(NewCRMGetCustomerTool(...)); err != nil { return err }
    if err := reg.Register(NewTicketCreateTool(...)); err != nil { return err }
    return nil
}
func (s *SupportSkill) PromptAdditions() []string {
    return []string{
        "Skill support enabled: verify customer status before creating any ticket.",
    }
}
```

Then:

1. `skillRegistry.Register(&SupportSkill{})`
2. `AGENT_CORE_SKILLS=ops,support`

### 11.3 Connect MCP tools

1. Enable `AGENT_CORE_MCP_ENABLED=true`
2. Configure `AGENT_CORE_MCP_SERVERS`
3. (Optional) provide tokens via `MCP_TOKEN_<SERVER>`

After import, tools are available as:

- `mcp.<server>.<tool>`

### 11.4 Final response contract (JSON Schema)

Enforce strict structured final responses:

```powershell
$env:AGENT_CORE_OUTPUT_JSON_SCHEMA='{"type":"object","required":["answer","confidence"],"properties":{"answer":{"type":"string"},"confidence":{"type":"number","minimum":0,"maximum":1}},"additionalProperties":false}'
```

If final output is invalid:

- agent retries up to `AGENT_CORE_OUTPUT_VALIDATION_RETRIES`;
- a system correction prompt is added to memory to force a compliant final response.

---

## 12) Business AI Agent Implementation Examples

Practical templates below can be implemented without changing core runtime architecture.

### Scenario A: Support agent (L1/L2 triage)

Goal:

- classify incoming issue quickly;
- validate customer profile;
- propose resolution or create ticket.

Tools:

- read-only: `crm.get_customer`, `kb.search_article`
- mutating: `ticket.create`
- helper: `time.now`, `kv.put`, `kv.get`

Recommended output schema:

```json
{
  "type": "object",
  "required": ["category", "priority", "answer", "next_action"],
  "properties": {
    "category": {"type": "string"},
    "priority": {"type": "string", "enum": ["low", "medium", "high", "critical"]},
    "answer": {"type": "string"},
    "next_action": {"type": "string", "enum": ["resolved", "create_ticket", "ask_clarification"]},
    "ticket_payload": {"type": "object"}
  },
  "additionalProperties": false
}
```

Key configuration:

```powershell
$env:AGENT_CORE_SKILLS="ops,support"
$env:AGENT_CORE_TOOLS_ALLOWLIST="time.now,kv.put,kv.get,crm.get_customer,kb.search_article,ticket.create"
$env:AGENT_CORE_AGENT_TOOL_ERROR_MODE="continue"
$env:AGENT_CORE_AGENT_TOOL_ERROR_FALLBACK='ticket.create=fail'
$env:AGENT_CORE_OUTPUT_JSON_SCHEMA='<schema above>'
```

Why this works:

- keep `ticket.create` strict (`fail`) to avoid silently dropping incidents;
- read-only tools can be cached and retried safely;
- planner is constrained to catalog tools only.

---

### Scenario B: Sales agent (qualification + next best action)

Goal:

- qualify lead;
- recommend next action to account manager;
- optionally create/update opportunity.

Tools:

- `crm.find_lead`
- `crm.upsert_lead`
- `scoring.score_lead`
- `calendar.find_slots`
- `mail.send_template`

Output:

- structured JSON with `lead_score`, `segment`, `recommended_action`, `draft_message`.

Recommended guardrails:

```powershell
$env:AGENT_CORE_GUARDRAILS_MAX_STEPS="6"
$env:AGENT_CORE_GUARDRAILS_MAX_TOOL_CALLS="6"
$env:AGENT_CORE_AGENT_MAX_INPUT_CHARS="6000"
$env:AGENT_CORE_TOOLS_MAX_OUTPUT_BYTES="32768"
```

Practical pattern:

- mark `calendar.find_slots` and `scoring.score_lead` as read-only + cache TTL;
- keep `crm.upsert_lead` mutating + safe-retry only if CRM side is truly idempotent.

---

### Scenario C: E-commerce post-purchase agent

Goal:

- answer order-status questions;
- evaluate refund eligibility;
- create refund request when policy allows.

Tools:

- `order.get`
- `shipment.track`
- `refund.check_policy`
- `refund.create_request`

Important notes:

- `shipment.track` can be implemented via safe `http.get` with carrier-domain allowlist;
- keep `refund.create_request` in strict `fail` mode on tool errors;
- enforce output JSON schema for deterministic downstream integration.

---

### Scenario D: Ops / Incident response agent (SRE NOC)

Goal:

- receive alerts;
- collect context (metrics/logs/deploy status);
- produce structured runbook steps for operator.

Tools:

- built-in `ops` skill (`time.now`, `http.get`)
- MCP tools, for example:
  - `mcp.monitoring.query`
  - `mcp.logs.search`
  - `mcp.deployments.last_change`

Configuration:

```powershell
$env:AGENT_CORE_SKILLS="ops"
$env:AGENT_CORE_MCP_ENABLED="true"
$env:AGENT_CORE_MCP_SERVERS='[{"name":"obs","base_url":"http://mcp-observability.internal","enabled":true}]'
$env:MCP_TOKEN_OBS="<secret>"
$env:AGENT_CORE_TOOLS_ALLOWLIST="time.now,http.get,mcp.obs.monitoring.query,mcp.obs.logs.search,mcp.obs.deployments.last_change"
```

HTTP mode recommendation:

- for long-running service use `first-only=false` with external orchestrator/reverse proxy.

---

## 13) Production Checklist

Before production rollout verify:

1. Tool allowlist/denylist is explicitly configured.
2. Retry semantics for mutating tools are explicitly defined.
3. Guardrail limits are aligned with your SLA.
4. Structured logging and correlation (`session_id`, `correlation_id`) are enabled.
5. Output schema is validated against downstream contract.
6. Redaction is tested on real payload samples.
7. File backplane is enabled for multi-instance deployments.
8. Integration/e2e tests cover your business tools.

---

## 14) Testing and Determinism

Core commands:

```bash
go test ./...
go test -covermode=atomic -coverprofile=coverage.out ./...
go tool cover -func=coverage.out
```

Useful test packages:

- `internal/agent/*_test.go` — agent lifecycle, tool-error policy, snapshot restore.
- `internal/planner/planner_test.go` — JSON retries and tool-selection policy.
- `internal/tools/*_test.go` — allowlist/security/retry/cache/dedup.
- `internal/llm/*_test.go` — provider cache/stream/token metrics.
- `cmd/agent-core/server_test.go` — HTTP contract and first-only behavior.

Golden fixture for stable runtime contract:

- `internal/agent/testdata/run_result.golden.json`

---

## 15) Current Framework Limitations

Important constraints:

- long-term recall is currently simple token-overlap (no embeddings/vector DB);
- built-in HTTP API is synchronous (no built-in task queue);
- RBAC/tenant isolation for business entities must be implemented in your own tools/services;
- provider-native streaming/usage depends on backend capabilities (`provider_native` may be unavailable).

---

## 16) Recommended Adoption Path

1. Start with 2-3 read-only tools and one mutating action.
2. Lock final output JSON schema for external consumers.
3. Enable structured telemetry and verify redaction.
4. Enable MCP only for truly required data sources.
5. Gradually expand skill packages by business domain.

---

## 17) License

See [`LICENSE`](./LICENSE).

---

## Langfuse Zero-Touch Bootstrap

`docker-compose.observability.yml` provisions Langfuse and all required dependencies with no manual setup in UI.

Pre-provisioned on startup:
- Organization: `default-org` (`Default Organization`)
- Project: `default-project` (`Default Project`)
- Initial user: `admin@local.dev` / `adminadmin`
- Project keys: `lf_pk_default_public_key` / `lf_sk_default_secret_key`
- S3 bucket for Langfuse events: `langfuse` (created by `langfuse-minio-init`)

How provisioning works:
- `LANGFUSE_INIT_*` and `LANGFUSE_DEFAULT_*` configure default org/project/user bootstrap.
- `langfuse-bootstrap` runs idempotent SQL for org/project membership defaults.
- `langfuse-minio-init` creates MinIO bucket `langfuse` before `langfuse-web` and `langfuse-worker` start.

Agent integration (already wired in `docker-compose.agent.yml`):
- `AGENT_CORE_LANGFUSE_ENABLED=true`
- `AGENT_CORE_LANGFUSE_HOST=http://langfuse-web:3000`
- `AGENT_CORE_LANGFUSE_PUBLIC_KEY=lf_pk_default_public_key`
- `AGENT_CORE_LANGFUSE_SECRET_KEY=lf_sk_default_secret_key`
- optional service metadata:
  - `AGENT_CORE_LANGFUSE_SERVICE_NAME`
  - `AGENT_CORE_LANGFUSE_SERVICE_VERSION`
  - `AGENT_CORE_LANGFUSE_ENVIRONMENT`
  - `AGENT_CORE_LANGFUSE_MODEL_PRICES` (optional fallback pricing for cost_details)

Runtime behavior:
- Agent exports traces via OTLP HTTP to `LANGFUSE_HOST + /api/public/otel/v1/traces`.
- Agent artifacts (`prompt`, `response`, `state`) are attached as span events and visible in Langfuse traces.
- Agent sets Langfuse-native IDs on each span:
  - `langfuse.session.id` from runtime `session_id`
  - `langfuse.user.id` from runtime `user_sub`
- Agent maps runtime artifacts to Langfuse input/output fields:
  - `agent.user_input` -> `langfuse.trace.input` + `langfuse.observation.input`
  - `agent.final_response` -> `langfuse.trace.output` + `langfuse.observation.output`
  - `agent.run_result` (fallback for guardrail stop) -> `langfuse.trace.output`
- Agent emits generation-level usage/cost fields, so Langfuse can display:
  - Model Usage
  - Model Costs
  - User Consumption (via `langfuse.user.id` + usage/cost details)
  - Generation Latency Percentiles (from generation spans + `completion_start_time`)
- Agent emits trace-level scores via Langfuse Public API (`/api/public/scores`):
  - `agent.run.success` (`1` when final response is valid, otherwise `0`)
  - `agent.run.steps`
  - `agent.run.tool_calls`
- For local logs and non-Langfuse diagnostics, raw `user_sub` is not logged; only `user_sub_hash` is emitted.

Clean re-init of observability data:

```bash
docker compose -f docker-compose.observability.yml down -v
docker compose -f docker-compose.observability.yml up -d
```
