# agent-core

> **English** | [Русский](README.ru.md)

`agent-core` is a production-oriented framework for building business AI agents in Go.

The project already includes:

- an executable agent runtime with the `observe -> enrich_context -> plan -> act -> reflect -> stop` loop;
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

### Pipeline loop (current implementation)

Default stage order:

1. `observe`
2. `enrich_context`
3. `plan`
4. `act`
5. `reflect`
6. `stop`

This stage loop is still executed by `core.Runtime.Run()` and is **not** replaced by custom business code.

### Deterministic phases inside the loop

Each stage uses explicit deterministic phases and `RunContext.ExecutePhase(...)`:

- `INPUT` (runtime entry)
- `NORMALIZE` (`sanitize` stage when enabled)
- `GUARDRAILS` (observe + act validation points)
- `ENRICH_CONTEXT` (`enrich_context` stage)
- `DECIDE_ACTION` (`plan` stage, LLM planner)
- `BEFORE_TOOL_EXECUTION` (`act`, tool path)
- `TOOL_EXECUTION` (`act`, tool path through `ToolExecutor`)
- `AFTER_TOOL_EXECUTION` (`act`, tool path)
- `EVALUATE` (`reflect`)
- `STOP_CHECK` (`reflect`)
- `FINALIZE` (`stop`)

This gives deterministic intervention points without changing base loop semantics.

### State model used across all phases

`RunContext.State` (`core.AgentState`) keeps centralized state for the run/iteration:

- `raw_input`, `normalized_input`
- `guardrail_results` counters/violations
- deterministic `context` enrichment payloads
- `retrieved_docs`
- memory snapshot used for planning
- `tool_calls_history` and `tool_results`
- `errors`
- `budgets` (token/time/cost fields)
- trace/debug info (`phase` traces + iteration metrics)
- `iteration` counter

### How cycle control works

The runtime loop is unchanged and driven by stage return control:

- `StageControlContinue` -> continue next stage/iteration
- `StageControlRetry` -> retry loop with optional backoff
- `StageControlStop` -> build `RunResult` and exit

### Stop conditions

Loop stops when one of the following is true:

- planner returned `final` or `done=true`;
- guardrail limits reached (`steps/time/tool_calls/output_size`);
- the same effective action repeated more than 3 times (`repeated_action_detected`);
- planner/tool/output-validation recovery retries were exhausted.

---

## 5) Codebase Map

### Layered structure

- `core/`
  - framework contracts and orchestrator (`RunInput/RunResult`, `Action`, `Stage`, `Pipeline`, `Runtime`);
  - deterministic phase model (`Phase`), centralized state (`AgentState`), interceptors, tool-executor abstraction;
  - stage composition APIs (`InsertBefore/After`, `Replace`, `Remove`, hooks, middleware);
  - stop/retry control model for stages.
- `pkg/`
  - reusable libraries with no `internal`/`cmd` dependency:
  - `pkg/apperrors`, `pkg/redact`, `pkg/retry`, `pkg/cache`, `pkg/jsonx`, `pkg/state`, `pkg/llm`, `pkg/telemetry`;
  - core runtime implementations: `pkg/planner`, `pkg/memory`, `pkg/guardrails`, `pkg/output`, `pkg/tools`, `pkg/mcp`, `pkg/stages`, `pkg/skills`.
- `internal/`
  - project-private domain code (business adapters/tools/policies that should not be exported).
  - this repository includes a real incident-response bundle under `internal/*` (tools, skill, pipeline mutations, interceptors).
- `cmd/`
  - transports and entrypoints:
  - `cmd/agent-core/main.go` CLI (`run`, `serve`);
  - `cmd/agent-core/runtime.go` runtime wiring (`config -> llm -> tools/memory/guardrails -> runtime`);
  - `cmd/agent-core/server.go` HTTP contract (`/healthz`, `/v1/agent/run`) depends only on `core.AgentRuntime`;
  - `cmd/agent-core/webui.go` built-in UI.

### Domain implementations

- Planner: `pkg/planner/*` (`ChatJSON`, semantic validation, tool policy).
- Tool runtime and policies: `pkg/tools/*` (registry, retries, dedup, cache, validation).
- Memory/guardrails: `pkg/memory/*`, `pkg/guardrails/*`.
- Reusable state backend: `pkg/state/*` (KV store, session namespace, snapshot store).
- Reusable LLM clients/runtime: `pkg/llm/*`.
- Reusable observability contracts/backends: `pkg/telemetry/*`.
- Built-in reusable primitives: `pkg/tools/*`, `pkg/skills/*`.
- Output validation: `pkg/output/*`.
- Integrations: `pkg/mcp/*`, `pkg/skills/*`.

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

Built-in reusable skill:

- `ops` (`pkg/skills/ops.go`)
  - registers `time.now` and `http.get`;
  - adds planner prompt hint.

Optional internal skill (available when bundle is enabled):

- `incident_ops` (`internal/skills/incident_ops_skill.go`)
  - guidance-only skill (does not register tools by itself);
  - adds deterministic incident workflow hints for planner behavior.

Active skills are configured via `AGENT_CORE_SKILLS` (CSV).
When `AGENT_CORE_BUNDLE_ENABLED=true`, add `incident_ops` explicitly (for example `AGENT_CORE_SKILLS=ops,incident_ops`) if you want the internal skill guidance.

### Business Pipeline Extensions

You have 3 extension levels, from simplest to strictest:

1. Stage-level (`pkg/stages` + `stages.PipelineMutation`)
- change stage order or inject custom stage (`InsertBefore/After`, `Replace`, `Remove`, `Append`).

2. Phase-level (`core.InterceptorRegistry`)
- register deterministic logic before/after any phase via `RegisterInterceptor(phase, interceptor)`.
- best place for policy enforcement, context injection, fallback decisions, and stop-condition adjustments.

3. Tool-level (`core.ToolExecutor` + tool interceptors)
- run every tool call through a centralized executor.
- can block/rewrite calls, add deterministic retries/fallbacks, enrich tracing, or route by policy.

Minimal registration pattern:

```go
interceptors := core.NewInterceptorRegistry()
interceptors.RegisterInterceptor(core.PhaseEnrichContext, myInterceptor)
interceptors.RegisterToolInterceptor(myToolInterceptor)

toolExecutor := core.NewRegistryToolExecutor(toolRegistry, interceptors)

runtime := core.NewRuntime(cfg, core.RuntimeDeps{
    // ...
    Tools:        toolRegistry,
    ToolExecutor: toolExecutor,
    Interceptors: interceptors,
}, pipeline)
```

The default deterministic example is `core.MCPContextEnrichment`, executed in `ENRICH_CONTEXT` before planner decision.

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
  - OAuth 2.1 JSON example (client credentials):
    ```json
    [{"name":"obs","base_url":"https://mcp.internal","enabled":true,"oauth2_1":{"enabled":true,"issuer_url":"https://idp.example.com","client_id":"agent-core","audience":"mcp-api","scopes":["mcp.read","mcp.call"],"auth_method":"client_secret_basic"}}]
    ```
  - KV example:
    ```text
    name=local,base_url=http://localhost:8787,enabled=true
    ```
- secret fallback: `MCP_TOKEN_<SERVER_NAME>`
- oauth secret fallbacks: `MCP_OAUTH_CLIENT_ID_<SERVER_NAME>`, `MCP_OAUTH_CLIENT_SECRET_<SERVER_NAME>`

Deterministic enrichment sources example:

```json
[
  {
    "name": "kb",
    "tool_name": "mcp.docs.search",
    "args": {"query": "current user task"},
    "required": true
  }
]
```

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
- `AGENT_CORE_BUNDLE_ENABLED` (enable internal incident-response bundle in `internal/*`)
- `AGENT_CORE_AGENT_MCP_ENRICHMENT_SOURCES` (JSON array for deterministic `ENRICH_CONTEXT` MCP calls)
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
- `AGENT_CORE_AUTH_OAUTH2_1_ENABLED`
- `AGENT_CORE_AUTH_OAUTH2_1_ISSUER_URL`
- `AGENT_CORE_AUTH_OAUTH2_1_JWKS_URL` (optional if issuer discovery is enabled)
- `AGENT_CORE_AUTH_OAUTH2_1_AUDIENCE`
- `AGENT_CORE_AUTH_OAUTH2_1_REQUIRED_SCOPES` (CSV)
- `AGENT_CORE_AUTH_OAUTH2_1_ALLOWED_ALGS` (CSV)
- `AGENT_CORE_AUTH_OAUTH2_1_CLOCK_SKEW_SEC`
- `AGENT_CORE_AUTH_OAUTH2_1_SUBJECT_CLAIM`
- `AGENT_CORE_AUTH_OAUTH2_1_SCOPE_CLAIM`
- `AGENT_CORE_AUTH_OAUTH2_1_ALLOW_INSECURE_HTTP`
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

- runtime snapshots persist short-term memory context;
- guardrail counters are reset for each new run and are not restored from previous requests.

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

### 11.1 Control the pipeline loop without rewriting it

You can safely control behavior at three levels:

1. Stage-level (`stages.PipelineMutation`)
- reorder/insert/replace/remove stages.

2. Phase-level (`core.InterceptorRegistry`)
- add deterministic business logic before/after a phase (`INPUT`, `ENRICH_CONTEXT`, `STOP_CHECK`, etc.).

3. Tool-level (`ToolExecutionInterceptor`)
- block/rewrite/fallback tool calls in one centralized place.

### 11.2 Stage-level control (pipeline mutations)

```go
pipeline, err := stages.BuildDefaultPipeline(
    stages.FactoryConfig{},
    stages.InsertBefore("plan", myBusinessStage{}),
    stages.InsertAfter("reflect", myAuditStage{}),
)
```

Use stage mutations when you need explicit structural changes in stage order.

### 11.3 Phase-level control (deterministic business logic)

```go
type PolicyInterceptor struct{}

func (PolicyInterceptor) Name() string { return "policy" }
func (PolicyInterceptor) BeforePhase(ctx context.Context, run *core.RunContext, phase core.Phase) error {
    if phase == core.PhaseStopCheck && shouldForceStop(run) {
        run.PendingStop = true
        run.PendingStopReason = "business_policy_stop"
    }
    return nil
}
func (PolicyInterceptor) AfterPhase(context.Context, *core.RunContext, core.Phase, error) error { return nil }

interceptors := core.NewInterceptorRegistry()
interceptors.RegisterInterceptor(core.PhaseEnrichContext, PolicyInterceptor{})
```

Use phase interceptors for:

- deterministic context enrichment;
- stop-condition overrides;
- policy checks before LLM decision;
- state rewrites between iterations.

### 11.4 Tool execution control in one place

`ToolExecutor` is the single execution gateway. The default implementation delegates to `pkg/tools.Registry` (allow/deny policy, schema validation, timeout, retries, cache, dedup, logging), and additionally supports around-execution interceptors.

```go
type BlockDangerousWrites struct{}

func (BlockDangerousWrites) Name() string { return "block_writes" }
func (BlockDangerousWrites) AroundToolExecution(
    ctx context.Context,
    run *core.RunContext,
    call core.ToolCall,
    next core.ToolExecutionFunc,
) (core.ToolResult, error) {
    if call.Name == "crm.delete_customer" {
        return core.ToolResult{}, fmt.Errorf("tool blocked by business policy")
    }
    return next(ctx, run, call)
}
```

### 11.5 Deterministic MCP enrichment before planner decision

Built-in example: `core.MCPContextEnrichment` runs in `ENRICH_CONTEXT` phase and injects outputs into `RunContext.State.Context`.

Runtime wiring:

```go
interceptors := core.NewInterceptorRegistry()
interceptors.RegisterInterceptor(
    core.PhaseEnrichContext,
    core.NewMCPContextEnrichment(resolved.MCPEnrichmentSources),
)
toolExecutor := core.NewRegistryToolExecutor(toolRegistry, interceptors)
```

Config:

- `AGENT_CORE_AGENT_MCP_ENRICHMENT_SOURCES` (JSON array)
- each entry: `name`, `tool_name`, `args`, `required`

### 11.6 Add a reusable tool in `pkg/tools` (shared library)

Steps:

1. Implement `tools.Tool`.
2. (Optional) implement `RetryPolicy` and `CachePolicy`.
3. Register in runtime wiring or inside a skill.
4. Add tool to `AGENT_CORE_TOOLS_ALLOWLIST`.

### 11.7 Add a private business tool in `internal/*` (project-specific)

Use this when the tool contains private adapters/domain logic that should not become reusable package API.

Suggested layout:

```text
internal/
  support/
    tools/
      ticket_create.go
```

Register it in runtime wiring (`cmd/agent-core/runtime.go`):

```go
ticketTool := supporttools.NewTicketCreateTool(ticketClient)
if err := toolRegistry.Register(ticketTool); err != nil {
    return nil, err
}
```

This keeps architecture clean:

- domain-specific code remains private in `internal/*`;
- core loop, phase control, and tool execution contracts stay unchanged.

### 11.8 Add a new skill

A `skill` = tool package + prompt additions + optional pipeline mutations.

```go
type SupportSkill struct{}

func (s *SupportSkill) Name() string { return "support" }
func (s *SupportSkill) Register(reg *tools.Registry) error {
    if err := reg.Register(NewCRMGetCustomerTool(...)); err != nil { return err }
    if err := reg.Register(NewTicketCreateTool(...)); err != nil { return err }
    return nil
}
func (s *SupportSkill) PromptAdditions() []string {
    return []string{"Skill support enabled: verify customer status before creating any ticket."}
}
```

Then:

1. `skillRegistry.Register(&SupportSkill{})`
2. `AGENT_CORE_SKILLS=ops,support`

### 11.9 Connect MCP tools

1. Enable `AGENT_CORE_MCP_ENABLED=true`
2. Configure `AGENT_CORE_MCP_SERVERS`
3. Choose auth mode per server:
   - static bearer via `token` / `MCP_TOKEN_<SERVER>`
   - OAuth 2.1 client credentials via `oauth2_1` config (+ optional `MCP_OAUTH_CLIENT_SECRET_<SERVER>`)

After import, tools are available as `mcp.<server>.<tool>`.

### 11.10 Final response contract (JSON Schema)

```powershell
$env:AGENT_CORE_OUTPUT_JSON_SCHEMA='{"type":"object","required":["answer","confidence"],"properties":{"answer":{"type":"string"},"confidence":{"type":"number","minimum":0,"maximum":1}},"additionalProperties":false}'
```

If final output is invalid:

- agent retries up to `AGENT_CORE_OUTPUT_VALIDATION_RETRIES`;
- a system correction prompt is added to memory to force a compliant final response.

### 11.11 Internal incident-response bundle (real business case)

The repository now includes a concrete private business implementation under `internal/*`: an incident-response/NOC agent bundle.
It is opt-in and enabled by `AGENT_CORE_BUNDLE_ENABLED=true` (historical env name preserved for compatibility).

Current structure:

```text
internal/
  bundle.go
  tools/
    service_lookup_tool.go
    runbook_lookup_tool.go
    oncall_lookup_tool.go
    incident_create_tool.go
    incident_update_tool.go
    incident_status_tool.go
    incident_store.go
  skills/
    incident_ops_skill.go
  pipeline/
    input_sanitizer.go
    input_gate_stage.go
    post_reflect_audit_stage.go
    pipeline_mutations.go
  interceptors/
    normalize_trace_interceptor.go
    context_enrichment_interceptor.go
    after_tool_execution_state_interceptor.go
    stop_policy_interceptor.go
    finalize_trace_interceptor.go
    tool_policy_interceptor.go
    tool_rewrite_interceptor.go
    tool_fallback_interceptor.go
```

Runtime wiring when bundle is enabled (`cmd/agent-core/runtime.go`):

- registers private tools and appends them to tool allowlist automatically;
- registers private phase interceptors and tool execution interceptors;
- injects private pipeline mutations (`sanitize`, `_input_gate`, `_post_reflect_audit`);
- registers internal skill `incident_ops` in shared skill registry (activate via `AGENT_CORE_SKILLS`);
- appends deterministic prompt hints for incident workflows.

Business tools in this bundle:

- read-only triage tools:
  - `.service.lookup` (service metadata, owner team, runbook, dependencies, default severity)
  - `.runbook.lookup` (scenario-specific runbook steps + verification checklist)
  - `.oncall.lookup` (primary/secondary escalation contacts)
  - `.incident.status` (current incident state + timeline summary)
- mutating orchestration tools:
  - `.incident.create` (create incident, derive defaults, initialize timeline)
  - `.incident.update` (status transitions, note/next_action, assignee changes)

Deterministic controls included:

- input control: sanitizer + blocked-phrase gate before planning;
- context enrichment: incident intent/severity inference, session-state snapshot hints, optional MCP capability hints;
- tool governance: deny/prefix policy, argument rewrites, deterministic fallback payloads for selected read-only failures;
- stop/finalize control: iteration cap + final trace markers.

Recommended env for this business case:

```powershell
$env:AGENT_CORE_BUNDLE_ENABLED="true"
$env:AGENT_CORE_SKILLS="ops,incident_ops"
$env:AGENT_CORE_TOOLS_ALLOWLIST="time.now,kv.put,kv.get,http.get,.service.lookup,.runbook.lookup,.oncall.lookup,.incident.create,.incident.update,.incident.status"
```

Notes:

- private bundle tools are auto-appended to allowlist when enabled, so explicit listing is optional but recommended for audit clarity;
- `incident_ops` is guidance-focused (prompt additions), while domain logic and side effects stay in `internal/tools`;
- this is a real reference implementation, not placeholder `example_*` stubs.

### 11.12 Build your own internal bundle (detailed playbook)

Use this sequence to implement your own business case without changing core runtime contracts.

1. Define domain boundaries first.
- List entities, mutable operations, and irreversible side effects.
- Classify each operation as read-only or mutating.
- Decide which failures must hard-fail and which may degrade.

2. Model private domain data in `internal/*`.
- Implement deterministic adapters/stores first (service clients, repositories, fixtures for tests).
- Keep business API private; avoid leaking it into reusable `pkg/*`.

3. Implement tools with strict schemas.
- Each tool implements `tools.Tool` with explicit JSON input/output schema.
- Add `RetryPolicy` intentionally (`IsReadOnly`, `IsSafeRetry`).
- Add `CachePolicy` only for genuinely stable read paths.

4. Add a domain skill for planning guidance.
- Implement `skills.Skill` in `internal/skills`.
- Keep side effects in tools; use skill for planner hints and workflow guidance.
- Activate through `AGENT_CORE_SKILLS` exactly like standard reusable skills.

5. Add pipeline mutations only for structural stage changes.
- Use `stages.InsertBefore/After`, `Replace`, `Remove` for topology changes.
- Keep custom stages deterministic and cheap; avoid network I/O there.

6. Add phase interceptors for deterministic policies.
- `ENRICH_CONTEXT`: inject private context and retrieved docs.
- `AFTER_TOOL_EXECUTION`: derive compact state for next planner step.
- `STOP_CHECK` and `FINALIZE`: enforce business stop rules and diagnostics.

7. Add tool execution interceptors for centralized governance.
- Policy interceptor: block tool names/prefixes.
- Rewrite interceptor: normalize/fill args before execution.
- Fallback interceptor: return deterministic safe payloads for selected read-only failures.

8. Aggregate everything in one bundle object.
- Expose `RegisterTools`, `RegisterSkills`, `RegisterInterceptors`, `PipelineMutations`, `PromptAdditions`.
- Keep runtime wiring composable and minimal.

Reference skeleton:

```go
type Bundle struct {
    Tools  []tools.Tool
    Skills []skills.Skill
}

func NewBundle() *Bundle { /* build domain components */ }
func (b *Bundle) RegisterTools(reg *tools.Registry) error { /* ... */ }
func (b *Bundle) RegisterSkills(reg *skills.Registry) error { /* ... */ }
func (b *Bundle) RegisterInterceptors(ir *core.InterceptorRegistry) { /* ... */ }
func (b *Bundle) PipelineMutations() []stages.PipelineMutation { /* ... */ }
func (b *Bundle) PromptAdditions() []string { /* ... */ }
```

9. Wire the bundle behind one feature flag in `cmd/agent-core/runtime.go`.
- Instantiate only when the flag is enabled.
- Register/apply skills, then register tools/interceptors.
- Merge prompt additions and pipeline mutations into runtime config.

10. Configure env explicitly.
- Enable bundle: `AGENT_CORE_BUNDLE_ENABLED=true`.
- Enable skills: `AGENT_CORE_SKILLS=ops,<your_skill>`.
- Keep explicit tool allowlist and per-tool error policy.
- Tune guardrails and output schema to downstream contract.

11. Build a rollout test matrix.
- Unit tests per tool (schema, happy path, edge cases, retry/cache flags).
- Unit tests for interceptors and pipeline mutations.
- Runtime integration tests for end-to-end flow and stop reasons.
- HTTP contract tests for `/v1/agent/run`.
- Negative tests for blocked input, blocked tools, fallback branches, and mutating tool failure behavior.

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
$env:AGENT_CORE_MCP_SERVERS='[{"name":"obs","base_url":"https://mcp-observability.internal","enabled":true,"oauth2_1":{"enabled":true,"issuer_url":"https://idp.internal","client_id":"agent-core","audience":"mcp-observability","scopes":["mcp.read","mcp.call"],"auth_method":"client_secret_basic"}}]'
$env:MCP_OAUTH_CLIENT_SECRET_OBS="<secret>"
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

- `core/pipeline_test.go` — stage ordering and pipeline mutation behavior.
- `pkg/stages/*_test.go` — isolated stage tests and extensible stage ordering.
- `pkg/planner/*_test.go` — JSON retries and tool-selection policy.
- `pkg/tools/*_test.go` — allowlist/security/retry/cache/dedup.
- `pkg/llm/*_test.go` — provider cache/stream/token metrics.
- `pkg/telemetry/*_test.go` — context propagation and telemetry backend behavior.
- `cmd/agent-core/server_test.go` — HTTP contract and first-only behavior.

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
