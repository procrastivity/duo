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
      in
      {
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
