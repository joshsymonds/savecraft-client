{
  description = "Savecraft — game save parser + MCP server";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixpkgs-unstable";
    devenv.url = "github:cachix/devenv";
    git-hooks = {
      url = "github:cachix/git-hooks.nix";
      inputs.nixpkgs.follows = "nixpkgs";
    };
  };

  nixConfig = {
    extra-trusted-public-keys = "devenv.cachix.org-1:w1cLUi8dv3hnoSPGAuibQv+f9TZLr6cv/Hm9XgU50cw=";
    extra-substituters = "https://devenv.cachix.org";
  };

  outputs = {
    self,
    nixpkgs,
    devenv,
    ...
  } @ inputs: let
    forEachSystem = nixpkgs.lib.genAttrs ["x86_64-linux" "aarch64-linux" "aarch64-darwin" "x86_64-darwin"];
  in {
    nixosModules.mtga-data-refresh = import ./nix/mtga-data-refresh.nix;
    nixosModules.pob-server = import ./nix/pob-server.nix;

    packages = forEachSystem (system: let
      pkgs = nixpkgs.legacyPackages.${system};
    in {
      pob-server = pkgs.buildGoModule {
        pname = "pob-server";
        version = "0.1.0";
        src = builtins.path {
          name = "savecraft-src";
          path = ./.;
          filter = path: type:
            let rel = pkgs.lib.removePrefix (toString ./.) (toString path);
            in (type == "directory" && !pkgs.lib.hasPrefix "/vendor" rel)
              || pkgs.lib.hasPrefix "/go.mod" rel
              || pkgs.lib.hasPrefix "/go.sum" rel
              || pkgs.lib.hasPrefix "/cmd/pob-server" rel
              || pkgs.lib.hasPrefix "/internal" rel;
        };
        subPackages = ["cmd/pob-server"];
        vendorHash = "sha256-MlyfksE53wwkvda/5ppHIH5YlwuN5GKyugVBK9S33bk=";
        postInstall = ''
          mkdir -p $out/share/pob-server
          cp cmd/pob-server/wrapper.lua $out/share/pob-server/
        '';
        meta.mainProgram = "pob-server";
      };
    });

    devShells = forEachSystem (system: let
      pkgs = nixpkgs.legacyPackages.${system};
    in {
      default = devenv.lib.mkShell {
        inherit inputs pkgs;
        modules = [./devenv.nix];
      };
    });

  };
}
