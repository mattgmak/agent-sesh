# Glossary — Go term → Rust name

| Go (agent-sesh) | Meaning | Rust name |
| --- | --- | --- |
| `registry.Session` | one row in sessions.json describing a pi pane | `registry::Session` |
| `registry.Load(path)` | read + parse sessions.json (no sanitize) | `registry::load` |
| `registry.Sanitize(sessions, opts)` | drop dead/duplicate rows, return kept + pruned | `registry::sanitize` → `(Vec<Session>, Vec<PruneReason>)` |
| `registry.CountByCategory` | totals per status category | `registry::count_by_category` |
| `config.Load()` | read config.json (1s cache), env overrides | `config::load` |
| `tmux.Snapshot` | cached `tmux list-panes` + ps scan result | `tmux::Snapshot` |
| `tmux.GetSnapshot(warm)` | build/refresh snapshot | `tmux::get_snapshot` |
| `tmux.CachedSnapshot(max_age)` | reuse persisted snapshot if fresh | `tmux::cached_snapshot` |
| `picker.List(w, opts)` | render registry rows as text or JSON | `picker::list` |
| `picker.Run()` | launch Bubble Tea TUI | `picker::run` (ratatui) |
| `picker.RunFzf()` | pipe entries through fzf | `picker::run_fzf` |
| `picker.Lookup(key)` | find one session by id/target/title/pi name | `picker::lookup` |
| `counts.Run` | print category totals (text/JSON/template) | `counts::run` |
| `daemon.Serve/Start/Stop/Status` | unix-socket daemon control | `daemon::{serve,start,stop,status}` |
| `notify.Send(event, opts)` | emit notification via backend | `notify::send` |
| `debug.Registry/Validate/Panes/Discover/Pane` | read-only diagnostics | `debug::{registry,validate,panes,discover,pane}` |
| `version.Write(w)` | print `agent-sesh <v>` | `version::write` |
| `pi` | the pi coding agent process | `pi` (same) |
| pane target `%N` | tmux pane id | `TmuxTarget` string |
| `DisplayStatus()` | status shown in list rows | `Session::display_status` |
| nerd-font icons | status icons in rows | `icons::*` consts (same codepoints) |
