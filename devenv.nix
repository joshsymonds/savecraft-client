{ pkgs, ... }:

let
  # Pinned Path of Building source — same revision the production NixOS
  # module uses (nix/pob-server.nix). Tests in cmd/pob-server/ that spawn
  # real wrapper.lua read POB_DIR; this gives every dev shell a working
  # setup with no per-test cloning, and guarantees dev/CI/prod parity on
  # the PoB revision.
  pobSrc = import ./nix/pob-source.nix { inherit pkgs; };
  # Pinned PathOfBuilding-PoE2 source — same revision the production
  # NixOS module uses (nix/pob-server.nix). Tests in cmd/pob-server/ that
  # spawn a real wrapper.lua against the PoE2 pool read POB2_DIR; mirrors
  # pobSrc above so dev/CI/prod can never drift on the PoE2 revision.
  pob2Src = import ./nix/pob2-source.nix { inherit pkgs; };
in
{
  dotenv.enable = true;

  # Pre-push hook runs all checks (lint, format, test) in parallel.
  # Bypass with: git push --no-verify
  git-hooks.hooks.check = {
    enable = true;
    name = "check";
    description = "Run all lint, format, and test checks";
    entry = "just check";
    language = "system";
    pass_filenames = false;
    stages = [ "pre-push" ];
  };

  packages = [
    # Go daemon + plugins
    pkgs.go_1_26
    pkgs.gopls
    pkgs.go-tools       # staticcheck
    pkgs.golangci-lint  # comprehensive linter
    pkgs.delve          # debugger

    # Protobuf codegen
    pkgs.buf            # buf CLI (lint, generate, breaking)
    pkgs.protobuf       # protoc + well-known types

    # Cloudflare Worker + web UI
    pkgs.nodejs_22
    pkgs.nodePackages.npm
    pkgs.nodePackages.wrangler

    # Rust (Clausewitz/Paradox plugins)
    pkgs.rustup

    # WASM tooling
    pkgs.wabt           # wasm-objdump, wasm2wat, wat2wasm

    # Build tooling
    pkgs.just           # command runner (Justfile)
    pkgs.luajit         # PoB headless wrapper + tree-data extractor
    pkgs.zlib           # PoB Inflate/Deflate via LuaJIT FFI (POB_ZLIB_PATH)
    pkgs.curl           # bulk-card download in scryfall-fetch (Cloudflare bot management
                        # JA3-blocks Go's net/http on data.scryfall.io)

    # Shell linting
    pkgs.shellcheck     # static analysis for bash/sh
    pkgs.shfmt          # shell formatter

    # Azure (Trusted Signing for Windows MSI)
    (pkgs.azure-cli.withExtensions [ pkgs.azure-cli-extensions.trustedsigning ])
  ];

  enterShell = ''
    export GOEXPERIMENT=jsonv2
    export GOPATH="$DEVENV_STATE/go"
    export GOMODCACHE="$GOPATH/pkg/mod"
    export PATH="$GOPATH/bin:$PATH"

    # Install Go tools with project's Go version (nix gotools is built against older Go)
    if ! command -v goimports &>/dev/null; then
      go install golang.org/x/tools/cmd/goimports@latest
    fi
    if ! command -v deadcode &>/dev/null; then
      go install golang.org/x/tools/cmd/deadcode@latest
    fi

    # Rust: ensure stable toolchain + WASI target for Clausewitz plugins
    if ! rustup toolchain list 2>/dev/null | grep -q stable; then
      rustup default stable
    fi
    if ! rustup target list --installed 2>/dev/null | grep -q wasm32-wasip1; then
      rustup target add wasm32-wasip1
    fi

    # Use nix-patched workerd binary for miniflare/vitest (NixOS can't run npm's dynamically linked workerd)
    export MINIFLARE_WORKERD_PATH="$(find ${pkgs.nodePackages.wrangler}/lib -name workerd -path '*/workerd-linux-64/bin/workerd' | head -1)"

    # Workerd does outbound HTTPS during tests (Clerk OAuth, fetch() in DOs)
    # and can't find the system trust store on NixOS without a hint. Point
    # it at the nss-cacert bundle so TLS verifies cleanly; otherwise tests
    # cascade-fail with "TLS peer's certificate is not trusted" → "Network
    # connection lost" in unrelated assertions.
    export SSL_CERT_FILE=${pkgs.cacert}/etc/ssl/certs/ca-bundle.crt
    export NIX_SSL_CERT_FILE=${pkgs.cacert}/etc/ssl/certs/ca-bundle.crt
    export NODE_EXTRA_CA_CERTS=${pkgs.cacert}/etc/ssl/certs/ca-bundle.crt

    # PoB calc engine path — consumed by pob-server at runtime AND by the
    # Go integration tests in cmd/pob-server/ that spawn real wrapper.lua.
    # Mirrors nix/pob-server.nix's systemd unit. POB_ZLIB_PATH backs PoB's
    # FFI Inflate/Deflate (HeadlessWrapper stubs them otherwise, breaking
    # build-code import + Timeless Jewel LUTs). Shared by both games —
    # wrapper.lua's zlib FFI binding is game-agnostic.
    export POB_DIR=${pobSrc}/src
    export POB_ZLIB_PATH=${pkgs.zlib}/lib/libz.so

    # PathOfBuilding-PoE2 calc engine path — same role as POB_DIR above,
    # for the PoE2 process pool (POB_GAME=poe2). No separate zlib path:
    # wrapper.lua's POB_ZLIB_PATH lookup is not game-specific.
    export POB2_DIR=${pob2Src}/src
  '';

  processes.web.exec = "cd web && npm run dev";
  processes.site.exec = "cd site && npm run dev";
  processes.storybook.exec = "cd web && npm run storybook";
}
