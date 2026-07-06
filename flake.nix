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

        # Prebuilt standalone binaries published to the GitHub release by
        # .github/workflows/release-bin.yml. `packages.duo-bin` fetches the
        # asset for the host system; regenerate this manifest per release with
        # `node scripts/update-nix-binaries.mjs <tag>`.
        prebuilt = lib.importJSON ./nix/prebuilt-binaries.json;
        prebuiltAsset = prebuilt.systems.${system} or null;
      in
      {
        # From-source build. Bundles nodejs_24 in the closure but builds from
        # whatever ref you point nix at — so `nix run github:.../<branch|sha>`
        # gives a duo built from exactly that source. This stays the default.
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
          npmDepsHash = "sha256-ZAwbLLxjZpaAwMFiVUmG2ftE4Gi/0qQo2QeardnspzE=";

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

        # Prebuilt standalone binary (bun --compile output). No node in the
        # closure — a true self-contained executable. Released tags only:
        # installing from a non-release ref yields whatever version the pinned
        # manifest points at, not that ref's source. On systems without a
        # published asset (e.g. x86_64-darwin) evaluating this errors clearly.
        packages.duo-bin =
          if prebuiltAsset == null then
            throw "duo: no prebuilt binary for ${system}; use packages.duo (built from source)"
          else
            pkgs.stdenvNoCC.mkDerivation {
              pname = "duo-bin";
              version = prebuilt.version;

              src = pkgs.fetchurl { inherit (prebuiltAsset) url hash; };
              dontUnpack = true;

              nativeBuildInputs =
                lib.optionals pkgs.stdenv.hostPlatform.isLinux [ pkgs.autoPatchelfHook ];

              # bun-compiled Linux binaries dynamically link glibc/libstdc++.
              # Darwin (Mach-O) needs no patching.
              buildInputs = lib.optionals pkgs.stdenv.hostPlatform.isLinux [
                pkgs.stdenv.cc.cc.lib
                pkgs.zlib
              ];

              installPhase = ''
                runHook preInstall
                install -Dm755 $src $out/bin/duo
                runHook postInstall
              '';

              meta = with lib; {
                description = "Solo MCP companion + control-plane CLI (prebuilt standalone binary)";
                homepage = "https://github.com/procrastivity/duo";
                license = licenses.mit;
                mainProgram = "duo";
                platforms = [ "aarch64-darwin" "aarch64-linux" "x86_64-linux" ];
                sourceProvenance = [ sourceTypes.binaryNativeCode ];
              };
            };

        # From-source stays default so ad-hoc / branch / commit installs work.
        packages.default = self.packages.${system}.duo;

        apps.duo = {
          type = "app";
          program = "${self.packages.${system}.duo}/bin/duo";
        };
        apps.duo-bin = {
          type = "app";
          program = "${self.packages.${system}.duo-bin}/bin/duo";
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
      # own nixpkgs: `overlays.default` exposes `duo` (from source) and
      # `duo-bin` (prebuilt) for the consuming system.
      overlays.default = final: _prev: {
        duo = self.packages.${final.stdenv.hostPlatform.system}.duo;
        duo-bin = self.packages.${final.stdenv.hostPlatform.system}.duo-bin;
      };
    };
}
