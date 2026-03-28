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

# Run Worker tests (4 parallel shards, each with its own Miniflare)
test-worker:
    cd worker && npm run test:shard

# Run reference Worker infrastructure tests (copies D2R wasm, then tests WASI shim)
test-reference-worker:
    cd reference && just test

# Start Worker dev server (Miniflare)
dev-worker:
    cd worker && npx wrangler dev

# Lint Go code
lint-go:
    golangci-lint run ./internal/... ./cmd/...
    deadcode -test ./internal/... ./cmd/... ./plugins/...

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
        ldflags="${ldflags} -H=windowsgui"
        output="${output}.exe"
    fi
    CGO_ENABLED=0 GOOS={{os}} GOARCH={{arch}} go build \
        -ldflags "${ldflags}" \
        -o "${output}" \
        ./cmd/savecraftd/

# Build daemon for all release platforms
build-daemon-all version="dev" server_url="https://api.savecraft.gg" install_url="https://install.savecraft.gg" app_name="savecraft" status_port="9182" frontend_url="https://my.savecraft.gg":
    just build-daemon linux amd64 {{version}} {{server_url}} {{install_url}} {{app_name}} {{status_port}} {{frontend_url}}
    just build-daemon linux arm64 {{version}} {{server_url}} {{install_url}} {{app_name}} {{status_port}} {{frontend_url}}
    just build-daemon darwin amd64 {{version}} {{server_url}} {{install_url}} {{app_name}} {{status_port}} {{frontend_url}}
    just build-daemon darwin arm64 {{version}} {{server_url}} {{install_url}} {{app_name}} {{status_port}} {{frontend_url}}
    just build-daemon windows amd64 {{version}} {{server_url}} {{install_url}} {{app_name}} {{status_port}} {{frontend_url}}

# Cross-compile tray binary: just build-tray linux amd64
# systray uses pure Go (dbus) on Linux, WinAPI on Windows — CGO only needed for macOS (Cocoa).
# Windows gets -H=windowsgui to suppress the console window.
build-tray os arch app_name="savecraft" status_port="9182" frontend_url="https://my.savecraft.gg":
    #!/usr/bin/env bash
    set -euo pipefail
    mkdir -p dist
    cgo=0
    pkg="main"
    ldflags="-s -w"
    ldflags="${ldflags} -X ${pkg}.defaultStatusPort={{status_port}}"
    ldflags="${ldflags} -X ${pkg}.defaultFrontendURL={{frontend_url}}"
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

# Build tray for all release platforms (Windows only — Linux uses systemd, no tray)
build-tray-all app_name="savecraft" status_port="9182" frontend_url="https://my.savecraft.gg":
    just build-tray windows amd64 {{app_name}} {{status_port}} {{frontend_url}}

# Build Windows MSI installer (requires WiX v5: dotnet tool install --global wix --version 5.0.2 + wix extension add WixToolset.Util.wixext/5.0.2)
build-msi version="1.0.0" app_name="savecraft":
    wix build \
        -arch x64 \
        -d Version={{version}} \
        -d DaemonPath=dist/{{app_name}}-daemon-windows-amd64.exe \
        -d TrayPath=dist/{{app_name}}-tray-windows-amd64.exe \
        -o dist/{{app_name}}.msi \
        -ext WixToolset.Util.wixext \
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

# Lint everything in parallel (mirrors CI lint steps, no tests)
lint:
    #!/usr/bin/env bash
    set -uo pipefail
    tmpdir=$(mktemp -d)
    trap 'rm -rf "$tmpdir"' EXIT
    pids=()
    names=()
    run() {
        local name=$1; shift
        "$@" >"$tmpdir/$name.out" 2>&1 &
        pids+=($!)
        names+=("$name")
    }
    run lint-go        just lint-go
    run lint-worker    just lint-worker
    run lint-web       just lint-web
    run lint-site      just lint-site
    run lint-sh        just lint-sh
    run fmt-go-check   just fmt-go-check
    run fmt-worker     just fmt-worker-check
    run fmt-web        just fmt-web-check
    run fmt-site       just fmt-site-check
    run fmt-sh         just fmt-sh-check
    run check-web      just check-web
    run check-site     just check-site
    failed=0
    for i in "${!pids[@]}"; do
        if ! wait "${pids[$i]}"; then
            echo "==> FAIL: ${names[$i]}"
            cat "$tmpdir/${names[$i]}.out"
            failed=1
        else
            echo "==> OK: ${names[$i]}"
        fi
    done
    exit $failed

# Run all tests in parallel
test:
    #!/usr/bin/env bash
    set -uo pipefail
    tmpdir=$(mktemp -d)
    trap 'rm -rf "$tmpdir"' EXIT
    pids=()
    names=()
    run() {
        local name=$1; shift
        "$@" >"$tmpdir/$name.out" 2>&1 &
        pids+=($!)
        names+=("$name")
    }
    run test-go              just test-go
    run test-worker          just test-worker
    run test-reference       just test-reference-worker
    run test-web             just test-web
    run test-site            just test-site
    run test-install-worker  just test-install-worker
    run test-install-docker  just test-install-docker
    failed=0
    for i in "${!pids[@]}"; do
        if ! wait "${pids[$i]}"; then
            echo "==> FAIL: ${names[$i]}"
            cat "$tmpdir/${names[$i]}.out"
            failed=1
        else
            echo "==> OK: ${names[$i]}"
        fi
    done
    exit $failed

# Update MTGA reference data (local game data + remote D1/Vectorize): just update-mtga staging
update-mtga env:
    #!/usr/bin/env bash
    set -euo pipefail
    cf_account="cc0a94bb7aff760efd48b49ce983fe97"
    if [[ "{{env}}" == "production" ]]; then
        d1="df241bb0-9b7d-48e5-a4d4-f84ebf09e6e5"
        rules_vec="mtga-rules"
        cards_vec="mtga-cards"
    elif [[ "{{env}}" == "staging" ]]; then
        d1="0147892e-82e6-413e-a0ef-52f6d8787fdf"
        rules_vec="mtga-rules-staging"
        cards_vec="mtga-cards-staging"
    else
        echo "Usage: just update-mtga staging|production" >&2
        exit 1
    fi

    # Phase 0: mtga-carddb must complete first — it populates mtga_cards from
    # the MTGA client database. scryfall-fetch enriches these rows.
    db=".reference/mtga-carddb/Raw_CardDatabase.mtga"
    if [ ! -f "$db" ]; then
        echo "MTGA card database not found at $db" >&2
        echo "Copy Raw_CardDatabase_*.mtga from your MTGA install:" >&2
        echo "  MTGA_Data/Downloads/Raw/Raw_CardDatabase_*.mtga" >&2
        echo "To: .reference/mtga-carddb/Raw_CardDatabase.mtga" >&2
        exit 1
    fi
    echo "==> Phase 0: MTGA card data ({{env}})"
    go run ./plugins/mtga/tools/mtga-carddb/ \
        --card-db="$db" --cf-account-id="$cf_account" --d1-database-id="$d1" \
        --cf-api-token="$CLOUDFLARE_API_TOKEN" 2>&1 | sed 's/^/  [carddb] /'

    echo "==> Phase 1: rules + scryfall enrichment (parallel, {{env}})"
    go run ./plugins/mtga/tools/rules-fetch/ \
        --cf-account-id="$cf_account" --d1-database-id="$d1" --vectorize-index="$rules_vec" 2>&1 | sed 's/^/  [rules] /' &
    pid_rules=$!
    go run ./plugins/mtga/tools/scryfall-fetch/ \
        --cf-account-id="$cf_account" --d1-database-id="$d1" --vectorize-index="$cards_vec" 2>&1 | sed 's/^/  [cards] /' &
    pid_cards=$!

    fail=0
    wait $pid_rules || fail=1
    wait $pid_cards || fail=1
    if [ $fail -ne 0 ]; then
        echo "Phase 1 failed" >&2
        exit 1
    fi

    echo "==> Phase 2: card roles ({{env}})"
    go run ./plugins/mtga/tools/tagger-fetch/ \
        --cf-account-id="$cf_account" --d1-database-id="$d1" 2>&1 | sed 's/^/  [roles] /'

    echo "==> Phase 3: draft ratings ({{env}})"
    go run ./plugins/mtga/tools/17lands-fetch/ \
        --cf-account-id="$cf_account" --d1-database-id="$d1" 2>&1 | sed 's/^/  [17lands] /'

    echo "==> Done ({{env}})"

# Retry failed D1 imports from cached SQL files (no CSV reprocessing)
update-mtga-retry env:
    #!/usr/bin/env bash
    set -euo pipefail
    cf_account="cc0a94bb7aff760efd48b49ce983fe97"
    if [[ "{{env}}" == "production" ]]; then
        d1="df241bb0-9b7d-48e5-a4d4-f84ebf09e6e5"
    elif [[ "{{env}}" == "staging" ]]; then
        d1="0147892e-82e6-413e-a0ef-52f6d8787fdf"
    else
        echo "Usage: just update-mtga-retry staging|production" >&2
        exit 1
    fi

    echo "==> Retrying tagger roles ({{env}})"
    go run ./plugins/mtga/tools/tagger-fetch/ \
        --retry --cf-account-id="$cf_account" --d1-database-id="$d1" 2>&1 | sed 's/^/  [roles] /'

    echo "==> Retrying draft ratings + synergies ({{env}})"
    go run ./plugins/mtga/tools/17lands-fetch/ \
        --retry --cf-account-id="$cf_account" --d1-database-id="$d1" 2>&1 | sed 's/^/  [17lands] /'

    echo "==> Retry done ({{env}})"

# Show production stats from D1: just stats 1h
stats window="24h":
    ./scripts/stats.sh {{window}}

# Check everything: lint, generate, format, test (in parallel)
check:
    #!/usr/bin/env bash
    set -uo pipefail
    tmpdir=$(mktemp -d)
    trap 'rm -rf "$tmpdir"' EXIT
    pids=()
    names=()
    run() {
        local name=$1; shift
        "$@" >"$tmpdir/$name.out" 2>&1 &
        pids+=($!)
        names+=("$name")
    }
    run proto-lint  just proto-lint
    run proto       just proto
    run lint        just lint
    run test        just test
    failed=0
    for i in "${!pids[@]}"; do
        if ! wait "${pids[$i]}"; then
            echo "==> FAIL: ${names[$i]}"
            cat "$tmpdir/${names[$i]}.out"
            failed=1
        else
            echo "==> OK: ${names[$i]}"
        fi
    done
    exit $failed
