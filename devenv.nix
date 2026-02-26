{ pkgs, ... }:

{
  packages = [
    # Go daemon + plugins
    pkgs.go_1_24
    pkgs.gopls
    pkgs.gotools        # goimports, etc.
    pkgs.go-tools       # staticcheck
    pkgs.delve          # debugger

    # Cloudflare Worker + web UI
    pkgs.nodejs_22
    pkgs.nodePackages.npm
    pkgs.nodePackages.wrangler
  ];

  enterShell = ''
    export GOPATH="$DEVENV_STATE/go"
    export GOMODCACHE="$GOPATH/pkg/mod"
  '';
}
