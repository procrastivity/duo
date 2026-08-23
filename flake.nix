{
  description = "duo — dev shell and package for the duo Go CLI";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-25.05";
    flake-utils.url = "github:numtide/flake-utils";
  };

  outputs = { self, nixpkgs, flake-utils }:
    flake-utils.lib.eachDefaultSystem (system:
      let
        pkgs = import nixpkgs { inherit system; };

        # A flake sees rev/shortRev/dirtyRev and lastModified — never tags.
        # So the Nix path stamps the commit where the make path stamps the
        # release tag: `nix build` reports a short rev, `make build` at a
        # tag reports `v0.1.0`. Both name the same commit, and for
        # `nix build github:procrastivity/duo/v0.1.0` the rev is the tag
        # resolved — a stricter identifier, not a looser one. Do not "fix"
        # this with a VERSION file: the tag is the single source of truth,
        # and a second copy would go stale in silence.
        version = self.shortRev or self.dirtyShortRev or "dev";

        duo = pkgs.buildGoModule {
          pname = "duo";
          inherit version;
          src = ./.;
          vendorHash = "sha256-+yCn/j7O6N1MFKiJ7JC2jhDe7shHmtQnssU525ExJyk=";

          env.CGO_ENABLED = 0;

          ldflags = [
            "-X main.version=${version}"
            "-X main.commit=${self.rev or self.dirtyRev or "unknown"}"
            # A fixed epoch, not an oversight: a real build date would make
            # the derivation unreproducible. `commit` above carries the
            # provenance the date would otherwise supply.
            "-X main.date=1970-01-01T00:00:00Z"
          ];

          subPackages = [ "cmd/duo" ];

          postInstall = ''
            mkdir -p $out/share/duo
            cp -r assets $out/share/duo/assets
            # assets.go only exists so `assets/` can embed itself as the
            # binary's last-resort fallback; it is source, not a shipped
            # asset, and must not appear in the installed share tree.
            rm -f $out/share/duo/assets/assets.go
          '';

          meta = {
            description = "duo — attach to and observe agent terminal sessions";
            license = pkgs.lib.licenses.mit;
            mainProgram = "duo";
          };
        };
      in {
        packages.default = duo;

        devShells.default = pkgs.mkShell {
          name = "duo";
          packages = with pkgs; [
            go
            golangci-lint
            gofumpt
            git-cliff
            gnumake
            pre-commit
          ];

          shellHook = ''
            echo "duo dev shell — run 'make check' to lint+test, 'make hooks' to install pre-commit."
          '';
        };
      });
}
