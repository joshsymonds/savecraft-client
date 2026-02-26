# Generate protobuf code for Go + TypeScript
proto:
    buf generate

# Lint protobuf definitions
proto-lint:
    buf lint

# Check protobuf breaking changes against main
proto-breaking:
    buf breaking --against '.git#branch=main'

# Run all Go tests with 80% coverage enforcement
test-go:
    #!/usr/bin/env bash
    set -euo pipefail
    # Find packages that have test files
    pkgs=$(go list ./internal/... | while read -r pkg; do
        dir=$(go list -f '{{ "{{" }}.Dir{{ "}}" }}' "$pkg")
        if ls "$dir"/*_test.go &>/dev/null; then echo "$pkg"; fi
    done)
    if [[ -z "$pkgs" ]]; then echo "No test packages found"; exit 1; fi
    output=$(echo "$pkgs" | xargs go test -cover)
    echo "$output"
    fail=0
    while IFS= read -r line; do
        if [[ "$line" =~ coverage:\ ([0-9]+)\.[0-9]+%\ of\ statements ]]; then
            pct="${BASH_REMATCH[1]}"
            if (( pct < 80 )); then
                pkg=$(echo "$line" | awk '{print $2}')
                echo "FAIL: $pkg coverage below 80%"
                fail=1
            fi
        fi
    done <<< "$output"
    if (( fail )); then exit 1; fi

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
    golangci-lint run ./...
    deadcode -test ./...

# Lint Worker (TypeScript)
lint-worker:
    cd worker && npx eslint .

# Format Go code
fmt-go:
    goimports -w .

# Format Worker (TypeScript)
fmt-worker:
    cd worker && npx prettier --write 'src/**/*.ts' 'test/**/*.ts'

# Check Worker formatting
fmt-worker-check:
    cd worker && npx prettier --check 'src/**/*.ts' 'test/**/*.ts'

# Build a single plugin: just build-plugin echo
build-plugin name:
    cd plugins/{{name}} && just build

# Build all plugins
build-plugins:
    @for dir in plugins/*/; do just build-plugin "$(basename "$dir")"; done

# Check everything: lint, generate, format, test
check: proto-lint proto lint-go lint-worker fmt-worker-check test
