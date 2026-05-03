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
      in
      {
        packages.duo = pkgs.buildNpmPackage {
          pname = "duo";
          version = (lib.importJSON ./package.json).version;

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
          npmDepsHash = "sha256-HJcKVcQlA7qwy+pW8dpCEmyHkQqheh6pA+yJBIkHt4Y=";

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

          buildInputs = with pkgs; [
            python312
            uv
            curl
            jq
            nodejs_24
          ];

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
