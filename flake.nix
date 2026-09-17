{
  description = "cert-manager webhook for Nexus DNS";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-26.05";
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
        helpers = nix-helpers.legacyPackages."${system}";

        version = "0.1.8";

        webhook = pkgs.buildGoModule {
          pname = "cert-manager-webhook-nexus";
          inherit version;
          src = ./.;
          doCheck = false;
          # Run `nix build` once; it will fail with the correct hash to use here.
          vendorHash = "sha256-z5XEqdq/my+4y9TA+t8K7l/yplH1/ksCpszIR7GVOo0=";
          ldflags = [ "-w" "-extldflags '-static'" ];
          subPackages = [ "." ];
        };
      in {
        packages = rec {
          default = webhook;
          deployContainer = helpers.deployContainers {
            name = "cert-manager-webhook-nexus";

            # ghcr.io, not the citadel registry: this image is what solves the
            # DNS-01 challenge that issues that registry's certificate, so
            # hosting it there is a bootstrap cycle — the cluster cannot pull
            # the thing it needs in order to be able to pull.
            #
            # The namespace is a GitHub *login*, not a domain. "ghcr.io/fudo"
            # is not a thing that exists and 403s at the token exchange.
            repo = "ghcr.io/fudoniten";

            # Both, deliberately: `latest` for convenience, the pinned tag for
            # anything that actually deploys. A manifest referencing `latest`
            # with pullPolicy Always re-pulls on every pod start, which turns
            # a lapsed registry cert into an unrecoverable deadlock.
            tags = [ "v${version}" "latest" ];

            # No `authfile` here on purpose — pinning one would hardcode a
            # single machine's home directory into the repo. The default
            # search (REGISTRY_AUTH_FILE, then $XDG_RUNTIME_DIR/containers/
            # auth.json, then ~/.docker/config.json) applies, and the helper
            # fails with instructions if none of them has a ghcr.io entry.
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
