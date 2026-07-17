# Generate Go protobuf code.
proto:
    buf generate

# Lint protobuf definitions.
proto-lint:
    buf lint

# Check protobuf compatibility with main.
proto-breaking:
    buf breaking --against '.git#branch=main'

# Run all Go tests. Internal packages with tests are coverage-gated at 80%.
test-go:
    #!/usr/bin/env bash
    set -euo pipefail
    export GOEXPERIMENT=jsonv2
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
    go test -count=1 ./cmd/...
    go test -count=1 ./plugins/...

test-go-race:
    GOEXPERIMENT=jsonv2 go test -race ./internal/... ./cmd/...

lint-go:
    GOEXPERIMENT=jsonv2 golangci-lint run ./internal/... ./cmd/...
    GOEXPERIMENT=jsonv2 deadcode -test ./internal/... ./cmd/... ./plugins/...

fmt-go:
    find internal/ cmd/ plugins/ -name '*.go' -not -path 'internal/proto/*' -print0 | xargs -0 goimports -w

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

# Build a parser or mod using its public per-game recipe.
build-plugin name:
    cd plugins/{{name}} && just build

build-plugins:
    @for manifest in plugins/*/plugin.toml; do just build-plugin "$(basename "$(dirname "$manifest")")"; done

# Generate the client-only per-game registry descriptor.
plugin-registry name version="dev":
    GOEXPERIMENT=jsonv2 go run ./cmd/plugin-registry/ --version {{version}} plugins/{{name}}

# Package the Factorio in-game mod.
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

keygen:
    GOEXPERIMENT=jsonv2 go run ./cmd/savecraft-keygen/

sign file:
    GOEXPERIMENT=jsonv2 go run ./cmd/savecraft-sign/ {{file}}

verify file:
    GOEXPERIMENT=jsonv2 go run ./cmd/savecraft-verify/ {{file}}

sign-plugins:
    #!/usr/bin/env bash
    set -euo pipefail
    export GOEXPERIMENT=jsonv2
    for wasm in plugins/*/parser.wasm; do
        [[ -f "$wasm" ]] || continue
        go run ./cmd/savecraft-sign/ "$wasm"
    done

# Cross-compile the daemon.
build-daemon os arch version="dev" server_url="https://api.savecraft.gg" install_url="https://install.savecraft.gg" app_name="savecraft" status_port="9182" frontend_url="https://savecraft.gg":
    #!/usr/bin/env bash
    set -euo pipefail
    export GOEXPERIMENT=jsonv2
    mkdir -p dist
    ldflags="-s -w -X main.version={{version}} -X main.serverURLDefault={{server_url}} -X main.installURLDefault={{install_url}} -X main.appName={{app_name}} -X main.statusPortDefault={{status_port}} -X main.frontendURLDefault={{frontend_url}}"
    output="dist/{{app_name}}-daemon-{{os}}-{{arch}}"
    if [[ "{{os}}" == "windows" ]]; then
        ldflags="${ldflags} -H=windowsgui"
        output="${output}.exe"
    fi
    CGO_ENABLED=0 GOOS={{os}} GOARCH={{arch}} go build -ldflags "${ldflags}" -o "${output}" ./cmd/savecraftd/

build-daemon-all version="dev" server_url="https://api.savecraft.gg" install_url="https://install.savecraft.gg" app_name="savecraft" status_port="9182" frontend_url="https://my.savecraft.gg":
    just build-daemon linux amd64 {{version}} {{server_url}} {{install_url}} {{app_name}} {{status_port}} {{frontend_url}}
    just build-daemon linux arm64 {{version}} {{server_url}} {{install_url}} {{app_name}} {{status_port}} {{frontend_url}}
    just build-daemon darwin amd64 {{version}} {{server_url}} {{install_url}} {{app_name}} {{status_port}} {{frontend_url}}
    just build-daemon darwin arm64 {{version}} {{server_url}} {{install_url}} {{app_name}} {{status_port}} {{frontend_url}}
    just build-daemon windows amd64 {{version}} {{server_url}} {{install_url}} {{app_name}} {{status_port}} {{frontend_url}}

# Cross-compile the tray. macOS requires CGO; release CI currently builds Windows.
build-tray os arch app_name="savecraft" status_port="9182" frontend_url="https://my.savecraft.gg":
    #!/usr/bin/env bash
    set -euo pipefail
    export GOEXPERIMENT=jsonv2
    mkdir -p dist
    cgo=0
    ldflags="-s -w -X main.defaultStatusPort={{status_port}} -X main.defaultFrontendURL={{frontend_url}}"
    output="dist/{{app_name}}-tray-{{os}}-{{arch}}"
    if [[ "{{os}}" == "darwin" ]]; then
        cgo=1
    elif [[ "{{os}}" == "windows" ]]; then
        ldflags="${ldflags} -H=windowsgui"
        output="${output}.exe"
    fi
    CGO_ENABLED="${cgo}" GOOS={{os}} GOARCH={{arch}} go build -ldflags "${ldflags}" -o "${output}" ./cmd/savecraft-tray/

build-tray-all app_name="savecraft" status_port="9182" frontend_url="https://my.savecraft.gg":
    just build-tray windows amd64 {{app_name}} {{status_port}} {{frontend_url}}

build-msi version="1.0.0" app_name="savecraft":
    wix build \
        -arch x64 \
        -d Version={{version}} \
        -d DaemonPath=dist/{{app_name}}-daemon-windows-amd64.exe \
        -d TrayPath=dist/{{app_name}}-tray-windows-amd64.exe \
        -o dist/{{app_name}}.msi \
        -ext WixToolset.Util.wixext \
        install/windows/savecraft.wxs

lint-sh:
    shellcheck install/install.sh install/test/run-test.sh

fmt-sh:
    shfmt -w -i 4 -bn -ci install/install.sh install/test/run-test.sh

fmt-sh-check:
    shfmt -d -i 4 -bn -ci install/install.sh install/test/run-test.sh

test-install-worker:
    cd install/worker && npm test

test-install-docker:
    docker build -t savecraft-install-test -f install/test/Dockerfile install/
    docker run --rm savecraft-install-test

lint:
    just lint-go
    just lint-sh
    just fmt-go-check
    just fmt-sh-check

test:
    just test-go
    just test-install-worker

check:
    just proto-lint
    just proto
    just lint
    just test
