# agent-core

> [English](README.md) | **Русский**

`agent-core` — production-oriented каркас для построения бизнес AI-агентов на Go.

Проект уже содержит:

- исполняемое ядро агента с циклом `observe -> plan -> act -> reflect -> stop`;
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

### Высокоуровневый поток

1. Вход (`CLI`/`HTTP`) -> `RunInput`.
2. Создается/восстанавливается сессионный контекст (`session_id`, `correlation_id`).
3. `memory.BuildContext()` формирует наблюдение для планировщика.
4. `planner.Plan()` возвращает JSON-действие.
5. `guardrails.ValidateAction()` проверяет безопасность.
6. `agent.act()` выполняет `tool` или завершает `final`.
7. Результаты шага пишутся в память/state/telemetry.
8. Финальный ответ валидируется (`output policy + optional json schema`).
9. Возвращается `RunResult` (`api_version=v1`) и сохраняется runtime snapshot.

### Stop conditions

Цикл завершится при одном из условий:

- `planner` вернул `final` или `done=true`;
- guardrails достигли лимита (`steps/time/tool_calls/output_size`);
- обнаружено повторение одного и того же действия > 3 раз (`repeated_action_detected`);
- не удалось восстановиться после ошибок планировщика/инструментов/валидации.

---

## 5) Карта кодовой базы

### Точка входа

- `cmd/agent-core/main.go`
  - сборка runtime зависимостей;
  - CLI команды `run` и `serve`;
  - wiring: `config -> llm -> memory -> tools -> skills -> mcp -> planner -> guardrails -> agent`.
- `cmd/agent-core/server.go`
  - HTTP API (`/healthz`, `/v1/agent/run`) и регистрация Web UI роутов;
  - JSON decoding c ограничением размера и `DisallowUnknownFields`.
- `cmd/agent-core/webui.go`
  - одностраничный встроенный интерфейс для теста агента;
  - отправка запросов в `/v1/agent/run` напрямую из браузера.

### Конфигурация

- `config/config.go`
  - типизированный `Config` + `DefaultConfig()`;
  - загрузка env, derived defaults, валидация;
  - парсинг `AGENT_CORE_MCP_SERVERS` и `AGENT_CORE_AGENT_TOOL_ERROR_FALLBACK`.

### Агент и runtime контракты

- `internal/agent/agent.go`
  - основной цикл, retries планировщика, tool error policy, output validation.
- `internal/agent/events.go`
  - наблюдаемые события (`run_started`, `step_planned`, `tool_failed`, ...).
- `internal/agent/snapshot.go`, `kv_snapshot_store.go`
  - контракт `SnapshotStore` и реализация поверх `state.Store`.
- `internal/agent/contract_notes.go`
  - журнал изменений публичного контракта `v1`.

### Планировщик

- `internal/planner/planner.go`
  - строгая JSON-схема action;
  - `ChatJSON` + semantically validate + policy validate + retries.
- `internal/planner/tool_policy.go`
  - policy выбора инструментов из текущего каталога.

### Инструменты

- `internal/tools/registry.go`
  - allowlist/denylist;
  - timeout + concurrency limiter;
  - retry/backoff;
  - дедуп mutating-вызовов;
  - read-cache (локальный + shared backplane);
  - валидация input/output JSON schema.
- `internal/tools/kv.go`, `time_now.go`, `http_get.go`
  - базовые встроенные инструменты.

### LLM слой

- `internal/llm/factory.go`
  - выбор провайдера (`openai`, `openrouter`, `ollama`, `lmstudio`).
- `internal/llm/langchain_provider.go`
  - вызовы модели через `langchaingo`;
  - circuit breaker, retry/backoff, cache, stream;
  - token-usage metrics (`provider_native` или `estimated`).

### Память, state, guardrails

- `internal/memory/memory.go`
  - short-term window + long-term recall;
  - token budget trimming;
  - write policy + redaction.
- `internal/state/store.go`, `session_scope.go`
  - thread-safe KV, atomic file persistence, session namespace.
- `internal/guardrails/guardrails.go`
  - лимиты шагов, времени, tool calls, tool output bytes.

### Безопасность и валидация выхода

- `internal/output/*`
  - policy validator + schema validator + compose.
- `internal/redact/redact.go`
  - маскирование ключей, bearer/JWT, email, private keys.
- `internal/apperrors/errors.go`
  - типизированные ошибки и HTTP mapping.

### Интеграции и расширяемость

- `internal/skills/*`
  - skill registry, встроенный `ops` skill.
- `internal/mcp/mcp.go`
  - импорт удаленных MCP tools как `mcp.<server>.<tool>`.
- `internal/cache/backplane.go`
  - `InMemoryBackplane` и `FileBackplane` для shared cache.

### Телеметрия

- `internal/telemetry/*`
  - session/correlation context;
  - context-aware logger;
  - tracer/metrics interfaces;
  - debug artifacts sink.

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

Сейчас в коде есть:

- `ops` (`internal/skills/ops.go`)
  - регистрирует `time.now` и `http.get`;
  - добавляет prompt hint для планировщика.

Активные skills задаются через `AGENT_CORE_SKILLS` (CSV).

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
  - KV пример:
    ```text
    name=local,base_url=http://localhost:8787,enabled=true
    ```
- secret fallback: `MCP_TOKEN_<SERVER_NAME>`

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

`internal/memory` использует 2 слоя:

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

`internal/state/KVStore`:

- потокобезопасный KV;
- атомарная запись в файл;
- versioned формат persisted state;
- helpers с context (`PutWithContext`, `GetWithContext`, ...);
- keys namespaced по сессии через `state.NamespacedKey`.

### Cache backplane

`internal/cache` позволяет шарить кэш между инстансами процесса:

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

### 11.1 Добавление нового инструмента

Шаги:

1. Реализовать интерфейс `tools.Tool`.
2. (Опционально) реализовать `RetryPolicy` и `CachePolicy`.
3. Зарегистрировать tool в `buildRuntime` или внутри skill.
4. Добавить имя инструмента в `AGENT_CORE_TOOLS_ALLOWLIST`.

Пример:

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

### 11.2 Добавление нового skill

`skill` = пакет инструментов + prompt additions.

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

Дальше:

1. `skillRegistry.Register(&SupportSkill{})`
2. `AGENT_CORE_SKILLS=ops,support`

### 11.3 Подключение MCP инструментов

1. Включить `AGENT_CORE_MCP_ENABLED=true`
2. Задать `AGENT_CORE_MCP_SERVERS`
3. (Опционально) токены через `MCP_TOKEN_<SERVER>`

После импорта инструменты будут доступны как:

- `mcp.<server>.<tool>`

### 11.4 Контракт финального ответа (JSON Schema)

Можно заставить агента возвращать строго структурированный ответ:

```powershell
$env:AGENT_CORE_OUTPUT_JSON_SCHEMA='{"type":"object","required":["answer","confidence"],"properties":{"answer":{"type":"string"},"confidence":{"type":"number","minimum":0,"maximum":1}},"additionalProperties":false}'
```

Если ответ невалиден:

- агент делает до `AGENT_CORE_OUTPUT_VALIDATION_RETRIES` повторов;
- в память добавляется системная подсказка о необходимости скорректировать финальный ответ.

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
$env:AGENT_CORE_MCP_SERVERS='[{"name":"obs","base_url":"http://mcp-observability.internal","enabled":true}]'
$env:MCP_TOKEN_OBS="<secret>"
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

- `internal/agent/*_test.go` — жизненный цикл агента, tool error policy, snapshot restore.
- `internal/planner/planner_test.go` — JSON retries и tool selection policy.
- `internal/tools/*_test.go` — allowlist/security/retry/cache/dedup.
- `internal/llm/*_test.go` — provider cache/stream/token metrics.
- `cmd/agent-core/server_test.go` — HTTP контракт и first-only.

Golden fixture для стабильного runtime-контракта:

- `internal/agent/testdata/run_result.golden.json`

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
