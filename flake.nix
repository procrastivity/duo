{
  description = "Little buddy for Solo";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
    flake-utils.url = "github:numtide/flake-utils";
  };

  outputs = { self, nixpkgs, flake-utils }:
    flake-utils.lib.eachDefaultSystem (system:
      let
        pkgs = nixpkgs.legacyPackages.${system};
        lib = pkgs.lib;

        # Bun version pin — keep in sync with `bun-version` in
        # .github/workflows/release-bin.yml. We assert here so a nixpkgs bump
        # that drifts past this version is a hard error rather than silent
        # version skew between dev shell and CI binary builds.
        expectedBunVersion = "1.3.11";
        bun = if pkgs.bun.version == expectedBunVersion
          then pkgs.bun
          else throw ''
            flake.nix expects bun ${expectedBunVersion} but nixpkgs provides ${pkgs.bun.version}.
            Either update expectedBunVersion (and the bun-version pin in
            .github/workflows/release-bin.yml) or pin the nixpkgs input to a
            revision that ships bun ${expectedBunVersion}.
          '';
      in
      {
        # From-source build. Bundles nodejs_24 in the closure but builds from
        # whatever ref you point nix at — so `nix run github:.../<branch|sha>`
        # gives a duo built from exactly that source. This stays the default.
        packages.duo = pkgs.buildNpmPackage {
          pname = "duo";
          version = (lib.importJSON ./package.json).version;

          # The build sandbox has no `.git`, so `git rev-parse` in
          # scripts/build-defines.mjs finds nothing and `duo version` prints
          # `—`. Inject the flake's own revision as the source SHA. Clean
          # checkouts get `shortRev`; dirty trees get `dirtyShortRev`; `or ""`
          # keeps eval from throwing when neither exists (e.g. path builds).
          DUO_GIT_SHA = self.shortRev or self.dirtyShortRev or "";

          # allow-list — extend if `npm run build` starts reading new top-level paths.
          src = lib.fileset.toSource {
            root = ./.;
            fileset = lib.fileset.unions [
              ./package.json
              ./package-lock.json
              ./src
              ./scripts
              ./tsconfig.json
            ];
          };

          # To recompute after package-lock.json changes:
          #   1. Set npmDepsHash = lib.fakeHash;
          #   2. Run nix build .#duo
          #   3. Copy the "got: sha256-..." value from the failure output into npmDepsHash
          npmDepsHash = "sha256-hpVD5VV0mbfAmNNm3Ct0CNoYDH9PmuoRt77nNy8A4bI=";

          npmBuildScript = "build";

          nativeBuildInputs = [ pkgs.makeWrapper ];

          installPhase = ''
            runHook preInstall
            mkdir -p $out/lib/duo $out/bin
            cp dist/duo.mjs $out/lib/duo/duo.mjs
            chmod +x $out/lib/duo/duo.mjs
            makeWrapper ${pkgs.nodejs_24}/bin/node $out/bin/duo --add-flags $out/lib/duo/duo.mjs
            runHook postInstall
          '';

          meta = with lib; {
            description = "Solo MCP companion + control-plane CLI (built from source)";
            homepage = "https://github.com/procrastivity/duo";
            license = licenses.mit;
            mainProgram = "duo";
            platforms = platforms.unix;
          };
        };

        # From-source stays default so ad-hoc / branch / commit installs work.
        packages.default = self.packages.${system}.duo;

        apps.duo = {
          type = "app";
          program = "${self.packages.${system}.duo}/bin/duo";
        };
        apps.default = self.apps.${system}.duo;

        devShells.default = pkgs.mkShell {
          name = "duo";

          buildInputs = (with pkgs; [
            python312
            uv
            curl
            jq
            nodejs_24
            git-cliff
          ]) ++ [ bun ]; # `bun` enforces expectedBunVersion (see let-binding above)

          shellHook = ''
            export PROJECT_ROOT="$(pwd)"
            . "${./.}/scripts/activate.sh"
          '';

          PYTHONDONTWRITEBYTECODE = "1";
          PYTHONHASHSEED = "0";
        };
      }
    ) // {
      # System-agnostic overlay so downstream flakes can pull duo into their
      # own nixpkgs: `overlays.default` exposes `duo` (from source) for the
      # consuming system.
      overlays.default = final: _prev: {
        duo = self.packages.${final.stdenv.hostPlatform.system}.duo;
      };
    };
}
