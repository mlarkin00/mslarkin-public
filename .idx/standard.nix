# To learn more about how to use Nix to configure your environment
# see: https://firebase.google.com/docs/studio/customize-workspace
{ pkgs, ... }: {
  # Which nixpkgs channel to use.
  channel = "stable-25.05"; # or "unstable"

  # Use https://search.nixos.org/packages to find packages
  packages = [
    # pkgs.go
    pkgs.docker
    # pkgs.google-cloud-sdk
    # pkgs.nodejs_20
    # pkgs.nodePackages.nodemon
  ];

  # Standard services
  services.docker.enable = true;

  # Sets environment variables in the workspace
  env = {};
  idx = {
    # Search for the extensions you want on https://open-vsx.org/ and use "publisher.id"
    extensions = [
      # "vscodevim.vim"
      "esbenp.prettier-vscode"
      "zaaack.markdown-editor"
      "google.geminicodeassist"
      "googlecloudtools.cloudcode"
    ];
  };
}
