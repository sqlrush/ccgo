.PHONY: test audit

test:
	go test ./...

audit:
	go run ./tools/sourceaudit -source /Users/sqlrush/agent/claude-code
