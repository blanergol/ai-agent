# agent-core

> [English](README.md) | **Русский**

`agent-core` — production-oriented каркас для построения бизнес AI-агентов на Go.

Проект уже содержит:

- исполняемое ядро агента с циклом `observe -> enrich_context -> plan -> act -> reflect -> stop`;
- строгий JSON-планировщик действий (`tool | final | noop`);
- реестр инструментов с политиками безопасности, retry/backoff, кэшированием и дедупликацией;
- управление памятью (short-term + long-term), state/snapshot, guardrails;
- HTTP API и CLI для запуска;
- расширяемость через skills и MCP-серверы;
- базовую телеметрию (logs/tracing/metrics/artifacts) и redaction чувствительных данных.

---

## 1) Для чего этот каркас

`agent-core` решает типовую задачу бизнес-агента:

1. принять задачу пользователя;
2. выбрать следующий безопасный шаг (вызов инструмента или финальный ответ);
3. выполнить действие с контролем лимитов и ошибок;
4. запомнить релевантный контекст;
5. отдать валидированный результат и диагностику.

Ключевая идея: разделить агент на небольшие, тестируемые компоненты с жесткими контрактами.

---

## 2) Быстрый старт

### Требования

- Go `1.25.1` (см. `go.mod`)
- для `openai` провайдера: `OPENAI_API_KEY` или `AGENT_CORE_LLM_OPENAI_API_KEY`
- для `openrouter` провайдера: `OPENROUTER_API_KEY` или `AGENT_CORE_LLM_OPENROUTER_API_KEY`

### Установка и тесты

```bash
go mod tidy
go test ./...
```

### Пример 1. Один запуск через CLI

```powershell
$env:AGENT_CORE_LLM_PROVIDER="openai"
$env:AGENT_CORE_LLM_MODEL="gpt-4o-mini"
$env:OPENAI_API_KEY="<secret>"
go run ./cmd/agent-core run --input "Который сейчас UTC-время?"
```

Пример через OpenRouter:

```powershell
$env:AGENT_CORE_LLM_PROVIDER="openrouter"
$env:AGENT_CORE_LLM_MODEL="openai/gpt-4o-mini"
$env:OPENROUTER_API_KEY="<secret>"
$env:AGENT_CORE_LLM_OPENROUTER_HTTP_REFERER="http://localhost"
$env:AGENT_CORE_LLM_OPENROUTER_APP_TITLE="agent-core-local"
go run ./cmd/agent-core run --input "Который сейчас UTC-время?"
```

Полезные флаги `run`:

- `--provider` (`openai|openrouter|ollama|lmstudio`)
- `--model`
- `--debug`
- `--input` (если пусто, агент читает stdin)
- `--user-sub`
- `--session-id`
- `--correlation-id`

### Пример 2. HTTP сервер

```powershell
go run ./cmd/agent-core serve --addr ":8080" --first-only=true
```

Включение встроенного Web UI (одна страница для ручного теста):

```powershell
$env:AGENT_CORE_WEB_UI_ENABLED="true"
go run ./cmd/agent-core serve --addr ":8080" --first-only=false
```

После запуска UI доступен по `http://localhost:8080/` (также `http://localhost:8080/ui`).

Пример запроса:

```bash
curl -X POST http://localhost:8080/v1/agent/run \
  -H "Content-Type: application/json" \
  -d '{"input":"Который сейчас UTC-время?"}'
```

Флаги `serve`:

- `--addr` (по умолчанию `:8080`)
- `--first-only` (по умолчанию `true`)
- `--shutdown-timeout-ms` (по умолчанию `5000`)
- `--provider`, `--model`, `--debug`

---

## 3) API контракт

### `GET /` и `GET /ui` (опционально)

Маршрут доступен только при `AGENT_CORE_WEB_UI_ENABLED=true`.

Что есть в интерфейсе:

- сверху: сообщения чата (ответы агента и ваши запросы);
- по центру: расширяемое поле ввода + кнопка `Отправить`;
- снизу: статус выполнения и минимальные технические детали (`HTTP`, `steps`, `tool_calls`, `stop_reason`, `session_id`, `correlation_id`).

### `GET /healthz`

Ответ:

```json
{"status":"ok"}
```

### `POST /v1/agent/run`

Тело запроса:

```json
{
  "input": "строка задачи",
  "user_sub": "optional",
  "session_id": "optional",
  "correlation_id": "optional"
}
```

Примечание по идентификаторам:

- `session_id` — стабильный id диалога/цепочки запросов.
- `correlation_id` — id конкретного запроса в рамках `session_id`.
- `user_sub` — стабильный идентификатор пользователя (лучше псевдоним, не email). При включенном Langfuse попадает в `userId` трейса.

Успешный ответ:

```json
{
  "final_response": "текст ответа",
  "steps": 2,
  "tool_calls": 1,
  "stop_reason": "planner_done",
  "session_id": "9a89...",
  "correlation_id": "7fd1...",
  "api_version": "v1"
}
```

Ошибки:

- тело: `{"error":"..."}` (без внутреннего stack/cause)
- статусы маппятся из typed-ошибок (`BAD_REQUEST`, `VALIDATION`, `RATE_LIMIT`, `TRANSIENT` и т.д.)

Особенность `first-only=true`:

- сервер принимает только первый **успешный** запрос;
- следующий вернет `409 Conflict` с ошибкой `"first request already processed"`;
- после первого успешного запроса инициируется graceful shutdown.

---

## 4) Архитектура исполнения

### Pipeline loop (текущая реализация)

Порядок stage по умолчанию:

1. `observe`
2. `enrich_context`
3. `plan`
4. `act`
5. `reflect`
6. `stop`

Этот stage-loop по-прежнему исполняется в `core.Runtime.Run()` и **не** заменяется пользовательской бизнес-логикой.

### Детерминированные фазы внутри цикла

Каждый stage использует явные фазы и `RunContext.ExecutePhase(...)`:

- `INPUT` (вход в runtime)
- `NORMALIZE` (`sanitize` stage, если включен)
- `GUARDRAILS` (точки валидации в `observe` и `act`)
- `ENRICH_CONTEXT` (`enrich_context` stage)
- `DECIDE_ACTION` (`plan` stage, LLM planner)
- `BEFORE_TOOL_EXECUTION` (`act`, перед вызовом tool)
- `TOOL_EXECUTION` (`act`, вызов через `ToolExecutor`)
- `AFTER_TOOL_EXECUTION` (`act`, после вызова tool)
- `EVALUATE` (`reflect`)
- `STOP_CHECK` (`reflect`)
- `FINALIZE` (`stop`)

Так появляются детерминированные точки вмешательства без изменения семантики базового цикла.

### Модель State, общая для всех фаз

`RunContext.State` (`core.AgentState`) хранит централизованное состояние запуска/итерации:

- `raw_input`, `normalized_input`
- результаты guardrails (счетчики/нарушения)
- детерминированные payload-обогащения в `context`
- `retrieved_docs`
- snapshot памяти для планировщика
- `tool_calls_history` и `tool_results`
- `errors`
- `budgets` (token/time/cost поля)
- trace/debug данные (`phase` traces + iteration metrics)
- `iteration` counter

### Как управляется цикл

Базовый runtime-loop не меняется и управляется кодом возврата stage:

- `StageControlContinue` -> перейти к следующему stage/итерации
- `StageControlRetry` -> повторить цикл с optional backoff
- `StageControlStop` -> собрать `RunResult` и завершить

### Stop conditions

Цикл завершается при одном из условий:

- planner вернул `final` или `done=true`;
- guardrails достигли лимитов (`steps/time/tool_calls/output_size`);
- одно и то же эффективное действие повторилось более 3 раз (`repeated_action_detected`);
- исчерпаны recovery retries планировщика/инструментов/валидации.

---

## 5) Карта кодовой базы

### Слоистая структура

- `core/`
  - контракты каркаса и оркестратор (`RunInput/RunResult`, `Action`, `Stage`, `Pipeline`, `Runtime`);
  - детерминированная фазовая модель (`Phase`), централизованный state (`AgentState`), interceptors, абстракция tool-executor;
  - API композиции stage (`InsertBefore/After`, `Replace`, `Remove`, hooks, middleware);
  - stop/retry модель для stage.
- `pkg/`
  - переиспользуемые библиотеки без зависимости на `internal`/`cmd`:
  - `pkg/apperrors`, `pkg/redact`, `pkg/retry`, `pkg/cache`, `pkg/jsonx`, `pkg/state`, `pkg/llm`, `pkg/telemetry`;
  - runtime-реализации ядра: `pkg/planner`, `pkg/memory`, `pkg/guardrails`, `pkg/output`, `pkg/tools`, `pkg/mcp`, `pkg/stages`, `pkg/skills`.
- `internal/`
  - project-private domain code (business adapters/tools/policies that should not be exported).
  - this repository includes a real incident-response bundle under `internal/*` (tools, skill, pipeline mutations, interceptors).
- `cmd/`
  - транспорты и точки входа:
  - `cmd/agent-core/main.go`: сборка runtime и запуск `run`/`serve`;
  - `cmd/agent-core/runtime.go`: wiring зависимостей (`LLM`, `memory`, `planner`, `tool registry`, `interceptors`, `pipeline`);
  - `cmd/agent-core/server.go`: HTTP API (`/healthz`, `/v1/agent/run`) + optional Web UI.
- `config/`
  - типизированный `Config`, env parsing, валидация и derived defaults;
  - разбор `AGENT_CORE_MCP_SERVERS`, `AGENT_CORE_AGENT_TOOL_ERROR_FALLBACK`, `AGENT_CORE_AGENT_MCP_ENRICHMENT_SOURCES`.

---

## 6) Встроенные инструменты и skills

### Встроенные tools

1. `time.now`
- возвращает UTC в `RFC3339`;
- read-only + safe-retry.

2. `kv.put`
- пишет JSON value в `state` текущей сессии (`session:<session_id>:<key>`);
- mutating, но помечен safe-retry.

3. `kv.get`
- читает JSON value из текущей сессии;
- возвращает `null`, если ключ отсутствует.

4. `http.get` (в `ops` skill)
- только `http/https`;
- только allowlist домены;
- запрет внутренних IP (включая резолв DNS в private ranges);
- лимит ответа и timeout;
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

### Расширения бизнес-pipeline

Есть 3 уровня расширения, от простого к строгому:

1. Stage-level (`pkg/stages` + `stages.PipelineMutation`)
- изменить порядок stage или добавить custom stage (`InsertBefore/After`, `Replace`, `Remove`, `Append`).

2. Phase-level (`core.InterceptorRegistry`)
- регистрировать детерминированную логику до/после любой фазы через `RegisterInterceptor(phase, interceptor)`.
- лучший уровень для policy enforcement, context injection, fallback-логики и корректировки stop conditions.

3. Tool-level (`core.ToolExecutor` + tool interceptors)
- пропускать каждый вызов инструмента через единый executor.
- можно блокировать/переписывать вызовы, добавлять детерминированные retries/fallback, трассировку и routing по политике.

Минимальный шаблон регистрации:

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

Детерминированный пример по умолчанию: `core.MCPContextEnrichment`, выполняется в `ENRICH_CONTEXT` до решения планировщика.

---

## 7) Конфигурация (ENV)

Базовый шаблон: [`.env.example`](./.env.example)

Ниже сгруппированы важные переменные. Для полного поведения ориентируйтесь на `config/DefaultConfig()` и `applyEnv()`.

### Runtime mode

- `AGENT_CORE_MODE=dev|test|prod`
  - `test` автоматически включает:
    - `AGENT_CORE_AGENT_DETERMINISTIC=true`
    - `AGENT_CORE_LLM_DISABLE_JITTER=true`

### LLM

- `AGENT_CORE_LLM_PROVIDER=openai|openrouter|ollama|lmstudio`
- `AGENT_CORE_LLM_MODEL=...`
- `AGENT_CORE_LLM_BASE_URL=...` (`openrouter` по умолчанию использует `https://openrouter.ai/api/v1`)
- `AGENT_CORE_LLM_OPENAI_API_KEY=...` (fallback: `OPENAI_API_KEY`)
- `AGENT_CORE_LLM_OPENROUTER_API_KEY=...` (fallback: `OPENROUTER_API_KEY`, затем `OPENAI_API_KEY`)
- `AGENT_CORE_LLM_OPENROUTER_HTTP_REFERER=...` (опционально, заголовок `HTTP-Referer`)
- `AGENT_CORE_LLM_OPENROUTER_APP_TITLE=...` (опционально, заголовок `X-Title`)
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
- `AGENT_CORE_LLM_CACHE_TTL_MS` (shared/локальный chat cache, `0` отключает)

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
  - JSON пример:
    ```json
    [{"name":"local","base_url":"http://localhost:8787","enabled":true,"token":""}]
    ```
  - OAuth 2.1 JSON пример (client credentials):
    ```json
    [{"name":"obs","base_url":"https://mcp.internal","enabled":true,"oauth2_1":{"enabled":true,"issuer_url":"https://idp.example.com","client_id":"agent-core","audience":"mcp-api","scopes":["mcp.read","mcp.call"],"auth_method":"client_secret_basic"}}]
    ```
  - KV пример:
    ```text
    name=local,base_url=http://localhost:8787,enabled=true
    ```
- secret fallback: `MCP_TOKEN_<SERVER_NAME>`
- oauth secret fallbacks: `MCP_OAUTH_CLIENT_ID_<SERVER_NAME>`, `MCP_OAUTH_CLIENT_SECRET_<SERVER_NAME>`
- детерминированное pre-plan enrichment:
  - `AGENT_CORE_AGENT_MCP_ENRICHMENT_SOURCES` (JSON array), пример:
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

- `AGENT_CORE_STATE_PERSIST_PATH` (файл persisted KV state)
- `AGENT_CORE_STATE_CACHE_BACKPLANE_DIR` (директория shared file cache)
- `AGENT_CORE_STATE_TIMEOUT_MS` (timeout snapshot операций)

Если `CACHE_BACKPLANE_DIR` не указан, но задан `PERSIST_PATH`, путь может быть выведен автоматически.

### Agent / Guardrails

- `AGENT_CORE_AGENT_MAX_STEP_DURATION_MS`
- `AGENT_CORE_AGENT_CONTINUE_ON_TOOL_ERROR`
- `AGENT_CORE_AGENT_TOOL_ERROR_MODE=fail|continue`
- `AGENT_CORE_AGENT_TOOL_ERROR_FALLBACK`
  - JSON: `{"http.get":"continue","crm.write":"fail"}`
  - CSV: `http.get=continue,crm.write=fail`
- `AGENT_CORE_AGENT_MAX_INPUT_CHARS`
- `AGENT_CORE_AGENT_DETERMINISTIC`
- `AGENT_CORE_BUNDLE_ENABLED` (enable internal incident-response bundle in `internal/*`)
- `AGENT_CORE_AGENT_MCP_ENRICHMENT_SOURCES` (JSON array для детерминированных MCP вызовов в `ENRICH_CONTEXT`)
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

- `AGENT_CORE_AUTH_USER_AUTH_HEADER` (по умолчанию `X-User-Sub`)
- `AGENT_CORE_AUTH_OAUTH2_1_ENABLED`
- `AGENT_CORE_AUTH_OAUTH2_1_ISSUER_URL`
- `AGENT_CORE_AUTH_OAUTH2_1_JWKS_URL` (опционально, если используется issuer discovery)
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
  - `true`: включает встроенный web-интерфейс на `/` и `/ui`
  - `false`: UI роуты не регистрируются, доступны только API-эндпоинты

### Langfuse

- `AGENT_CORE_LANGFUSE_ENABLED`
- `AGENT_CORE_LANGFUSE_HOST`
- `AGENT_CORE_LANGFUSE_PUBLIC_KEY`
- `AGENT_CORE_LANGFUSE_SECRET_KEY`
- `AGENT_CORE_LANGFUSE_TIMEOUT_MS`
- `AGENT_CORE_LANGFUSE_SERVICE_NAME`
- `AGENT_CORE_LANGFUSE_SERVICE_VERSION`
- `AGENT_CORE_LANGFUSE_ENVIRONMENT`
- `AGENT_CORE_LANGFUSE_MODEL_PRICES` (JSON map fallback-цен в USD за 1M токенов, пример:
  `{"openai/gpt-4o-mini":{"input_per_1m":0.15,"output_per_1m":0.6}}`)
- fallback compatibility variables:
  - `LANGFUSE_HOST`
  - `LANGFUSE_PUBLIC_KEY`
  - `LANGFUSE_SECRET_KEY`

---

## 8) Память, state и кэш

### Память

`pkg/memory` использует 2 слоя:

1. short-term (`[]llm.Message`)
- окно последних сообщений;
- автоматическая свертка старой части при переполнении.

2. long-term
- интерфейс `LongTermMemory` (по умолчанию in-memory);
- recall `topK` по простому overlap токенов;
- хранение в скоупе текущей `session_id`.

Важные детали:

- tool output добавляется как `RoleTool` и как **untrusted data**;
- контекст ограничивается `token_budget`;
- записи long-term проходят redaction и size trim.

### State

`pkg/state/KVStore`:

- потокобезопасный KV;
- атомарная запись в файл;
- versioned формат persisted state;
- helpers с context (`PutWithContext`, `GetWithContext`, ...);
- keys namespaced по сессии через `state.NamespacedKey`.

Snapshots:

- runtime snapshots persist short-term memory context;
- guardrail counters are reset for each new run and are not restored from previous requests.

### Cache backplane

`pkg/cache` позволяет шарить кэш между инстансами процесса:

- `InMemoryBackplane` — process-local;
- `FileBackplane` — общий file-backed.

Используется в:

- LLM cache (`llm.<provider>` namespace);
- tool dedup/read cache (`tools.dedup`, `tools.read_cache`).

---

## 9) Телеметрия и наблюдаемость

### Логи

`telemetry.ContextLogger` автоматически добавляет:

- `session_id`
- `correlation_id`
- `user_sub_hash` (хеш, не сырой user identifier)

### Tracing

Ключевые span:

- `agent.run`, `agent.plan`, `agent.act`
- `tool.execute`
- `llm.chat`, `llm.chat_stream`
- `memory.*`

Langfuse correlation mapping (через OTLP attributes):

- `langfuse.session.id` <- `session_id`
- `langfuse.user.id` <- `user_sub`
- `agent.session_id` и `agent.correlation_id` остаются как внутренние диагностические атрибуты
- `agent.user_sub_hash` сохраняется для безопасной корреляции в логах без утечки сырого user id
- для generation-span (`llm.chat`, `llm.chat_stream`) агент передаёт:
  - `langfuse.observation.type=generation`
  - `langfuse.observation.model.name`
  - `langfuse.observation.model.parameters` (JSON)
  - `langfuse.observation.usage_details` (JSON с `input/output/total`)
  - `langfuse.observation.cost_details` (JSON с `input/output/total/currency`)
  - `langfuse.observation.completion_start_time` (RFC3339Nano)

### Метрики (через интерфейс)

Примеры имен:

- `agent.run`
- `tool.calls`, `tool.retries`, `tool.latency_ms`
- `llm.calls`, `llm.tokens`, `llm.tokens.total`
- `memory.write`, `memory.recall`, `memory.context.tokens`

### Debug artifacts

Опционально (`AGENT_CORE_LOGGING_DEBUG_ARTIFACTS=true`) пишутся:

- prompt payload;
- response payload;
- state/error payload;

с redaction и ограничением `MAX_CHARS`.

---

## 10) Безопасность и guardrails

### Что контролируется

- allowlist/denylist инструментов;
- лимиты по шагам/времени/вызовам/размеру output;
- timeout и retry политики;
- output schema validation;
- redaction чувствительных данных в логах и артефактах;
- запрет внутренних IP и доменных обходов в `http.get`;
- обработка tool output как недоверенных данных в памяти и prompt.

### Минимальная threat model, покрытая каркасом

- prompt injection (user/tool/memory);
- data exfiltration через logs/output;
- unsafe retries для mutating tools;
- SSRF при HTTP-инструментах;
- каскадные отказы внешнего LLM (retry + circuit breaker).

---

## 11) Расширение каркаса

### 11.1 Управление pipeline-циклом без переписывания loop

Поведение можно безопасно контролировать на трех уровнях:

1. Stage-level (`stages.PipelineMutation`)
- менять порядок/состав stage (reorder/insert/replace/remove).

2. Phase-level (`core.InterceptorRegistry`)
- добавлять детерминированную бизнес-логику до/после фазы (`INPUT`, `ENRICH_CONTEXT`, `STOP_CHECK` и т.д.).

3. Tool-level (`ToolExecutionInterceptor`)
- блокировать/переписывать/fallback-ить tool calls в одном централизованном месте.

### 11.2 Stage-level контроль (мутации pipeline)

```go
pipeline, err := stages.BuildDefaultPipeline(
    stages.FactoryConfig{},
    stages.InsertBefore("plan", myBusinessStage{}),
    stages.InsertAfter("reflect", myAuditStage{}),
)
```

Используйте stage-mutations, когда нужна явная структурная правка порядка stage.

### 11.3 Phase-level контроль (детерминированная бизнес-логика)

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

Phase-interceptors подходят для:

- детерминированного enrichment контекста;
- override stop conditions;
- policy checks до решения LLM;
- переписывания state между итерациями.

### 11.4 Централизованный контроль исполнения tools

`ToolExecutor` - единый gateway выполнения. Базовая реализация делегирует в `pkg/tools.Registry` (allow/deny policy, schema validation, timeout, retries, cache, dedup, logging) и дополнительно поддерживает around-execution interceptors.

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

### 11.5 Детерминированный MCP enrichment до решения planner

Встроенный пример: `core.MCPContextEnrichment` работает в фазе `ENRICH_CONTEXT` и добавляет результаты в `RunContext.State.Context`.

Wiring в runtime:

```go
interceptors := core.NewInterceptorRegistry()
interceptors.RegisterInterceptor(
    core.PhaseEnrichContext,
    core.NewMCPContextEnrichment(resolved.MCPEnrichmentSources),
)
toolExecutor := core.NewRegistryToolExecutor(toolRegistry, interceptors)
```

Конфигурация:

- `AGENT_CORE_AGENT_MCP_ENRICHMENT_SOURCES` (JSON array)
- формат элемента: `name`, `tool_name`, `args`, `required`

### 11.6 Добавить переиспользуемый tool в `pkg/tools` (shared library)

Шаги:

1. Реализовать `tools.Tool`.
2. (Опционально) реализовать `RetryPolicy` и `CachePolicy`.
3. Зарегистрировать инструмент в runtime wiring или внутри skill.
4. Добавить имя в `AGENT_CORE_TOOLS_ALLOWLIST`.

### 11.7 Добавить приватный бизнес-tool в `internal/*` (project-specific)

Используйте этот вариант, когда инструмент содержит приватную доменную логику/адаптеры и не должен становиться публичным API пакетов.

Рекомендуемый layout:

```text
internal/
  support/
    tools/
      ticket_create.go
```

Регистрация в runtime wiring (`cmd/agent-core/runtime.go`):

```go
ticketTool := supporttools.NewTicketCreateTool(ticketClient)
if err := toolRegistry.Register(ticketTool); err != nil {
    return nil, err
}
```

Так архитектура остается чистой:

- доменно-специфичный код остается приватным в `internal/*`;
- core loop, phase control и контракты tool execution остаются без изменений.

### 11.8 Добавить новый skill

`skill` = пакет инструментов + prompt additions + optional pipeline mutations.

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

Дальше:

1. `skillRegistry.Register(&SupportSkill{})`
2. `AGENT_CORE_SKILLS=ops,support`

### 11.9 Подключить MCP tools

1. Включить `AGENT_CORE_MCP_ENABLED=true`
2. Настроить `AGENT_CORE_MCP_SERVERS`
3. Выбрать режим auth для сервера:
   - статический bearer через `token` / `MCP_TOKEN_<SERVER>`
   - OAuth 2.1 client credentials через `oauth2_1` (+ опционально `MCP_OAUTH_CLIENT_SECRET_<SERVER>`)

После импорта инструменты доступны как `mcp.<server>.<tool>`.

### 11.10 Контракт финального ответа (JSON Schema)

```powershell
$env:AGENT_CORE_OUTPUT_JSON_SCHEMA='{"type":"object","required":["answer","confidence"],"properties":{"answer":{"type":"string"},"confidence":{"type":"number","minimum":0,"maximum":1}},"additionalProperties":false}'
```

Если финальный output невалиден:

- агент делает до `AGENT_CORE_OUTPUT_VALIDATION_RETRIES` повторов;
- в память добавляется системная коррекция, чтобы принудить совместимый финальный ответ.

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

## 12) Примеры реализации бизнес AI-агента на этом каркасе

Ниже практические шаблоны, которые можно реализовать без изменения архитектуры ядра.

### Сценарий A: Агент поддержки (L1/L2 triage)

Цель:

- быстро классифицировать обращение;
- проверить профиль клиента;
- предложить решение или завести тикет.

Инструменты:

- read-only: `crm.get_customer`, `kb.search_article`
- mutating: `ticket.create`
- вспомогательно: `time.now`, `kv.put`, `kv.get`

Рекомендованный output schema:

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

Ключевые настройки:

```powershell
$env:AGENT_CORE_SKILLS="ops,support"
$env:AGENT_CORE_TOOLS_ALLOWLIST="time.now,kv.put,kv.get,crm.get_customer,kb.search_article,ticket.create"
$env:AGENT_CORE_AGENT_TOOL_ERROR_MODE="continue"
$env:AGENT_CORE_AGENT_TOOL_ERROR_FALLBACK='ticket.create=fail'
$env:AGENT_CORE_OUTPUT_JSON_SCHEMA='<schema above>'
```

Почему это работает хорошо:

- `ticket.create` можно сделать строгим (`fail`), чтобы не терять инциденты молча;
- read-only инструменты кешируются и повторяются безопасно;
- planner обязан выбирать только инструменты из каталога.

---

### Сценарий B: Агент продаж (qualification + next best action)

Цель:

- квалифицировать лид;
- дать следующий шаг менеджеру;
- опционально создать opportunity.

Инструменты:

- `crm.find_lead`
- `crm.upsert_lead`
- `scoring.score_lead`
- `calendar.find_slots`
- `mail.send_template`

Выход:

- структурированный JSON с `lead_score`, `segment`, `recommended_action`, `draft_message`.

Рекомендуемые guardrails:

```powershell
$env:AGENT_CORE_GUARDRAILS_MAX_STEPS="6"
$env:AGENT_CORE_GUARDRAILS_MAX_TOOL_CALLS="6"
$env:AGENT_CORE_AGENT_MAX_INPUT_CHARS="6000"
$env:AGENT_CORE_TOOLS_MAX_OUTPUT_BYTES="32768"
```

Практический паттерн:

- `calendar.find_slots` и `scoring.score_lead` сделать read-only + cache TTL;
- `crm.upsert_lead` оставить mutating + safe retry только при идемпотентном ключе на стороне CRM.

---

### Сценарий C: E-commerce post-purchase агент

Цель:

- отвечать по статусу заказа;
- рассчитать eligibility возврата;
- запускать процесс возврата при выполнении условий.

Инструменты:

- `order.get`
- `shipment.track`
- `refund.check_policy`
- `refund.create_request`

Особо важно:

- `shipment.track` можно реализовать через безопасный `http.get` и allowlist домены перевозчиков;
- `refund.create_request` держать в режиме `fail` при ошибке инструмента;
- результат ограничить JSON schema, чтобы downstream-API получали предсказуемый формат.

---

### Сценарий D: Ops / Incident response агент (SRE NOC)

Цель:

- принимать алерты;
- собирать контекст (метрики, логи, статус сервисов);
- выдавать оператору структурированный runbook.

Инструменты:

- встроенный `ops` skill (`time.now`, `http.get`)
- MCP инструменты, например:
  - `mcp.monitoring.query`
  - `mcp.logs.search`
  - `mcp.deployments.last_change`

Конфиг:

```powershell
$env:AGENT_CORE_SKILLS="ops"
$env:AGENT_CORE_MCP_ENABLED="true"
$env:AGENT_CORE_MCP_SERVERS='[{"name":"obs","base_url":"https://mcp-observability.internal","enabled":true,"oauth2_1":{"enabled":true,"issuer_url":"https://idp.internal","client_id":"agent-core","audience":"mcp-observability","scopes":["mcp.read","mcp.call"],"auth_method":"client_secret_basic"}}]'
$env:MCP_OAUTH_CLIENT_SECRET_OBS="<secret>"
$env:AGENT_CORE_TOOLS_ALLOWLIST="time.now,http.get,mcp.obs.monitoring.query,mcp.obs.logs.search,mcp.obs.deployments.last_change"
```

Рекомендация по HTTP режиму:

- для сервиса лучше `first-only=false` и внешний orchestrator/reverse proxy.

---

## 13) Production checklist

Перед production запуском проверьте:

1. Настроены allowlist/denylist инструментов.
2. Для mutating tools явно определены retry semantics.
3. Заданы лимиты guardrails под вашу SLA.
4. Включены structured logs и корреляция (`session_id`, `correlation_id`).
5. Проверена output schema совместимость с downstream.
6. Проверен redaction в логах на реальных payload.
7. Для multi-instance включен file backplane.
8. Подготовлены integration/e2e tests на ваших инструментах.

---

## 14) Тестирование и детерминизм

Основные команды:

```bash
go test ./...
go test -covermode=atomic -coverprofile=coverage.out ./...
go tool cover -func=coverage.out
```

Полезные тестовые пакеты:

- `core/pipeline_test.go, pkg/stages/*_test.go` — жизненный цикл агента, tool error policy, short-term snapshot restore.
- `pkg/planner/*_test.go` — JSON retries и tool selection policy.
- `pkg/tools/*_test.go` — allowlist/security/retry/cache/dedup.
- `pkg/llm/*_test.go, pkg/telemetry/*_test.go` — provider cache/stream/token metrics.
- `cmd/agent-core/server_test.go` — HTTP контракт и first-only.

Golden fixture для стабильного runtime-контракта:

- `core run contract is validated by cmd/agent-core/server_test.go`

---

## 15) Ограничения текущего каркаса

Важно учитывать:

- long-term recall сейчас простой (token overlap), без embeddings/vector DB;
- встроенный HTTP API синхронный (нет встроенной очереди задач);
- RBAC/tenant isolation для бизнес-сущностей должны быть реализованы в ваших инструментах и внешних сервисах;
- провайдерные streaming/usage зависят от возможностей backend-а (`provider_native` может быть недоступен).

---

## 16) Рекомендуемый путь внедрения в проект

1. Начать с 2-3 read-only tools + одного mutating action.
2. Зафиксировать output JSON schema для внешних потребителей.
3. Подключить structured telemetry и проверить redaction.
4. Включить MCP только для действительно нужных источников.
5. Затем постепенно расширять skill-пакеты по бизнес-доменам.

---

## 17) Лицензия

См. [`LICENSE`](./LICENSE).

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
  - `AGENT_CORE_LANGFUSE_MODEL_PRICES` (опционально, fallback pricing для cost_details)

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
- Agent emits generation-level usage/cost fields, поэтому в Langfuse доступны:
  - Model Usage
  - Model Costs
  - User Consumption (через `langfuse.user.id` + usage/cost details)
  - Generation Latency Percentiles (на основе generation-span и `completion_start_time`)
- Agent emits trace-level scores через Langfuse Public API (`/api/public/scores`):
  - `agent.run.success` (`1` при валидном финальном ответе, иначе `0`)
  - `agent.run.steps`
  - `agent.run.tool_calls`
- For local logs and non-Langfuse diagnostics, raw `user_sub` is not logged; only `user_sub_hash` is emitted.

Clean re-init of observability data:

```bash
docker compose -f docker-compose.observability.yml down -v
docker compose -f docker-compose.observability.yml up -d
```
