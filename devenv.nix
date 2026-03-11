{ pkgs, ... }:

{
  dotenv.enable = true;

  # Pre-push hook enforces the same lint/format/test checks that CI runs.
  # Bypass with: git push --no-verify
  git-hooks.hooks.lint = {
    enable = true;
    name = "lint";
    description = "Run all lint and format checks";
    entry = "just lint";
    language = "system";
    pass_filenames = false;
    stages = [ "pre-push" ];
  };

  git-hooks.hooks.test = {
    enable = true;
    name = "test";
    description = "Run all tests";
    entry = "just test";
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
  '';

  processes.web.exec = "cd web && npm run dev";
  processes.site.exec = "cd site && npm run dev";
  processes.storybook.exec = "cd web && npm run storybook";
}
