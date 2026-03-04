.PHONY: build run serve test test-profile lint vet fmt tidy

build:
	go build ./...

run:
	go run ./cmd/agent-core run --input "What time is it?"

serve:
	go run ./cmd/agent-core serve --addr ":8080" --first-only=true

test:
	go test -cover ./...

lint:
	golangci-lint run

vet:
	go vet ./...

fmt:
	go fmt ./...

imports:
	goimports -l -w .

tidy:
	go mod tidy
