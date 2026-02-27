{ pkgs, ... }:

{
  dotenv.enable = true;
  packages = [
    # Go daemon + plugins
    pkgs.go_1_26
    pkgs.gopls
    pkgs.gotools        # goimports, etc.
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

    # Build tooling
    pkgs.just           # command runner (Justfile)

    # Temporary: D2SLib reference parser
    pkgs.dotnetCorePackages.sdk_9_0
  ];

  enterShell = ''
    export GOPATH="$DEVENV_STATE/go"
    export GOMODCACHE="$GOPATH/pkg/mod"
    export PATH="$GOPATH/bin:$PATH"

    # Install deadcode if not present (no nix package available)
    if ! command -v deadcode &>/dev/null; then
      go install golang.org/x/tools/cmd/deadcode@latest
    fi

    # Use nix-patched workerd binary for miniflare/vitest (NixOS can't run npm's dynamically linked workerd)
    export MINIFLARE_WORKERD_PATH="$(find ${pkgs.nodePackages.wrangler}/lib -name workerd -path '*/workerd-linux-64/bin/workerd' | head -1)"
  '';

  processes.web.exec = "cd web && npm run dev";
  processes.storybook.exec = "cd web && npm run storybook";
}
