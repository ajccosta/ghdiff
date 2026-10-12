{
  description = "ghdiff: terminal diff viewer for git and gh-dash";

  inputs.nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";

  outputs =
    { self, nixpkgs }:
    let
      systems = [
        "aarch64-darwin"
        "x86_64-darwin"
        "aarch64-linux"
        "x86_64-linux"
      ];
      forAll = f: nixpkgs.lib.genAttrs systems (s: f nixpkgs.legacyPackages.${s});
    in
    {
      packages = forAll (pkgs: {
        default = pkgs.callPackage ./package.nix { };
      });
      overlays.default = final: prev: { ghdiff = final.callPackage ./package.nix { }; };
    };
}
