{
  description = "cert-manager webhook for Nexus DNS";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
    flake-utils.url = "github:numtide/flake-utils";
    nix-helpers = {
      url = "github:fudoniten/fudo-nix-helpers";
      inputs.nixpkgs.follows = "nixpkgs";
    };
  };

  outputs = { self, nixpkgs, flake-utils, nix-helpers, ... }:
    flake-utils.lib.eachDefaultSystem (system:
      let
        pkgs = import nixpkgs { inherit system; };
        helpers = nix-helpers.packages."${system}";

        webhook = pkgs.buildGoModule {
          pname = "cert-manager-webhook-nexus";
          version = "0.1.3";
          src = ./.;
          deleteVendor = true;
          # Run `nix build` once; it will fail with the correct hash to use here.
          vendorHash = "sha256-AfTBQtSLrzD+p9gETzeD/5azFdRtlDk9l5YpJLP6k/M=";
          ldflags = [ "-w" "-extldflags '-static'" ];
          subPackages = [ "." ];
        };
      in {
        packages = rec {
          default = webhook;
          deployContainer = helpers.deployContainers {
            name = "cert-manager-webhook-nexus";
            repo = "registry.kube.sea.fudo.link";
            tags = [ "latest" ];
            entrypoint = [ "${webhook}/bin/cert-manager-webhook-nexus" ];
            verbose = true;
          };
        };

        apps = rec {
          default = flake-utils.lib.mkApp { drv = webhook; };
          deployContainer = {
            type = "app";
            program =
              let deployContainer = self.packages."${system}".deployContainer;
              in "${deployContainer}/bin/deployContainers";
          };
        };
      });
}
