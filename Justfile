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

# Start Worker dev server (Miniflare)
dev-worker:
    cd worker && npx wrangler dev

# Lint Go code
lint-go:
    golangci-lint run ./internal/... ./cmd/...
    go run golang.org/x/tools/cmd/deadcode@latest -test ./internal/... ./cmd/...

# Lint Worker (TypeScript)
lint-worker:
    cd worker && npx eslint .

# Format Go code
fmt-go:
    find internal/ cmd/ plugins/ -name '*.go' -not -path 'internal/proto/*' -print0 | xargs -0 goimports -w

# Format Worker (TypeScript)
fmt-worker:
    cd worker && npx prettier --write 'src/**/*.ts' 'test/**/*.ts'

# Check Worker formatting
fmt-worker-check:
    cd worker && npx prettier --check 'src/**/*.ts' 'test/**/*.ts'

# Build a single plugin: just build-plugin echo
build-plugin name:
    cd plugins/{{name}} && just build

# Generate manifest.json for a plugin from its plugin.toml + built wasm
plugin-manifest name:
    go run ./cmd/plugin-manifest/ plugins/{{name}}

# Build all plugins
build-plugins:
    @for dir in plugins/*/; do just build-plugin "$(basename "$dir")"; done

# Run Web tests
test-web:
    cd web && npm test

# Lint Web (SvelteKit)
lint-web:
    cd web && npx eslint .

# Type-check Web (SvelteKit)
check-web:
    cd web && npm run check

# Format Web (SvelteKit)
fmt-web:
    cd web && npx prettier --write .

# Check Web formatting
fmt-web-check:
    cd web && npx prettier --check .

# Lint marketing site
lint-site:
    cd site && npx eslint .

# Type-check marketing site
check-site:
    cd site && npm run check

# Test marketing site
test-site:
    cd site && npm test

# Format marketing site
fmt-site:
    cd site && npx prettier --write .

# Check marketing site formatting
fmt-site-check:
    cd site && npx prettier --check .

# Lint shell scripts (shellcheck)
lint-sh:
    shellcheck install/install.sh install/test/run-test.sh scripts/generate-plugin-manifest.sh

# Format shell scripts
fmt-sh:
    shfmt -w -i 4 -bn -ci install/install.sh install/test/run-test.sh scripts/generate-plugin-manifest.sh

# Check shell script formatting
fmt-sh-check:
    shfmt -d -i 4 -bn -ci install/install.sh install/test/run-test.sh scripts/generate-plugin-manifest.sh

# Start Web dev server
dev-web:
    cd web && npm run dev

# Start Storybook
storybook:
    cd web && npm run storybook

# Generate Ed25519 keypair for plugin signing
keygen:
    go run ./cmd/savecraft-keygen/

# Sign a file with Ed25519
sign file:
    go run ./cmd/savecraft-sign/ {{file}}

# Verify a file's Ed25519 signature
verify file:
    go run ./cmd/savecraft-verify/ {{file}}

# Sign all compiled WASM plugins
sign-plugins:
    #!/usr/bin/env bash
    set -euo pipefail
    for wasm in plugins/*/*.wasm; do
        [[ -f "$wasm" ]] || continue
        go run ./cmd/savecraft-sign/ "$wasm"
    done

# Cross-compile daemon binary: just build-daemon linux amd64 dev https://api.savecraft.gg
build-daemon os arch version="dev" server_url="https://api.savecraft.gg":
    #!/usr/bin/env bash
    set -euo pipefail
    mkdir -p dist
    CGO_ENABLED=0 GOOS={{os}} GOARCH={{arch}} go build \
        -ldflags "-s -w -X main.version={{version}} -X main.serverURLDefault={{server_url}}" \
        -o "dist/savecraft-daemon-{{os}}-{{arch}}" \
        ./cmd/savecraftd/

# Build daemon for all release platforms
build-daemon-all version="dev" server_url="https://api.savecraft.gg":
    just build-daemon linux amd64 {{version}} {{server_url}}
    just build-daemon linux arm64 {{version}} {{server_url}}
    just build-daemon darwin amd64 {{version}} {{server_url}}
    just build-daemon darwin arm64 {{version}} {{server_url}}
    just build-daemon windows amd64 {{version}} {{server_url}}

# Build and sign test fixtures for the install integration test
install-fixtures version="0.1.0":
    #!/usr/bin/env bash
    set -euo pipefail
    just build-daemon linux amd64 {{version}}
    just build-daemon linux arm64 {{version}}
    # Sign both binaries
    go run ./cmd/savecraft-sign/ dist/savecraft-daemon-linux-amd64
    go run ./cmd/savecraft-sign/ dist/savecraft-daemon-linux-arm64
    # Create fixture directory
    mkdir -p install/test/fixtures/daemon-v{{version}}
    cp dist/savecraft-daemon-linux-amd64     install/test/fixtures/daemon-v{{version}}/
    cp dist/savecraft-daemon-linux-amd64.sig install/test/fixtures/daemon-v{{version}}/
    cp dist/savecraft-daemon-linux-arm64     install/test/fixtures/daemon-v{{version}}/
    cp dist/savecraft-daemon-linux-arm64.sig install/test/fixtures/daemon-v{{version}}/
    # Bake real public key into installer copy
    # DER prefix for Ed25519 public key: 302a300506032b6570032100
    # Use file I/O to avoid bash stripping null bytes from binary key
    tmp_der=$(mktemp)
    printf '\x30\x2a\x30\x05\x06\x03\x2b\x65\x70\x03\x21\x00' > "$tmp_der"
    cat internal/signing/signing_key.pub >> "$tmp_der"
    b64_key=$(base64 -w0 < "$tmp_der")
    rm -f "$tmp_der"
    sed "s|REPLACE_WITH_BASE64_PUBKEY|${b64_key}|" install/install.sh \
        > install/test/fixtures/install.sh
    chmod +x install/test/fixtures/install.sh
    echo "Fixtures ready in install/test/fixtures/"

# Run install integration test in Docker
test-install:
    #!/usr/bin/env bash
    set -euo pipefail
    just install-fixtures
    docker build -t savecraft-install-test install/test/
    docker run --rm savecraft-install-test

# Check Go formatting (non-destructive)
fmt-go-check:
    #!/usr/bin/env bash
    set -euo pipefail
    files=$(find internal/ cmd/ plugins/ -name '*.go' -not -path 'internal/proto/*')
    output=$(echo "$files" | xargs goimports -l)
    if [[ -n "$output" ]]; then
        echo "Files need goimports formatting:"
        echo "$output"
        exit 1
    fi

# Lint everything (mirrors CI lint steps, no tests)
lint: lint-go lint-worker lint-web lint-site lint-sh fmt-go-check fmt-worker-check fmt-web-check fmt-site-check fmt-sh-check check-web check-site

# Run all tests
test: test-go test-worker test-web test-site

# Check everything: lint, generate, format, test
check: proto-lint proto lint test
