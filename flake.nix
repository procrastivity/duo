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
        packages.duo = pkgs.buildNpmPackage {
          pname = "duo";
          version = (lib.importJSON ./package.json).version;

          # Pin the Node used for npm install/build to match the runtime
          # wrapper below, so a future nixpkgs default-Node bump can't
          # silently change the major version this package builds against.
          nodejs = pkgs.nodejs_24;

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
          npmDepsHash = "sha256-ikMHRrrGSzCKR6Bzt/OxE8hTK3flsgTC8pk8foCvHbI=";

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
        };

        packages.default = self.packages.${system}.duo;

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
    );
}
