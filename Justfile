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

# Run reference Worker infrastructure tests (copies D2R wasm, then tests WASI shim)
test-reference-worker:
    cd reference && just test

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
plugin-manifest name version="dev":
    go run ./cmd/plugin-manifest/ --version {{version}} plugins/{{name}}

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
    shellcheck install/install.sh install/test/run-test.sh

# Format shell scripts
fmt-sh:
    shfmt -w -i 4 -bn -ci install/install.sh install/test/run-test.sh

# Check shell script formatting
fmt-sh-check:
    shfmt -d -i 4 -bn -ci install/install.sh install/test/run-test.sh

# Start install Worker dev server
dev-install:
    cd install/worker && npx wrangler dev

# Deploy install Worker: just deploy-install staging
deploy-install env:
    cd install/worker && npx wrangler deploy --env {{env}}

# Upload install script to R2: just upload-install staging
upload-install env:
    #!/usr/bin/env bash
    set -euo pipefail
    if [[ "{{env}}" == "production" ]]; then
        bucket="savecraft-install"
    else
        bucket="savecraft-install-staging"
    fi
    cd install/worker
    npx wrangler r2 object put "${bucket}/curl/install.sh" --file ../install.sh --content-type "text/x-shellscript" --remote

# Upload daemon binaries to R2: just upload-daemon staging savecraft-staging
upload-daemon env app_name="savecraft":
    #!/usr/bin/env bash
    set -euo pipefail
    if [[ "{{env}}" == "production" ]]; then
        bucket="savecraft-install"
    else
        bucket="savecraft-install-staging"
    fi
    cd install/worker
    for f in ../../dist/{{app_name}}-daemon-* ../../dist/{{app_name}}-tray-*; do
        name="$(basename "$f")"
        key="daemon/${name}"
        echo "Uploading ${key}..."
        npx wrangler r2 object put "${bucket}/${key}" --file "$f" --content-type "application/octet-stream" --remote
    done

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

# Cross-compile daemon binary: just build-daemon linux amd64
# Daemon is always CGO_ENABLED=0 — no GUI dependencies.
build-daemon os arch version="dev" server_url="https://api.savecraft.gg" install_url="https://install.savecraft.gg" app_name="savecraft" status_port="9182" frontend_url="https://savecraft.gg":
    #!/usr/bin/env bash
    set -euo pipefail
    mkdir -p dist
    ldflags="-s -w -X main.version={{version}} -X main.serverURLDefault={{server_url}} -X main.installURLDefault={{install_url}} -X main.appName={{app_name}} -X main.statusPortDefault={{status_port}} -X main.frontendURLDefault={{frontend_url}}"
    output="dist/{{app_name}}-daemon-{{os}}-{{arch}}"
    if [[ "{{os}}" == "windows" ]]; then
        output="${output}.exe"
    fi
    CGO_ENABLED=0 GOOS={{os}} GOARCH={{arch}} go build \
        -ldflags "${ldflags}" \
        -o "${output}" \
        ./cmd/savecraftd/

# Build daemon for all release platforms
build-daemon-all version="dev" server_url="https://api.savecraft.gg" install_url="https://install.savecraft.gg" app_name="savecraft" status_port="9182" frontend_url="https://savecraft.gg":
    just build-daemon linux amd64 {{version}} {{server_url}} {{install_url}} {{app_name}} {{status_port}} {{frontend_url}}
    just build-daemon linux arm64 {{version}} {{server_url}} {{install_url}} {{app_name}} {{status_port}} {{frontend_url}}
    just build-daemon darwin amd64 {{version}} {{server_url}} {{install_url}} {{app_name}} {{status_port}} {{frontend_url}}
    just build-daemon darwin arm64 {{version}} {{server_url}} {{install_url}} {{app_name}} {{status_port}} {{frontend_url}}
    just build-daemon windows amd64 {{version}} {{server_url}} {{install_url}} {{app_name}} {{status_port}} {{frontend_url}}

# Cross-compile tray binary: just build-tray linux amd64
# systray uses pure Go (dbus) on Linux, WinAPI on Windows — CGO only needed for macOS (Cocoa).
# Windows gets -H=windowsgui to suppress the console window.
build-tray os arch app_name="savecraft":
    #!/usr/bin/env bash
    set -euo pipefail
    mkdir -p dist
    cgo=0
    ldflags="-s -w"
    output="dist/{{app_name}}-tray-{{os}}-{{arch}}"
    if [[ "{{os}}" == "darwin" ]]; then
        cgo=1
    elif [[ "{{os}}" == "windows" ]]; then
        ldflags="${ldflags} -H=windowsgui"
        output="${output}.exe"
    fi
    CGO_ENABLED="${cgo}" GOOS={{os}} GOARCH={{arch}} go build \
        -ldflags "${ldflags}" \
        -o "${output}" \
        ./cmd/savecraft-tray/

# Build tray for all release platforms
build-tray-all app_name="savecraft":
    just build-tray linux amd64 {{app_name}}
    just build-tray linux arm64 {{app_name}}
    just build-tray darwin amd64 {{app_name}}
    just build-tray darwin arm64 {{app_name}}
    just build-tray windows amd64 {{app_name}}

# Build Windows MSI installer (requires WiX v4: dotnet tool install --global wix)
build-msi version="1.0.0" app_name="savecraft":
    wix build \
        -d Version={{version}} \
        -d DaemonPath=dist/{{app_name}}-daemon-windows-amd64.exe \
        -d TrayPath=dist/{{app_name}}-tray-windows-amd64.exe \
        -o dist/{{app_name}}.msi \
        install/windows/savecraft.wxs

# Run install Worker tests
test-install-worker:
    cd install/worker && npm test

# Run install integration test in Docker
test-install-docker:
    docker build -t savecraft-install-test -f install/test/Dockerfile install/
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
test: test-go test-worker test-reference-worker test-web test-site test-install-worker test-install-docker

# Check everything: lint, generate, format, test
check: proto-lint proto lint test
