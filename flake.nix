{
  description = "Secure local-to-remote clipboard bridge for SSH workflows";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixpkgs-unstable";
    flake-utils.url = "github:numtide/flake-utils";
  };

  outputs = { self, nixpkgs, flake-utils }:
    flake-utils.lib.eachDefaultSystem (system:
      let
        pkgs = nixpkgs.legacyPackages.${system};
        version = self.shortRev or "dev";
      in
      {
        packages = rec {
          pastelocal = pkgs.buildGoModule {
            pname = "pastelocal";
            inherit version;
            src = self;
            vendorHash = pkgs.lib.fakeHash; # Update after first build
            subPackages = [ "cmd/pastelocal" "cmd/pastelocald" ];
            ldflags = [ "-s" "-w" "-X main.version=${version}" ];
          };
          pastelocal-remote = pkgs.buildGoModule {
            pname = "pastelocal-remote";
            inherit version;
            src = self;
            vendorHash = pkgs.lib.fakeHash; # Update after first build
            subPackages = [ "cmd/pastelocal-remote" ];
            ldflags = [ "-s" "-w" "-X main.version=${version}" ];
          };
          default = pastelocal;
        };

        devShells.default = pkgs.mkShell {
          packages = with pkgs; [
            go
            golangci-lint
            goreleaser
          ];
        };
      });
}
