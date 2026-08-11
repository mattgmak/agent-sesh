# agent-sesh

<p align="center">
  <a href="LICENSE"><img alt="MIT" src="https://img.shields.io/badge/License-MIT-yellow.svg"></a>
  <a href="https://github.com/mattgmak/agent-sesh"><img alt="Go" src="https://img.shields.io/badge/Go-1.25%2B-00ADD8"></a>
</p>

**A tmux picker for AI coding-agent sessions.** One keybind opens a popup listing every pane that is running an agent — with live status, current tool, model, git branch, and a scrollback preview — and lets you jump to it, kill it, or open a new window, without leaving tmux.

> [!WARNING]
> **Heavily vibecoded.** This project was written almost entirely by AI agents
> (the [pi coding agent](https://github.com/sst/pi) and friends), in single
> sitting-driven sessions, for the author's own daily use. It has not been
> battle-tested beyond that. Expect rough edges, unpolished UX, and breaking
> changes while the API settles. It works, but it is a personal tool first and
> a public project second. Use at your own risk; issues and PRs are welcome but
> responses may be slow.

## What it does

- **See every agent at a glance** — panes running an agent show status (`idle`, `working`, `tool_call`, `waiting`), the tool currently executing, the model, git branch, and working directory
- **Discover agents automatically** — panes running an agent that never registered are detected by scanning the pane's tty (even when the agent runs under `node` or a wrapper like `reattach-to-user-namespace`) and listed as dimmed entries
- **Jump, kill, or extend** — attach to a session, kill a pane or a whole session, open a new window, all from the picker
- **Scrollback preview** — the right panel shows live `capture-pane` output for the highlighted pane
- **Two pickers** — a built-in [Bubble Tea](https://github.com/charmbracelet/bubbletea) TUI, or an sesh-style [fzf](https://github.com/junegunn/fzf) picker
- **Plain-text output** — `agent-sesh list` prints rows for scripting and external fuzzy finders

```
>  NixConfig         pi   main     ctx_shell        …/NixConfig/vendor/agent-sesh
   mono/staging      pi   staging  ctx_read         …/code/mono/mono.staging
   tcm/tech-1013     pi   main     lsp_diagnostics  …/code/tcm/tcm.mattmak-…
   NixConfig 3.1     pi   idle     discovered       /Users/mattgmak/NixConfig
```

*Illustrative plain-text rendering — the real TUI adds nerd-font icons and color. The last row is a pane discovered via tty scan (extension not registered) and shows dimmed with `idle` status.*

## How it works

```
pi agent (extension) ──writes──▶ ~/.local/state/agent-sesh/sessions.json
                                      │
tmux ──list-panes + ps -t─────▶ agent-sesh picker ──▶ attach / kill / window
```

Two independent signals feed the picker:

1. **Explicit registry** — the bundled [pi-agent-sesh extension](#pi-extension) runs inside each pi agent and writes session lifecycle events (start, prompt, tool call, end) to `sessions.json`. This is where status, model, and current tool come from.
2. **Discovery** — any pane with a pi process on its tty that never registered (extension not installed, or started before it loaded) still appears, marked as idle and dimmed, so you can always reach every agent.

## Install

### Nix (flake)

```bash
nix run github:mattgmak/agent-sesh
# or: nix build github:mattgmak/agent-sesh
```

### Home Manager

```nix
{
  programs.agent-sesh = {
    enable = true;
    tmuxKey = "a";        # prefix key that opens the picker (default "a")
    popupWidth = "90%";   # default "90%"
    popupHeight = "90%";  # default "90%"
    useFzf = false;       # set true for the fzf picker instead of the TUI
  };
}
```

### tmux plugin (TPM-style)

Use the `agent-sesh-tmux` package (from the flake) or `plugin/agent_sesh.tmux` directly. The plugin reads tmux options:

```tmux
set -g @agent-sesh-bind "a"
set -g @agent-sesh-mode "tui"   # or "fzf"
set -g @agent-sesh-bin "/path/to/agent-sesh"
```

### Go

```bash
go install github.com/mattgmak/agent-sesh@latest
```

## Pi extension

For full status (model, current tool, working/waiting), each pi agent needs the `pi-agent-sesh` extension from [`extensions/pi-agent-sesh`](extensions/pi-agent-sesh). Symlink or copy it into your pi extensions directory and reload pi. Sessions whose extension isn't loaded are still discoverable, just without live status.

## Usage

`agent-sesh` runs the TUI picker. Launch it however you like — a tmux popup binding is the intended way:

```tmux
bind-key -T prefix a display-popup -E -b rounded -T "agent-sesh" -w 90% -h 90% "agent-sesh"
```

### Picker keys

| Key | Action |
|-----|--------|
| `enter` | attach to the highlighted session |
| `/` | filter |
| `ctrl+t` | new window in the highlighted session |
| `ctrl+x` | kill the highlighted pane |
| `ctrl+X` | kill the highlighted session |
| `ctrl+r` | rename session (stub) |
| `q` / `ctrl+c` / `esc` | quit |

### CLI

```
agent-sesh              TUI picker (default)
agent-sesh list         plain-text rows for fzf/scripts
agent-sesh fzf          fzf-based picker (needs fzf)
agent-sesh preview <id> capture-pane preview for a session
agent-sesh debug …      registry + tmux introspection (see Development)
```

## Registry format

The registry is the contract between the pi extension (writer) and the picker (reader): `~/.local/state/agent-sesh/sessions.json`.

```json
{
  "version": 1,
  "sessions": [
    {
      "id": "pi-session-uuid",
      "tmux_target": "nix:0.0",
      "cwd": "/Users/you/NixConfig",
      "branch": "main",
      "title": "agent-sesh specs",
      "status": "tool_call",
      "tool_name": "Shell: go mod tidy",
      "model": "claude-sonnet-4",
      "agent": "pi",
      "updated_at": "2026-07-31T03:00:00.000Z"
    }
  ]
}
```

Status model: `idle` · `working` · `tool_call` · `waiting`.

## Requirements

- [tmux](https://github.com/tmux/tmux) 3.x (macOS and Linux; built on macOS)
- [pi](https://github.com/sst/pi) agents running in tmux panes
- optional: [fzf](https://github.com/junegunn/fzf) for the fzf picker mode
- optional: [devenv](https://devenv.sh) for development

## Development

```bash
devenv shell   # or: direnv allow
run            # go run ./cmd/agent-sesh
test           # go test ./...
build          # go install ./cmd/agent-sesh
```

Manual (no devenv):

```bash
go mod tidy
go run ./cmd/agent-sesh
```

Debug the live state:

```bash
agent-sesh debug registry    # raw sessions.json
agent-sesh debug validate    # sanitize report (kept vs pruned)
agent-sesh debug panes       # registry rows vs live tmux panes
agent-sesh debug discover    # agent panes missing from the registry
agent-sesh debug pane %0     # one pane snapshot (JSON)
```

## Status & roadmap

Working today: registry-backed status, tty-based discovery, attach/kill/window, scrollback preview, fzf mode, Home Manager module.

Planned: pin `vendorHash` in the flake, `ctrl-r` rename prompt, status sources for non-pi agents (see [opensessions](https://github.com/Ataraxy-Labs/opensessions) / herdr), a screen recording or demo in this README.

## Related

- [sesh](https://github.com/joshmedeski/sesh) — the tmux session manager that inspired the picker UX
- [opensessions](https://github.com/Ataraxy-Labs/opensessions) — TypeScript tmux sidebar + HTTP API for multi-agent status
- [pi](https://github.com/sst/pi) — the coding agent this tool tracks

## License

[MIT](LICENSE).
