# pkg

This directory contains reusable runtime libraries. Packages here form the
framework surface that can be imported from other projects.

## Package List

- `apperrors`: typed error codes and mapping helpers.
- `cache`: shared cache backplane interfaces and file/in-memory implementations.
- `guardrails`: run-scoped guardrail policies (steps, tool calls, time, output size).
- `jsonx`: strict JSON decoding helpers.
- `llm`: LLM provider contracts, clients, and factory wiring.
- `mcp`: MCP tool import bridge.
- `memory`: short-term and long-term memory manager.
- `output`: final response validation (policy + JSON schema).
- `planner`: planning contract implementation and tool-selection policy.
- `redact`: sensitive data redaction helpers.
- `retry`: retry policy primitives.
- `skills`: skill registry and pipeline extension contracts.
- `skills/builtin`: concrete built-in skill implementations.
- `stages`: default pipeline stage implementations and builder/mutations.
- `state`: KV state, session scoping, and runtime snapshot persistence.
- `telemetry`: tracing/logging/metrics/artifacts interfaces and backends.
- `tools`: tool contracts and execution registry/policies.
- `tools/builtin`: concrete built-in tool implementations.

## Unified Structure Rules

Each package should follow the same readability pattern:

1. `doc.go`
   - short package contract and boundaries;
   - what the package owns and what it does not own.
2. `types.go` (when needed)
   - public interfaces and core data types first.
3. `config.go` / `options.go` (when needed)
   - package configuration and construction options.
4. `*.go` implementation files
   - split by domain concern, not by arbitrary size.
5. `*_test.go`
   - behavior-focused unit tests close to implementation.

## Dependency Rules

- `pkg/*` may depend on `core/*` and other `pkg/*`.
- `pkg/*` must not import `internal/*` or `cmd/*`.
- transport and app wiring stay outside `pkg/*`.
