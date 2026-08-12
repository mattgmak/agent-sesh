{
  pkgs,
  lib,
  config,
  inputs,
  ...
}:

{
  # https://devenv.sh/basics/

  packages = [
    pkgs.git
    # tmux is the runtime target; needed for integration work (`debug panes`,
    # capture-pane previews) and for the tmux plugin tests.
    pkgs.tmux
    # Optional sesh-style picker: `agent-sesh fzf` requires fzf.
    pkgs.fzf
    pkgs.golangci-lint
  ];

  # https://devenv.sh/languages/
  languages.go = {
    enable = true;
    # Use the nixpkgs default Go toolchain, same as the flake's buildGoModule
    # (go.mod requires go >= 1.25; current unstable ships 1.26). Pinning an
    # older Go here breaks tooling like gopls, which is rebuilt against the
    # pinned toolchain and now requires >= 1.26.
    # gopls LSP + Delve debugger are enabled by default; keep them.
  };

  # https://devenv.sh/scripts/
  scripts = {
    # Run the picker locally (outside tmux it renders standalone).
    run.exec = "go run ./cmd/agent-sesh \"$@\"";
    # `go install` → $GOPATH/bin/agent-sesh, which is on PATH.
    build.exec = "go install ./cmd/agent-sesh";
    test.exec = "go test ./...";
    vet.exec = "go vet ./...";
    lint.exec = "golangci-lint run ./...";
    fmt.exec = "gofmt -w .";
    # agent-sesh debug <subcommand> — inspect registry/tmux state.
    debug.exec = "go run ./cmd/agent-sesh debug \"$@\"";
    # CI gate: run all checks locally before pushing.
    check.exec = ''
      set -euo pipefail

      echo "=== gofmt ==="
      unformatted=$(gofmt -l .)
      if [ -n "$unformatted" ]; then
        echo "Unformatted files:" >&2
        echo "$unformatted" >&2
        exit 1
      fi
      echo "ok"

      echo "=== govet ==="
      go vet ./...
      echo "ok"

      echo "=== golangci-lint ==="
      golangci-lint run ./...
      echo "ok"

      echo "=== gotest ==="
      go test -count=1 ./...
      echo "ok"

      echo "All checks passed."
    '';
  };

  # Pre-commit hooks moved to .github/workflows/ci.yml (PR CI).
}
