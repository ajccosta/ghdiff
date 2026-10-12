{
  lib,
  buildGo127Module,
  makeWrapper,
  git,
  gh,
}:

buildGo127Module {
  pname = "ghdiff";
  version = "unstable";

  src = lib.fileset.toSource {
    root = ./.;
    fileset = lib.fileset.unions [
      ./go.mod
      ./go.sum
      (lib.fileset.fileFilter (f: f.hasExt "go") ./.)
    ];
  };

  # Changes when go.mod/go.sum do: set it to lib.fakeHash, build, and copy
  # the hash nix prints.
  vendorHash = "sha256-Gu3IyYCsv4O4Tvd6OU5hse2w8xIdSp8nhQHzaqEubSg=";

  nativeBuildInputs = [ makeWrapper ];

  # git and gh from nixpkgs, after the user's own (claude comes from the
  # user's PATH; pbcopy from macOS).
  postInstall = ''
    wrapProgram $out/bin/ghdiff --suffix PATH : ${lib.makeBinPath [ git gh ]}
  '';

  meta = {
    description = "Terminal diff viewer for git and gh-dash, with Ask Claude and PR review threads";
    homepage = "https://github.com/ajccosta/ghdiff";
    mainProgram = "ghdiff";
    platforms = lib.platforms.unix;
  };
}
