# Parity Test Harness

This directory is the home for black-box and golden tests that prove ccgo behavior
matches Claude Code behavior.

Initial scope:

- CLI stdout/stderr/exit code fixtures.
- SDK JSON/NDJSON event fixtures.
- Tool input/output/error fixtures.
- Session JSONL fixtures.
- Settings and MCP config parser fixtures.

Use `Golden.Assert` from `harness.go` for deterministic fixture checks. Set
`UPDATE_GOLDEN=1` when intentionally refreshing fixtures.
