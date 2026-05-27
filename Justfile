# Generate protobuf code for Go + TypeScript
proto:
    buf generate

# Lint protobuf definitions
proto-lint:
    buf lint

# Check protobuf breaking changes against main
proto-breaking:
    buf breaking --against '.git#branch=main'

# (internal) Mirror gitignored dev+build artifacts from the primary
# checkout into a worktree so `just check` runs there exactly as it
# does here — git worktree only checks out tracked files, so a fresh
# worktree lacks node_modules and built *.wasm. node_modules is
# symlinked (too large to copy; read-mostly). *.wasm is COPIED, not
# symlinked, so an agent rebuilding a plugin in the worktree writes a
# fresh file instead of corrupting the primary checkout's artifact
# through the link. .env.local is copied only when with_env=true: human
# dev worktrees need it for the SvelteKit/Storybook dev server, but
# `just check` does not read it, so the datagen worktree passes false
# and no secrets land in /tmp. gen.ts build outputs are NOT mirrored —
# `just check` regenerates them via its build deps.
_mirror-worktree-env wt with_env="true":
    #!/usr/bin/env bash
    set -euo pipefail
    wt="{{ wt }}"
    with_env="{{ with_env }}"
    for d in worker web site install/worker views reference; do
        if [ -d "$d/node_modules" ]; then
            mkdir -p "$wt/$d"
            ln -sfn "$(realpath "$d/node_modules")" "$wt/$d/node_modules"
        fi
    done
    while IFS= read -r w; do
        mkdir -p "$wt/$(dirname "$w")"
        cp -p "$w" "$wt/$w"
    done < <(find plugins -name '*.wasm' -not -path '*/node_modules/*')
    if [ "$with_env" = "true" ]; then
        while IFS= read -r e; do
            mkdir -p "$wt/$(dirname "$e")"
            cp -p "$e" "$wt/$e"
        done < <(find . -name .env.local -not -path './.worktrees/*' \
            -not -path './.devenv/*' -not -path './.reference/*')
    fi

# Create a feature worktree under .worktrees/<branch> with the gitignored
# dev+build environment mirrored in: node_modules symlinked, built *.wasm
# copied, .env.local copied — instant, no npm ci / no rebuild. Use this
# to isolate parallel agents; do NOT run `git worktree add` directly.
# To change dependencies in the worktree, replace that subdir's
# node_modules symlink with a real `npm ci`.
new-worktree branch:
    #!/usr/bin/env bash
    set -euo pipefail
    wt=".worktrees/{{ branch }}"
    if [ -e "$wt" ] || [ -L "$wt" ]; then echo "$wt already exists" >&2; exit 1; fi
    if git show-ref --verify --quiet "refs/heads/{{ branch }}"; then
        git worktree add "$wt" "{{ branch }}"
    else
        git worktree add -b "{{ branch }}" "$wt"
    fi
    just _mirror-worktree-env "$wt"
    echo ""
    echo "Worktree ready: $wt"
    echo "Next: cd $wt && direnv allow"
    echo "node_modules are symlinked from the primary checkout (shared); *.wasm"
    echo "are copied (safe to rebuild). For dependency changes, rm that subdir's"
    echo "node_modules symlink and npm ci."

# Remove a feature worktree: just rm-worktree feature/my-branch
# --force because the worktree intentionally holds untracked mirrored
# env (copied .env.local, symlinked node_modules). Leaves the branch.
rm-worktree branch:
    git worktree remove --force ".worktrees/{{ branch }}"
    git worktree prune 2>/dev/null || true

# Run all Go tests. internal/ packages are coverage-gated at 80%;
# cmd/ packages run too but aren't gated (entry-point code under
# main.go typically sits below 80% by design).
test-go:
    #!/usr/bin/env bash
    set -euo pipefail
    # internal/ packages: gated on >=80% coverage.
    internal_pkgs=$(go list ./internal/... | while read -r pkg; do
        dir=$(go list -f '{{ "{{" }}.Dir{{ "}}" }}' "$pkg")
        if ls "$dir"/*_test.go &>/dev/null; then echo "$pkg"; fi
    done)
    if [[ -z "$internal_pkgs" ]]; then echo "No internal test packages found"; exit 1; fi
    internal_output=$(echo "$internal_pkgs" | xargs go test -cover)
    echo "$internal_output"
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
    done <<< "$internal_output"
    if (( fail )); then exit 1; fi
    # cmd/ packages: run tests, do not gate on coverage. -count=1
    # disables the per-package result cache so JSON-fixture / wire-tag
    # mismatches that don't change the test binary's source-file hash
    # still re-execute (a real bug we shipped past once already).
    go test -count=1 ./cmd/...

# Run Go tests with race detector. Scoped to the source-of-truth packages
# (same set as `test-go`); a raw ./... also globs the gitignored
# views/storybook-static build mirror, which re-imports internal/ packages
# illegally and never exists in CI.
test-go-race:
    go test -race ./internal/... ./cmd/...

# Run Worker tests (4 parallel shards, each with its own Miniflare)
test-worker: build-manifests build-views
    cd worker && npm test

# Run MCP App view component tests (vitest + @testing-library/svelte)
test-views:
    cd views && npm test

# Run reference Worker infrastructure tests (copies D2R wasm, then tests WASI shim)
test-reference-worker:
    cd reference && just test

# Start Worker dev server (Miniflare)
dev-worker: build-manifests build-views
    cd worker && npx wrangler dev

# Lint Go code
lint-go:
    golangci-lint run ./internal/... ./cmd/...
    deadcode -test ./internal/... ./cmd/... ./plugins/...

# Lint Worker (TypeScript)
lint-worker: build-manifests build-views
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

# Package Factorio mod zip for mods.factorio.com upload
factorio-mod:
    #!/usr/bin/env bash
    set -euo pipefail
    version=$(jq -r .version plugins/factorio/mod/info.json)
    name=$(jq -r .name plugins/factorio/mod/info.json)
    out="${name}_${version}.zip"
    tmp=$(mktemp -d)
    trap 'rm -rf "$tmp"' EXIT
    mkdir "$tmp/${name}_${version}"
    cp plugins/factorio/mod/info.json \
       plugins/factorio/mod/control.lua \
       plugins/factorio/mod/thumbnail.png \
       plugins/factorio/mod/changelog.txt \
       "$tmp/${name}_${version}/"
    (cd "$tmp" && zip -r "$OLDPWD/$out" "${name}_${version}")
    echo "==> $out"

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

# Guard against dead user-facing URLs (epic Req 14): the app is
# my.savecraft.gg and has no /settings route — never strand the user/LLM
# on a path that does not exist in web/src/routes.
lint-no-dead-urls:
    #!/usr/bin/env bash
    set -uo pipefail
    # Tests legitimately assert against the forbidden literal — scan
    # shipping source/docs only, not test files.
    hits=$(grep -rn "savecraft\.gg/settings" \
        --include='*.ts' --include='*.js' --include='*.go' \
        --include='*.svelte' --include='*.md' --include='*.toml' \
        --exclude='*.test.ts' --exclude='*.test.js' \
        --exclude='*_test.go' --exclude='*.stories.svelte' \
        --exclude-dir='test' --exclude-dir='tests' \
        plugins worker docs site web 2>/dev/null || true)
    if [ -n "$hits" ]; then
        echo "Forbidden dead URL 'savecraft.gg/settings' (no such route — use SAVECRAFT_APP_URL / my.savecraft.gg dashboard):"
        echo "$hits"
        exit 1
    fi
    echo "OK: no dead savecraft.gg/settings URLs"

# Guard the single-fetch-point contract (epic Req 7 + anti-pattern): the
# GGG API may only be reached from the PoE adapter (refresh_save /
# fetchState path). No reference module, route, or other code may call
# it — build_planner et al. must be pure consumers of stored state.
lint-no-rogue-ggg-calls:
    #!/usr/bin/env bash
    set -uo pipefail
    # Allowed only under plugins/poe/adapter/. Scan shipping source
    # (not tests, which import the adapter helpers legitimately).
    hits=$(grep -rnE "gggGet\(|api\.pathofexile\.com|pathofexile\.com/oauth|ensureGggAccessToken" \
        --include='*.ts' --include='*.js' \
        --exclude='*.test.ts' --exclude='*.test.js' \
        --exclude-dir='test' --exclude-dir='tests' \
        worker/src plugins 2>/dev/null \
        | grep -vE '(^|/)plugins/poe/adapter/' || true)
    if [ -n "$hits" ]; then
        echo "Rogue GGG API reference outside plugins/poe/adapter/ (Req 7: only refresh_save/fetchState may call GGG):"
        echo "$hits"
        exit 1
    fi
    echo "OK: GGG API references confined to plugins/poe/adapter/"

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

# Start Storybook (web dashboard components)
storybook:
    cd web && npm run storybook

# Build game manifests → worker/src/mcp/manifests.gen.ts
build-manifests:
    npx tsx scripts/build-manifests.ts

# Refresh the spot-check fixture from production D1 (read-only).
# Output: worker/test/fixtures/spot-check.sql (gitignored).
# Requires CLOUDFLARE_API_TOKEN + CLOUDFLARE_ACCOUNT_ID in env (via direnv).
spot-check-fetch:
    npx tsx scripts/dump-spot-check-fixture.ts

# Run the local spot-check matrix against the fixture.
# Asserts overlap ≥ 65%, 0 missing staples, lands in target_range.
# Failing assertions are calibration data — see the printed summary table.
# Standalone TS script (better-sqlite3 + thin D1 shim) — see Epic #50
# Approaches Considered for why this isn't vitest+miniflare.
spot-check:
    npx tsx scripts/spot-check.ts

# Extract PoE tree data → plugins/poe/reference/views/tree-data.gen.json
# Pulls from PoB's bundled .reference/pob/src/TreeData/3_28/tree.lua and
# computes node positions via PoB's exact coordinate formula. The
# Manual maintenance target — regenerate tree-data.gen.json from
# PoB's bundled tree.lua. The output IS committed (see .gitignore),
# because the input data lives in the local-only .reference/pob clone
# that CI doesn't have. Run this only when PoB ships a tree update
# (every league / version bump), then commit the regenerated file.
# Requires luajit on PATH (devenv.nix provides it locally).
extract-tree-data:
    luajit views/scripts/extract-tree-data.lua > plugins/poe/reference/views/tree-data.gen.json

# Build MCP App views → worker/src/mcp/views.gen.ts
# Consumes the committed tree-data.gen.json — does NOT regenerate it.
build-views:
    cd views && npx tsx scripts/build.ts

# Start Storybook (MCP App views)
storybook-views:
    cd views && npm run storybook

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
    run lint-urls      just lint-no-dead-urls
    run lint-ggg       just lint-no-rogue-ggg-calls
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
    run test-views           just test-views
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

# Update MTGA reference data (remote D1/Vectorize only): just update-mtga staging
# Cadence data only — never regenerates committed codegen. The arena-card
# codegen is refreshed separately via `just datagen-magic` (PR-gated).
update-mtga env:
    #!/usr/bin/env bash
    set -euo pipefail
    cf_account="cc0a94bb7aff760efd48b49ce983fe97"
    if [[ "{{env}}" == "production" ]]; then
        d1="df241bb0-9b7d-48e5-a4d4-f84ebf09e6e5"
        rules_vec="magic-rules"
        cards_vec="magic-cards"
    elif [[ "{{env}}" == "staging" ]]; then
        d1="0147892e-82e6-413e-a0ef-52f6d8787fdf"
        rules_vec="magic-rules-staging"
        cards_vec="magic-cards-staging"
    else
        echo "Usage: just update-mtga staging|production" >&2
        exit 1
    fi

    # scryfall-fetch compiles in the committed plugins/magic/parser/data
    # (arena_id coverage) as-is — codegen is NOT regenerated here. Brand-new
    # sets land in D1 once Scryfall maps them, or sooner once a
    # `just datagen-magic` PR merges. The nightly never mutates tracked files.
    echo "==> Phase 1: rules + scryfall enrichment (parallel, {{env}})"
    go run ./plugins/magic/tools/rules-fetch/ \
        --cf-account-id="$cf_account" --d1-database-id="$d1" --vectorize-index="$rules_vec" 2>&1 | sed 's/^/  [rules] /' &
    pid_rules=$!
    go run ./plugins/magic/tools/scryfall-fetch/ \
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
    go run ./plugins/magic/tools/tagger-fetch/ \
        --cf-account-id="$cf_account" --d1-database-id="$d1" 2>&1 | sed 's/^/  [roles] /'

    echo "==> Phase 3: draft ratings ({{env}})"
    go run ./plugins/magic/tools/17lands-fetch/ \
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
    go run ./plugins/magic/tools/tagger-fetch/ \
        --retry --cf-account-id="$cf_account" --d1-database-id="$d1" 2>&1 | sed 's/^/  [roles] /'

    echo "==> Retrying draft ratings + synergies ({{env}})"
    go run ./plugins/magic/tools/17lands-fetch/ \
        --retry --cf-account-id="$cf_account" --d1-database-id="$d1" 2>&1 | sed 's/^/  [17lands] /'

    echo "==> Retry done ({{env}})"

# Regenerate the committed MTGA arena-card codegen; open a PR only if it
# changed. v1 of the "game-dependabot" pattern: codegen artifacts are
# refreshed through a reviewed, CI-gated PR — never by the nightly, never
# pushed to main. Runs where the game data lives, reusing the host's
# existing `gh` auth (no bot account). No-diff runs are a true no-op.
# `db`/`branch` are overridable only for controlled self-tests against a
# fixture DB; the defaults are the real production invocation.
datagen-magic db=".reference/mtga-carddb/Raw_CardDatabase.mtga" branch="datagen/magic-arena-cards":
    #!/usr/bin/env bash
    set -euo pipefail
    gen="plugins/magic/parser/data/arena_cards_gen.go"
    branch="{{ branch }}"
    db="{{ db }}"
    # Reject param values that could break out of the bash string just
    # literally interpolated them into. Only a concern for manual
    # self-test misuse — the automated timer passes no args.
    [[ "$branch" =~ ^[A-Za-z0-9._/-]+$ ]] || { echo "datagen-magic: invalid branch '$branch'" >&2; exit 1; }
    [[ "$db" =~ ^[A-Za-z0-9._/-]+$ ]] || { echo "datagen-magic: invalid db '$db'" >&2; exit 1; }
    # Capture whether $gen was already dirty BEFORE we regenerate, so the
    # no-op/cleanup revert never silently discards a human's local edits.
    gen_was_dirty=0
    git diff --quiet -- "$gen" || gen_was_dirty=1
    restore_gen() {
        if [ "$gen_was_dirty" -eq 0 ]; then
            git checkout -- "$gen" 2>/dev/null || true
        fi
    }
    if [ ! -f "$db" ]; then
        echo "MTGA card database not found at $db" >&2
        echo "Copy Raw_CardDatabase_*.mtga from your MTGA install:" >&2
        echo "  MTGA_Data/Downloads/Raw/Raw_CardDatabase_*.mtga" >&2
        echo "To: .reference/mtga-carddb/Raw_CardDatabase.mtga" >&2
        exit 1
    fi
    go run ./plugins/magic/tools/mtga-carddb/ --card-db="$db" 2>&1 | sed 's/^/  [carddb] /'

    # Baseline the no-op decision on origin/main — that's what the PR is
    # built against. Comparing the local checkout instead would open a
    # zero-diff PR if the host is behind origin/main for $gen.
    git fetch -q origin
    if git diff --quiet origin/main -- "$gen"; then
        echo "arena_cards: no change vs origin/main — no branch, no PR"
        restore_gen
        exit 0
    fi

    echo "arena_cards changed — preparing PR on $branch"

    # Build the commit in an isolated worktree based on origin/main. This
    # never switches the primary tree's branch and is immune to unrelated
    # dirty files / unpushed local commits — a `git checkout -B` in the
    # main tree aborts whenever origin/main's $gen differs from the
    # freshly regenerated one (i.e. every real data change). The PR
    # therefore contains exactly the codegen diff vs origin/main.
    wtbase="$(mktemp -d)"
    wt="$wtbase/wt"
    cleanup() {
        git worktree remove --force "$wt" 2>/dev/null || true
        git worktree prune 2>/dev/null || true
        rm -rf "$wtbase"
        # Artifact advances only through the merged PR — drop the local
        # regen, but never a human's pre-existing edits (see restore_gen).
        restore_gen
    }
    trap cleanup EXIT

    git worktree prune 2>/dev/null || true
    git worktree add -q -B "$branch" "$wt" origin/main

    # Mirror the gitignored dev+build env so the pre-push `just check`
    # gate runs in the worktree exactly as in the primary tree
    # (node_modules symlinked, built *.wasm copied) — no npm ci, no WASM
    # rebuild. Pass with_env=false: `just check` does not read .env.local
    # and the datagen worktree must not write secrets into /tmp.
    just _mirror-worktree-env "$wt" false

    cp "$gen" "$wt/$gen"
    git -C "$wt" add -- "$gen"
    git -C "$wt" commit -q -m "chore(magic): regenerate arena_cards from MTGA client DB"
    # Push from the worktree — the pre-push hook runs the full `just check`
    # here against the mirrored environment; a green local gate plus the
    # PR's CI both guard the merge. Plain ssh `origin` is correct for the
    # unattended systemd timer too: ~/.ssh/id_ed25519 is passphrase-less
    # so ssh authenticates with no agent (HOME is set in the unit), and a
    # global url.insteadOf rewrites https->ssh anyway.
    git -C "$wt" push -q -f origin "$branch"
    if [ -z "$(gh pr list --head "$branch" --state open --json number -q '.[].number')" ]; then
        gh pr create --base main --head "$branch" \
            --title "chore(magic): regenerate arena_cards from MTGA client DB" \
            --body "Automated datagen regeneration of \`$gen\` from the MTGA client \`Raw_CardDatabase\`. Review the card-set diff; CI must be green before merge. Generated by \`just datagen-magic\`."
        echo "opened PR for $branch"
    else
        echo "existing open PR for $branch updated (force-pushed)"
    fi

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
