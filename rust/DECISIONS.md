# Decisions (ADRs) — agent-sesh Go→Rust port

Headless kanban run (t_5774d5c5): the task body pre-set scope and equivalence; decisions
downstream of it are made here and recorded as ADRs. The task body's constraints:
port the Go picker + TUI, keep CLI output format compatible, do not touch NixConfig
integration until the Rust build passes.

## ADR-001: Shape — CLI, every entry point ported
Status: accepted (2026-11-22)
Context: `analyze --whole-app` reports shape `cli`, one entry point (`cmd/agent-sesh`), 11 units, waves 0–5.
Decision: port every command (`-v/--version`, `list`, `counts`, `daemon`, `api`, `notify`, `debug`, `preview`, `fzf`, bare TUI, unknown-command exit 2) in Rust.
Consequences: clap-based front door; no Go code runs in the final binary.

## ADR-002: TUI stack — ratatui + crossterm replaces Bubble Tea/lipgloss
Status: accepted (2026-11-22)
Context: D2–D7 of the plan (bubbletea, bubbles/textinput, lipgloss, colorprofile, x/ansi) have no drop-in Rust crate; ratatui+crossterm is the standard Rust equivalent.
Decision: reimplement the picker TUI on ratatui + crossterm. Byte-identical ANSI output vs Bubble Tea is not attempted; layout, content, keybindings, and behavior are ported 1:1.
Consequences: TUI verification is functional (ported logic tests + manual smoke), not replay-diffed; all non-TUI output (list, counts, version, usage, errors) stays byte-compatible.

## ADR-003: Equivalence bar
Status: accepted (2026-11-22)
Context: task body says "keep CLI output format compatible".
Decision: byte-for-byte for stdout/stderr/exit codes of `--version`, `list`, `list --json`, `counts` (all modes), `debug *`, `preview`, `notify --backend log`, usage errors; JSON field order and formatting match Go `encoding/json` output (serde with preserve_order + explicit field order). TUI per ADR-002.
Consequences: recording uses a fixed fixture (ADR-004) so replay is deterministic; noise normalizers: none needed beyond fixture control.

## ADR-004: Recording environment — fixture HOME + dedicated tmux server
Status: accepted (2026-11-22)
Context: `list`/`counts` read the registry and shell out to `tmux`/`ps`; live state changes between runs.
Decision: record against `rec-env/`: fixture HOME with config.json + sessions.json (3 pi panes on a dedicated tmux server socket `ra-rec`, panes run a fake `pi` binary; one dead entry exercises sanitize), fixed timestamps. Every recorded case runs through `rec-env/run.sh` which pins HOME, TMUX, AGENT_SESH_STATE_DIR.
Consequences: replay never touches the user's tmux server; daemon start/stop and fzf (interactive) are excluded from recorded cases; `daemon status` and `api` error paths are included.

## ADR-005: Version string
Status: accepted (2026-11-22)
Context: Go stamps `v0.0.0-<date>-<hash>` via ldflags/build info; the recorded binary prints `agent-sesh v0.0.0-20261120073805-dd76c3750229`.
Decision: Rust port stamps the same string via a build-time constant (env override `AGENT_SESH_VERSION`, default `dev`); the recorded case pins the current value.
Consequences: `--version` matches the recorded original; future releases stamp their own version.

## ADR-006: Crate layout — single app crate + per-unit modules, bridge crates for verification
Status: accepted (2026-11-22)
Context: whole-app flow wants `rust/app` plus per-unit bridge crates for record/diff/gate; 11 units, several tiny (version 26 LOC).
Decision: bridge crates under `rust/<unit>-rs/` per the tool's output for units whose functions are bridgeable (version, config, registry, tmux, counts, notify, debug, prof); the TUI (picker) and daemon are ported directly into `rust/app` modules (their functions take io.Writer/interfaces the bridge marks Go-side); `rust/app` is the single binary that wires everything.
Consequences: gate/diff cover the pure-logic units; TUI and daemon are proven by app replay + ported tests.

## ADR-007: Targets — Linux x86_64 primary, macOS best-effort
Status: accepted (2026-11-22)
Context: author runs macOS + this NixOS host; CI target here is linux x86_64.
Decision: build and verify on linux x86_64 (this host); keep code portable (no Linux-only syscalls outside `cfg(unix)` guards); macOS not built in this run.
Consequences: spawn_unix/spawn_other split ported with cfg gates.

## ADR-008: No NixConfig integration changes until Rust build passes
Status: accepted (2026-11-22)
Context: task body forbids touching NixConfig integration before the Rust build passes.
Decision: this run changes nothing under NixConfig; the Rust binary is built and replayed inside the task workspace only.
Consequences: cutover (NixConfig pointing at the Rust build) is a follow-up card after review.
