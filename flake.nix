{
  description = "tmux picker for pi agent sessions";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
    flake-utils.url = "github:numtide/flake-utils";
    home-manager = {
      url = "github:nix-community/home-manager";
      inputs.nixpkgs.follows = "nixpkgs";
    };
  };

  outputs =
    {
      self,
      nixpkgs,
      flake-utils,
      ...
    }:
    flake-utils.lib.eachDefaultSystem (
      system:
      let
        pkgs = nixpkgs.legacyPackages.${system};
        agent-sesh = pkgs.buildGoModule {
          pname = "agent-sesh";
          version = "0.1.0";
          src = self;
          vendorHash = null;
          meta.mainProgram = "agent-sesh";
        };
        tmuxPlugin = pkgs.tmuxPlugins.mkTmuxPlugin {
          pluginName = "agent-sesh";
          version = "0.1.0";
          src = self + "/plugin";
          extraDependencies = [ agent-sesh ];
          # tmux run-shell executes the rtp file directly; nix store copies are not +x by default.
          postInstall = ''
            chmod +x $target/agent_sesh.tmux
          '';
        };
      in
      {
        packages = {
          default = agent-sesh;
          inherit agent-sesh;
          agent-sesh-tmux = tmuxPlugin;
        };

        apps.default = {
          type = "app";
          program = "${agent-sesh}/bin/agent-sesh";
        };
      }
    )
    // {
      homeModules.agent-sesh =
        {
          config,
          lib,
          pkgs,
          ...
        }:
        let
          system = pkgs.stdenv.hostPlatform.system;
        in
        {
          options.programs.agent-sesh = {
            enable = lib.mkEnableOption "agent-sesh tmux picker for pi sessions";
            package = lib.mkPackageOption self.packages.${system} "agent-sesh" { };
            tmuxKey = lib.mkOption {
              type = lib.types.str;
              default = "a";
              description = "tmux prefix key that opens the picker popup";
            };
            popupWidth = lib.mkOption {
              type = lib.types.str;
              default = "90%";
            };
            popupHeight = lib.mkOption {
              type = lib.types.str;
              default = "90%";
            };
            popupStyle = lib.mkOption {
              type = lib.types.str;
              default = "bg=default,fg=default";
              description = ''
                tmux display-popup -s style. Use bg=default so the popup inherits
                the terminal background instead of a theme-specific popup color.
              '';
            };
            useFzf = lib.mkOption {
              type = lib.types.bool;
              default = false;
              description = ''
                Use an sesh-style fzf picker (`agent-sesh fzf`) instead of the
                built-in Bubble Tea UI. Requires fzf with tmux integration.
              '';
            };
            fzfPackage = lib.mkPackageOption pkgs "fzf" { nullable = true; };
          };

          config = lib.mkIf config.programs.agent-sesh.enable (
            let
              bin = lib.getExe config.programs.agent-sesh.package;
              key = config.programs.agent-sesh.tmuxKey;
              width = config.programs.agent-sesh.popupWidth;
              height = config.programs.agent-sesh.popupHeight;
              style = config.programs.agent-sesh.popupStyle;
            in
            {
              home.packages = [
                config.programs.agent-sesh.package
              ]
              ++ lib.optional (
                config.programs.agent-sesh.useFzf && config.programs.agent-sesh.fzfPackage != null
              ) config.programs.agent-sesh.fzfPackage;

              # Bind after plugins — plugin run-shell scripts can fail to register prefix binds during load.
              programs.tmux.extraConfig = lib.mkAfter (
                ''
                  set -g @agent-sesh-bin ${lib.escapeShellArg bin}
                ''
                + (
                  if config.programs.agent-sesh.useFzf then
                    ''
                      bind-key -T prefix -N "agent-sesh: picker (fzf)" ${key} run-shell "${bin} fzf"
                    ''
                  else
                    ''
                      bind-key -T prefix -N "agent-sesh: picker" ${key} display-popup -E -b rounded -T "agent-sesh" -s "${style}" -w "${width}" -h "${height}" "${bin}"
                    ''
                )
              );
            }
          );
        };
    };
}
