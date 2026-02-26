# Generate protobuf code for Go + TypeScript
proto:
    buf generate

# Lint protobuf definitions
proto-lint:
    buf lint

# Check protobuf breaking changes against main
proto-breaking:
    buf breaking --against '.git#branch=main'

# Run all Go tests
test-go:
    go test ./...

# Run Go tests with race detector
test-go-race:
    go test -race ./...

# Run Worker tests
test-worker:
    cd worker && npm test

# Run all tests
test: test-go test-worker

# Start Worker dev server (Miniflare)
dev-worker:
    cd worker && npx wrangler dev

# Lint Go code
lint-go:
    staticcheck ./...
    go vet ./...

# Format Go code
fmt-go:
    goimports -w .

# Check everything: lint, generate, test
check: proto-lint proto lint-go test
