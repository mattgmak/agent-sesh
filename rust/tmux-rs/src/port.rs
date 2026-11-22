//! The Rust port of `internal/tmux/`.
//!
//! Mirrors `agent-sesh/internal/tmux/{exec,tmux,agent,pi_detect,snapshot}.go`:
//! command construction, format strings, error strings, and pane-detection
//! semantics match the Go source. `GetSnapshot`/`PeekSnapshot`/`SaveSnapshot`
//! stay Go-side at the bridge, but the ported exports need the snapshot
//! machinery internally, so it is reproduced here privately.

#![forbid(unsafe_code)]
// The port mirrors Go's nested `if err != nil` structure verbatim; collapsing
// those `if`s (and the snapshot type aliases) would diverge from the source.
#![allow(clippy::collapsible_if, clippy::type_complexity, clippy::manual_find)]

use std::collections::{BTreeMap, BTreeSet};
use std::process::{Command, Stdio};
use std::sync::mpsc;
use std::sync::{Arc, Mutex, OnceLock};
use std::time::{Duration, Instant};

use serde_json::{Value, json};

/// Port of `defaultExecTimeout` (`internal/tmux/exec.go:13`).
const EXEC_TIMEOUT: Duration = Duration::from_secs(5);
/// Port of `defaultSnapshotTTL` (`internal/tmux/snapshot.go:18`).
const SNAPSHOT_TTL: Duration = Duration::from_secs(10);
/// Port of `piScanTTL` (`internal/tmux/snapshot.go:22`).
const PI_SCAN_TTL: Duration = Duration::from_secs(15);
/// Port of `defaultProcessTreeDepth` (`internal/tmux/pi_detect.go:9`).
const DEFAULT_PROCESS_TREE_DEPTH: u32 = 5;
/// Port of `snapshotFileName` (`internal/tmux/snapshot.go:25`).
const SNAPSHOT_FILE_NAME: &str = "pane-snapshot.json";
/// Port of `paneListFormat` (`internal/tmux/snapshot.go:26`).
const PANE_LIST_FORMAT: &str = "#{pane_id}\t#{session_name}\t#{window_index}\t#{pane_index}\t#{pane_tty}\t#{pane_current_command}\t#{pane_start_command}\t#{pane_current_path}\t#{pane_pid}\t#{pane_width}";
/// Port of `config.DefaultAgent` (`internal/config/config.go`), used when a
/// pane is known to run pi but names no agent.
const DEFAULT_AGENT: &str = "pi";
/// Port of `subagentScriptMarker` (`internal/tmux/pi_detect.go:97`).
const SUBAGENT_SCRIPT_MARKER: &str = "subagent-scripts";

// ---------------------------------------------------------------------------
// exec.go
// ---------------------------------------------------------------------------

/// Port of `execOutput` (`internal/tmux/exec.go:35`): run `argv` with a
/// bounded lifetime, capture stdout. The Go side bounds the run with
/// `context.WithTimeout` plus `WaitDelay`; the timeout error string is
/// reproduced here. `prof.Start(name)` is observability only and has no
/// bridge-visible effect, so it is not ported.
fn exec_output(name: &str, argv: &[&str]) -> Result<String, String> {
    let owned: Vec<String> = argv.iter().map(|s| s.to_string()).collect();
    let (tx, rx) = mpsc::channel();
    std::thread::spawn(move || {
        let result = Command::new(&owned[0])
            .args(&owned[1..])
            .stdin(Stdio::null())
            .stdout(Stdio::piped())
            .stderr(Stdio::piped())
            .output();
        let _ = tx.send(match result {
            Ok(output) if output.status.success() => {
                Ok(String::from_utf8_lossy(&output.stdout).into_owned())
            }
            Ok(output) => Err(format!(
                "exit status {}",
                output.status.code().unwrap_or(-1)
            )),
            Err(e) if e.kind() == std::io::ErrorKind::NotFound => Err(format!(
                "exec: {:?}: executable file not found in $PATH",
                owned[0]
            )),
            Err(e) => Err(e.to_string()),
        });
    });
    rx.recv_timeout(EXEC_TIMEOUT)
        .map_err(|_| format!("{name}: context deadline exceeded"))?
}

/// Port of `execRun` (`internal/tmux/exec.go:45`): run and discard stdout.
fn exec_run(name: &str, argv: &[&str]) -> Result<(), String> {
    exec_output(name, argv).map(|_| ())
}

// ---------------------------------------------------------------------------
// snapshot.go (private machinery used by the ported exports)
// ---------------------------------------------------------------------------

/// Port of `PaneInfo` (`internal/tmux/agent.go:11`).
#[derive(Clone, Default)]
struct PaneInfo {
    target: String,
    pane_id: String,
    session_name: String,
    window_index: String,
    pane_index: String,
    tty: String,
    current_command: String,
    start_command: String,
    current_path: String,
    shell_pid: i64,
    width: i64,
    exists: bool,
    has_pi_agent: bool,
    agent: String,
    is_subagent: bool,
}

fn pane_info_to_value(info: &PaneInfo) -> Value {
    json!({
        "Target": info.target,
        "PaneID": info.pane_id,
        "SessionName": info.session_name,
        "WindowIndex": info.window_index,
        "PaneIndex": info.pane_index,
        "TTY": info.tty,
        "CurrentCommand": info.current_command,
        "StartCommand": info.start_command,
        "CurrentPath": info.current_path,
        "ShellPID": info.shell_pid,
        "Width": info.width,
        "Exists": info.exists,
        "HasPiAgent": info.has_pi_agent,
        "Agent": info.agent,
        "IsSubagent": info.is_subagent,
    })
}

/// Port of `Snapshot` (`internal/tmux/snapshot.go:30`). Panes are kept in a
/// sorted map so `PaneIDs` has a stable order (Go's map order is random).
struct Snapshot {
    panes: BTreeMap<String, PaneInfo>,
    fetched_at: Instant,
}

impl Snapshot {
    /// Port of `(*Snapshot).HasPane` (`internal/tmux/snapshot.go:334`).
    fn has_pane(&self, target: &str) -> bool {
        let target = target.trim();
        !target.is_empty() && self.panes.contains_key(target)
    }

    /// Port of `(*Snapshot).PaneInfo` (`internal/tmux/snapshot.go:359`).
    fn pane_info(&self, target: &str) -> Option<PaneInfo> {
        self.panes.get(target.trim()).cloned()
    }

    /// Port of `(*Snapshot).HasPiAgent` (`internal/tmux/snapshot.go:368`).
    fn has_pi_agent(&self, target: &str) -> bool {
        self.pane_info(target).is_some_and(|info| info.has_pi_agent)
    }

    /// Port of `(*Snapshot).PaneIDs` (`internal/tmux/snapshot.go:389`).
    fn pane_ids(&self) -> Vec<String> {
        self.panes.keys().cloned().collect()
    }
}

fn snapshot_slot() -> &'static Mutex<Option<Arc<Snapshot>>> {
    static SLOT: OnceLock<Mutex<Option<Arc<Snapshot>>>> = OnceLock::new();
    SLOT.get_or_init(Default::default)
}

fn refresh_slot() -> &'static Mutex<()> {
    static SLOT: OnceLock<Mutex<()>> = OnceLock::new();
    SLOT.get_or_init(Default::default)
}

/// Port of the `piScanMu`/`piScanKey`/`piScanTTYs`/`piScanAt` globals
/// (`internal/tmux/snapshot.go:40-43`).
fn pi_scan_slot() -> &'static Mutex<Option<(String, BTreeMap<String, String>, Instant)>> {
    static SLOT: OnceLock<Mutex<Option<(String, BTreeMap<String, String>, Instant)>>> =
        OnceLock::new();
    SLOT.get_or_init(Default::default)
}

fn lock<T>(slot: &Mutex<T>) -> Result<std::sync::MutexGuard<'_, T>, String> {
    slot.lock().map_err(|_| "lock poisoned".to_string())
}

/// Port of `GetSnapshot` (`internal/tmux/snapshot.go:68`).
fn get_snapshot(force: bool) -> Result<Arc<Snapshot>, String> {
    if !force {
        if let Some(snap) = lock(snapshot_slot())?.clone() {
            if snap.fetched_at.elapsed() < SNAPSHOT_TTL {
                return Ok(snap);
            }
        }
    }
    refresh_snapshot()
}

/// Port of `refreshSnapshot` (`internal/tmux/snapshot.go:91`). The Go side
/// single-flights via a shared in-flight call; holding the refresh mutex for
/// the whole fetch and re-checking the cache at entry gives concurrent
/// callers the same shared result.
fn refresh_snapshot() -> Result<Arc<Snapshot>, String> {
    let _guard = lock(refresh_slot())?;
    if let Some(snap) = lock(snapshot_slot())?.clone() {
        if snap.fetched_at.elapsed() < SNAPSHOT_TTL {
            return Ok(snap);
        }
    }
    let snap = Arc::new(fetch_snapshot()?);
    *lock(snapshot_slot())? = Some(snap.clone());
    Ok(snap)
}

/// Port of `fetchSnapshot` (`internal/tmux/snapshot.go:111`).
fn fetch_snapshot() -> Result<Snapshot, String> {
    let out = exec_output(
        "tmux.list-panes",
        &["tmux", "list-panes", "-a", "-F", PANE_LIST_FORMAT],
    )?;

    let mut panes: BTreeMap<String, PaneInfo> = BTreeMap::new();
    let mut scan_ttys: BTreeSet<String> = BTreeSet::new();
    let mut check_pids: Vec<i64> = Vec::new();
    for line in out.trim().lines() {
        let line = line.trim();
        if line.is_empty() {
            continue;
        }
        let fields: Vec<&str> = line.split('\t').collect();
        if fields.len() < 8 {
            continue;
        }
        let target = fields[0];
        let mut info = PaneInfo {
            target: target.to_string(),
            pane_id: fields[0].to_string(),
            session_name: fields[1].to_string(),
            window_index: fields[2].to_string(),
            pane_index: fields[3].to_string(),
            tty: fields[4].to_string(),
            current_command: fields[5].to_string(),
            start_command: fields[6].to_string(),
            current_path: fields[7].to_string(),
            exists: true,
            ..Default::default()
        };
        if let Some(field) = fields.get(8) {
            if let Ok(pid) = go_atoi(field.trim()) {
                info.shell_pid = pid;
            }
        }
        if let Some(field) = fields.get(9) {
            if let Ok(width) = go_atoi(field.trim()) {
                info.width = width;
            }
        }
        let (agent, has_pi_agent) =
            agent_name_from_command(&info.current_command, &info.start_command);
        info.agent = agent;
        info.has_pi_agent = has_pi_agent;
        if !info.has_pi_agent {
            let tty = tty_base_name(&info.tty);
            if !tty.is_empty() {
                scan_ttys.insert(tty);
            }
            if info.shell_pid > 0 {
                check_pids.push(info.shell_pid);
            }
        }
        panes.insert(target.to_string(), info);
    }

    // One ps fork feeds both lookups: the tty→agent map for the pane ttys
    // whose command did not already prove pi, and the process trees under
    // each shell pid. Every pane already names its agent, so both sets are
    // empty and no ps runs.
    let procs = if !scan_ttys.is_empty() || !check_pids.is_empty() {
        Some(scan_processes()?)
    } else {
        None
    };
    let pi_ttys = match &procs {
        Some(procs) => cached_pi_agent_ttys(procs, &scan_ttys)?,
        None => None,
    };
    let (pi_roots, subagent_roots) = match &procs {
        Some(procs) => {
            agent_presence_in_process_trees(procs, &check_pids, DEFAULT_PROCESS_TREE_DEPTH)
        }
        None => (BTreeMap::new(), BTreeSet::new()),
    };

    for info in panes.values_mut() {
        if !info.has_pi_agent {
            let tty = tty_base_name(&info.tty);
            if let Some(name) = pi_ttys.as_ref().and_then(|map| map.get(&tty)) {
                info.agent = name.clone();
                info.has_pi_agent = true;
            }
        }
        if !info.has_pi_agent && info.shell_pid > 0 {
            if let Some(name) = pi_roots.get(&info.shell_pid) {
                info.agent = name.clone();
                info.has_pi_agent = true;
            }
        }
        if info.shell_pid > 0 && subagent_roots.contains(&info.shell_pid) {
            info.is_subagent = true;
        }
    }

    Ok(Snapshot {
        panes,
        fetched_at: Instant::now(),
    })
}

/// Port of `snapshotFilePath` (`internal/tmux/snapshot.go:271`): the disk
/// snapshot lives beside the registry, i.e. in `$XDG_STATE_HOME/agent-sesh/`
/// (or `$HOME/.local/state/agent-sesh/`).
fn snapshot_file_path() -> Result<std::path::PathBuf, String> {
    let state_home = match std::env::var_os("XDG_STATE_HOME") {
        Some(value) if !value.is_empty() => std::path::PathBuf::from(value),
        _ => {
            let home =
                std::env::var_os("HOME").ok_or_else(|| "$HOME is not defined".to_string())?;
            std::path::Path::new(&home).join(".local").join("state")
        }
    };
    Ok(state_home.join("agent-sesh").join(SNAPSHOT_FILE_NAME))
}

/// Port of `cachedPiAgentTTYs` (`internal/tmux/snapshot.go:281`).
fn cached_pi_agent_ttys(
    procs: &[PsProcess],
    scan_ttys: &BTreeSet<String>,
) -> Result<Option<BTreeMap<String, String>>, String> {
    if scan_ttys.is_empty() {
        return Ok(None);
    }
    let key = scan_ttys.iter().cloned().collect::<Vec<_>>().join(",");
    let mut slot = lock(pi_scan_slot())?;
    if let Some((cached_key, map, at)) = slot.as_ref() {
        if *cached_key == key && at.elapsed() < PI_SCAN_TTL {
            return Ok(Some(map.clone()));
        }
    }
    let map = pi_agent_ttys(procs, scan_ttys);
    *slot = Some((key, map.clone(), Instant::now()));
    Ok(Some(map))
}

/// Port of `piAgentTTYs` (`internal/tmux/agent.go:132`).
fn pi_agent_ttys(procs: &[PsProcess], want: &BTreeSet<String>) -> BTreeMap<String, String> {
    let mut agents: BTreeMap<String, String> = BTreeMap::new();
    for proc in procs {
        if proc.tty.is_empty() || proc.tty == "?" || proc.tty == "??" {
            continue;
        }
        if !want.contains(&proc.tty) {
            continue;
        }
        if let Some(name) = agent_process_name(&proc.command) {
            agents.insert(proc.tty.clone(), name);
        }
    }
    agents
}

/// Port of `ttyBaseName` (`internal/tmux/agent.go:126`).
fn tty_base_name(tty: &str) -> String {
    tty.trim()
        .strip_prefix("/dev/")
        .unwrap_or(tty.trim())
        .to_string()
}

/// Port of `agentNameFromCommand` (`internal/tmux/snapshot.go:315`): returns
/// (agent, ok) for a pane's current or start command.
fn agent_name_from_command(current: &str, start: &str) -> (String, bool) {
    if let Some(name) = agent_process_name(current) {
        return (name, true);
    }
    match agent_process_name(start) {
        Some(name) => (name, true),
        None => (String::new(), false),
    }
}

/// Port of `agentProcessName` (`internal/tmux/agent.go:161`): the configured
/// agent name a ps command line runs, from `config.Agents()`.
fn agent_process_name(line: &str) -> Option<String> {
    for name in configured_agents() {
        if command_runs(line, &name) {
            return Some(name);
        }
    }
    None
}

/// Port of `config.Agents` (`internal/config/config.go:131`) via the
/// ported `config-rs` crate; on failure Go falls back to the default agent.
///
/// Go imports internal/config; linking the generated config-rs crate here
/// would duplicate its `rust_anything_call` exports in this cdylib, so the
/// lookup (env override, file read, one-second cache) is reproduced inline.
fn configured_agents() -> Vec<String> {
    agents().unwrap_or_else(|_| vec![DEFAULT_AGENT.to_string()])
}

/// Port of the `cacheMu`/`cached`/`cachedAt`/`cachedFrom` globals and the
/// cache check in `Agents` (`internal/config/config.go:78-83,131`).
fn agents_cache() -> &'static Mutex<Option<(String, Vec<String>, Instant)>> {
    static CACHE: OnceLock<Mutex<Option<(String, Vec<String>, Instant)>>> = OnceLock::new();
    CACHE.get_or_init(Default::default)
}

fn agents() -> Result<Vec<String>, String> {
    let path = config_path()?;
    let mut guard = lock(agents_cache())?;
    if let Some((cached_from, cached, cached_at)) = guard.as_ref()
        && *cached_from == path
        && cached_at.elapsed() < Duration::from_secs(1)
    {
        return Ok(cached.clone());
    }
    let loaded = load_agents()?;
    *guard = Some((path, loaded.clone(), Instant::now()));
    Ok(loaded)
}

/// Port of `Load` (`internal/config/config.go:156`) narrowed to the agents
/// list that `Agents` reads.
fn load_agents() -> Result<Vec<String>, String> {
    let mut agents = vec![DEFAULT_AGENT.to_string()];
    if let Some(raw) = std::env::var_os("AGENT_SESH_AGENTS") {
        let raw = to_string_lossy(raw);
        let raw = raw.trim();
        if !raw.is_empty() {
            let names: Vec<String> = raw.split(',').map(|s| s.to_string()).collect();
            return Ok(normalize_agent_names(&names));
        }
    }
    let path = config_path()?;
    if path.is_empty() {
        return Ok(agents);
    }
    let Ok(data) = std::fs::read(&path) else {
        return Ok(agents);
    };
    let Ok(file) = serde_json::from_slice::<Value>(&data) else {
        return Ok(agents);
    };
    if let Some(items) = file.get("agents").and_then(Value::as_array) {
        let names: Vec<String> = items
            .iter()
            .filter_map(|v| v.as_str().map(String::from))
            .collect();
        let names = normalize_agent_names(&names);
        if !names.is_empty() {
            agents = names;
        }
    }
    Ok(agents)
}

/// Port of `Path` (`internal/config/config.go:111`) narrowed to what
/// `Agents` needs.
fn config_path() -> Result<String, String> {
    if let Some(p) = std::env::var_os("AGENT_SESH_CONFIG") {
        let p = to_string_lossy(p).trim().to_string();
        if !p.is_empty() {
            return Ok(p);
        }
    }
    let mut paths = Vec::new();
    if let Some(xdg) = std::env::var_os("XDG_CONFIG_HOME") {
        let xdg = to_string_lossy(xdg);
        let xdg = xdg.trim();
        if !xdg.is_empty() {
            paths.push(format!("{xdg}/agent-sesh/config.json"));
        }
    }
    if let Ok(home) = std::env::var("HOME") {
        paths.push(format!("{home}/.config/agent-sesh/config.json"));
    }
    for p in paths {
        if let Ok(st) = std::fs::metadata(&p)
            && st.is_file()
        {
            return Ok(p);
        }
    }
    Ok(String::new())
}

/// Port of `normalize` (`internal/config/config.go`): trim, drop empties,
/// dedupe in order.
fn normalize_agent_names(names: &[String]) -> Vec<String> {
    let mut out = Vec::with_capacity(names.len());
    let mut seen = BTreeSet::new();
    for name in names {
        let name = name.trim().to_string();
        if name.is_empty() || !seen.insert(name.clone()) {
            continue;
        }
        out.push(name);
    }
    out
}

/// Decodes an OS environment value; Go's `os.Getenv` yields the raw bytes
/// as a string.
fn to_string_lossy(value: std::ffi::OsString) -> String {
    value.to_string_lossy().into_owned()
}

/// Port of `commandRuns` (`internal/tmux/agent.go:172`).
fn command_runs(line: &str, name: &str) -> bool {
    let line = line.trim();
    if line.is_empty() {
        return false;
    }
    if line.contains("lazygit") {
        return false;
    }
    for field in line.split_whitespace() {
        let base = match field.rfind('/') {
            Some(idx) => &field[idx + 1..],
            None => field,
        };
        if base == name {
            return true;
        }
    }
    false
}

/// Port of `isPiProcessLine` (`internal/tmux/agent.go:153`).
fn is_pi_process_line(line: &str) -> bool {
    agent_process_name(line).is_some()
}

/// Port of `isSubagentScriptLine` (`internal/tmux/pi_detect.go:101`).
fn is_subagent_script_line(line: &str) -> bool {
    line.contains(SUBAGENT_SCRIPT_MARKER)
}

// ---------------------------------------------------------------------------
// pi_detect.go
// ---------------------------------------------------------------------------

/// Port of `psProcess` (`internal/tmux/pi_detect.go:11`).
struct PsProcess {
    pid: i64,
    ppid: i64,
    tty: String,
    command: String,
}

/// Port of `scanProcesses` (`internal/tmux/pi_detect.go:21`).
fn scan_processes() -> Result<Vec<PsProcess>, String> {
    let out = exec_output(
        "ps.process-list",
        &["ps", "-ax", "-o", "pid=,ppid=,tty=,command="],
    )?;
    let procs = parse_ps_process_list(&out);
    if procs.is_empty() {
        return Err("parse ps process list: no processes".to_string());
    }
    Ok(procs)
}

/// Port of `parsePSProcessList` (`internal/tmux/pi_detect.go:105`).
fn parse_ps_process_list(output: &str) -> Vec<PsProcess> {
    let mut out = Vec::new();
    for line in output.trim().lines() {
        let line = line.trim();
        if line.is_empty() {
            continue;
        }
        let fields: Vec<&str> = line.split_whitespace().collect();
        if fields.len() < 4 {
            continue;
        }
        let Ok(pid) = go_atoi(fields[0]) else {
            continue;
        };
        let Ok(ppid) = go_atoi(fields[1]) else {
            continue;
        };
        out.push(PsProcess {
            pid,
            ppid,
            tty: fields[2].to_string(),
            command: fields[3..].join(" "),
        });
    }
    out
}

/// Port of `agentPresenceInProcessTrees` (`internal/tmux/pi_detect.go:39`).
fn agent_presence_in_process_trees(
    procs: &[PsProcess],
    roots: &[i64],
    max_depth: u32,
) -> (BTreeMap<i64, String>, BTreeSet<i64>) {
    if roots.is_empty() {
        return (BTreeMap::new(), BTreeSet::new());
    }
    let max_depth = if max_depth == 0 {
        DEFAULT_PROCESS_TREE_DEPTH
    } else {
        max_depth
    };

    let mut children: BTreeMap<i64, Vec<i64>> = BTreeMap::new();
    let mut commands: BTreeMap<i64, String> = BTreeMap::new();
    for proc in procs {
        children.entry(proc.ppid).or_default().push(proc.pid);
        commands.insert(proc.pid, proc.command.clone());
    }

    let mut pi_roots: BTreeMap<i64, String> = BTreeMap::new();
    let mut subagent_roots: BTreeSet<i64> = BTreeSet::new();
    for &root in roots {
        if root <= 0 {
            continue;
        }
        if let Some(command) =
            tree_command(root, max_depth, &children, &commands, is_pi_process_line)
        {
            if let Some(name) = agent_process_name(&command) {
                pi_roots.insert(root, name);
            }
        }
        if tree_command(
            root,
            max_depth,
            &children,
            &commands,
            is_subagent_script_line,
        )
        .is_some()
        {
            subagent_roots.insert(root);
        }
    }
    (pi_roots, subagent_roots)
}

/// Port of `treeCommand` (`internal/tmux/pi_detect.go:74`).
fn tree_command(
    pid: i64,
    depth: u32,
    children: &BTreeMap<i64, Vec<i64>>,
    commands: &BTreeMap<i64, String>,
    matches: fn(&str) -> bool,
) -> Option<String> {
    if pid <= 0 || depth == 0 {
        return None;
    }
    if let Some(command) = commands.get(&pid) {
        if matches(command) {
            return Some(command.clone());
        }
    }
    for child in children.get(&pid).map(Vec::as_slice).unwrap_or(&[]) {
        if let Some(command) = tree_command(*child, depth - 1, children, commands, matches) {
            return Some(command);
        }
    }
    None
}

/// Port of `processTreeHasPi` (`internal/tmux/agent.go:193`).
fn process_tree_has_pi(pid: i64, depth: u32) -> bool {
    if pid <= 0 || depth == 0 {
        return false;
    }
    let Ok(out) = exec_output("pgrep.children", &["pgrep", "-P", &pid.to_string()]) else {
        return false;
    };
    for line in out.split_whitespace() {
        let Ok(child) = go_atoi(line) else { continue };
        if let Ok(cmd) = exec_output("ps.command", &["ps", "-o", "command=", "-p", line]) {
            if is_pi_process_line(&cmd) {
                return true;
            }
        }
        if process_tree_has_pi(child, depth - 1) {
            return true;
        }
    }
    false
}

// ---------------------------------------------------------------------------
// agent.go helpers used by the exports
// ---------------------------------------------------------------------------

/// Port of `paneAgentOnTTY` (`internal/tmux/agent.go:107`).
fn pane_agent_on_tty(target: &str) -> Option<String> {
    let tty = match pane_tty(target.to_string()) {
        Ok(tty) if !tty.is_empty() => tty,
        _ => return None,
    };
    let out = exec_output("ps.pane-tty", &["ps", "-t", &tty, "-o", "command="]).ok()?;
    for line in out.lines() {
        if let Some(name) = agent_process_name(line) {
            return Some(name);
        }
    }
    None
}

/// Port of `paneHasPiOnTTY` (`internal/tmux/agent.go:101`).
fn pane_has_pi_on_tty(target: &str) -> bool {
    pane_agent_on_tty(target).is_some()
}

/// Port of `paneHasSubagentScriptOnTTY` (`internal/tmux/agent.go:276`).
fn pane_has_subagent_script_on_tty(target: &str) -> bool {
    let tty = match pane_tty(target.to_string()) {
        Ok(tty) if !tty.is_empty() => tty,
        _ => return false,
    };
    let Ok(out) = exec_output(
        "ps.pane-tty-subagent",
        &["ps", "-t", &tty, "-o", "command="],
    ) else {
        return false;
    };
    out.lines().any(is_subagent_script_line)
}

/// Port of `strconv.Atoi` error strings (`strconv.Atoi`): Go reports
/// `strconv.Atoi: parsing "x": invalid syntax` (or `value out of range`).
fn go_atoi(s: &str) -> Result<i64, String> {
    match s.parse::<i64>() {
        Ok(value) => Ok(value),
        Err(e) => {
            let reason = match e.kind() {
                std::num::IntErrorKind::PosOverflow | std::num::IntErrorKind::NegOverflow => {
                    "value out of range"
                }
                _ => "invalid syntax",
            };
            Err(format!("strconv.Atoi: parsing {s:?}: {reason}"))
        }
    }
}

// ---------------------------------------------------------------------------
// exported ports
// ---------------------------------------------------------------------------

/// Port of `PaneTTY` (`internal/tmux/agent.go:34`).
pub fn pane_tty(target: String) -> Result<String, String> {
    let out = exec_output(
        "tmux.pane-tty",
        &[
            "tmux",
            "display-message",
            "-p",
            "-t",
            &target,
            "#{pane_tty}",
        ],
    )?;
    Ok(out.trim().to_string())
}

/// Port of `PaneLocation` (`internal/tmux/agent.go:43`).
pub fn pane_location(target: String) -> Result<Value, String> {
    let out = match exec_output(
        "tmux.pane-location",
        &[
            "tmux",
            "display-message",
            "-p",
            "-t",
            &target,
            "#{window_index}\t#{pane_index}",
        ],
    ) {
        Ok(out) => out,
        Err(_) => return Ok(json!(["", "", false])),
    };
    let fields: Vec<&str> = out.trim().split('\t').collect();
    if fields.len() != 2 {
        return Ok(json!(["", "", false]));
    }
    Ok(json!([fields[0], fields[1], true]))
}

/// Port of `PanePID` (`internal/tmux/agent.go:56`).
pub fn pane_pid(target: String) -> Result<i64, String> {
    let out = exec_output(
        "tmux.pane-pid",
        &[
            "tmux",
            "display-message",
            "-p",
            "-t",
            &target,
            "#{pane_pid}",
        ],
    )?;
    go_atoi(out.trim())
}

/// Port of `PaneHasPiAgent` (`internal/tmux/agent.go:69`).
pub fn pane_has_pi_agent(target: String) -> Result<bool, String> {
    let target = target.trim();
    if target.is_empty() {
        return Ok(false);
    }
    if let Ok(snap) = get_snapshot(false) {
        if snap.has_pi_agent(target) {
            return Ok(true);
        }
    }
    if pane_has_pi_on_tty(target) {
        return Ok(true);
    }
    match pane_pid(target.to_string()) {
        Ok(pid) => Ok(process_tree_has_pi(pid, 5)),
        Err(_) => Ok(false),
    }
}

/// Port of `PaneIsSubagent` (`internal/tmux/agent.go:88`).
pub fn pane_is_subagent(target: String) -> Result<bool, String> {
    let target = target.trim();
    if target.is_empty() {
        return Ok(false);
    }
    if let Ok(snap) = get_snapshot(false) {
        if let Some(info) = snap.pane_info(target) {
            return Ok(info.is_subagent);
        }
    }
    Ok(pane_info_for_opts_value(target.to_string(), false)?.is_subagent)
}

/// Port of `PaneInfoFor` (`internal/tmux/agent.go:218`).
pub fn pane_info_for(target: String) -> Result<Value, String> {
    Ok(pane_info_to_value(&pane_info_for_opts_value(
        target, false,
    )?))
}

/// Port of `PaneInfoForOpts` (`internal/tmux/agent.go:223`).
pub fn pane_info_for_opts(target: String, known_pi: bool) -> Result<Value, String> {
    Ok(pane_info_to_value(&pane_info_for_opts_value(
        target, known_pi,
    )?))
}

fn pane_info_for_opts_value(target: String, known_pi: bool) -> Result<PaneInfo, String> {
    let mut info = PaneInfo {
        target: target.trim().to_string(),
        ..Default::default()
    };
    if info.target.is_empty() {
        return Ok(info);
    }

    if let Ok(snap) = get_snapshot(false) {
        if let Some(cached) = snap.pane_info(&info.target) {
            let mut cached = cached;
            if known_pi {
                cached.has_pi_agent = true;
                if cached.agent.is_empty() {
                    cached.agent = DEFAULT_AGENT.to_string();
                }
            }
            return Ok(cached);
        }
    }

    info.exists = pane_exists_value(&info.target);
    let format = "#{pane_id}\t#{session_name}\t#{window_index}\t#{pane_index}\t#{pane_tty}\t#{pane_current_command}\t#{pane_start_command}\t#{pane_current_path}";
    let out = match exec_output(
        "tmux.pane-info",
        &[
            "tmux",
            "display-message",
            "-p",
            "-t",
            &info.target,
            "-F",
            format,
        ],
    ) {
        Ok(out) => out,
        Err(_) => return Ok(info),
    };
    let fields: Vec<&str> = out.trim().split('\t').collect();
    if fields.len() < 8 {
        return Ok(info);
    }

    info.pane_id = fields[0].to_string();
    info.session_name = fields[1].to_string();
    info.window_index = fields[2].to_string();
    info.pane_index = fields[3].to_string();
    info.tty = fields[4].to_string();
    info.current_command = fields[5].to_string();
    info.start_command = fields[6].to_string();
    info.current_path = fields[7].to_string();
    if known_pi {
        info.has_pi_agent = true;
        info.agent = DEFAULT_AGENT.to_string();
    } else {
        let (agent, has_pi_agent) =
            agent_name_from_command(&info.current_command, &info.start_command);
        info.agent = agent;
        info.has_pi_agent = has_pi_agent;
    }
    info.is_subagent = is_subagent_script_line(&info.start_command)
        || is_subagent_script_line(&info.current_command);
    if !info.is_subagent {
        info.is_subagent = pane_has_subagent_script_on_tty(&info.target);
    }
    Ok(info)
}

/// Port of `ListPaneIDs` (`internal/tmux/agent.go:294`).
pub fn list_pane_i_ds() -> Result<Vec<String>, String> {
    if let Ok(snap) = get_snapshot(false) {
        let ids = snap.pane_ids();
        if !ids.is_empty() {
            return Ok(ids);
        }
    }
    let out = exec_output(
        "tmux.list-panes-ids",
        &["tmux", "list-panes", "-a", "-F", "#{pane_id}"],
    )?;
    let lines: Vec<String> = out
        .trim()
        .lines()
        .map(|line| line.to_string())
        .filter(|line| !(out.trim().lines().count() == 1 && line.is_empty()))
        .collect();
    Ok(lines)
}

/// Port of `InvalidateSnapshot` (`internal/tmux/snapshot.go:58`).
pub fn invalidate_snapshot() -> Result<(), String> {
    *lock(snapshot_slot())? = None;
    if let Ok(path) = snapshot_file_path() {
        let _ = std::fs::remove_file(path);
    }
    Ok(())
}

/// Port of `PaneExists` (`internal/tmux/tmux.go:8`).
pub fn pane_exists(target: String) -> Result<bool, String> {
    Ok(pane_exists_value(&target))
}

fn pane_exists_value(target: &str) -> bool {
    if target.trim().is_empty() {
        return false;
    }
    if let Ok(snap) = get_snapshot(false) {
        if snap.has_pane(target) {
            return true;
        }
    }
    exec_run(
        "tmux.list-panes",
        &["tmux", "list-panes", "-t", target, "-F", "#{pane_id}"],
    )
    .is_ok()
}

/// Port of `CapturePane` (`internal/tmux/tmux.go:19`).
pub fn capture_pane(target: String, lines: i64) -> Result<String, String> {
    let target = target.trim();
    if target.is_empty() {
        return Err("empty pane target".to_string());
    }

    if let Some(content) = capture_pane_visible(target) {
        return Ok(content);
    }
    if let Some(content) = capture_pane_alt_screen(target) {
        return Ok(content);
    }

    let mut lines = lines;
    if lines <= 0 {
        lines = 200;
    }
    let out = exec_output(
        "tmux.capture-pane",
        &[
            "tmux",
            "capture-pane",
            "-e",
            "-p",
            "-J",
            "-S",
            &format!("-{lines}"),
            "-t",
            target,
        ],
    )?;
    Ok(out)
}

/// Port of `capturePaneVisible` (`internal/tmux/tmux.go:49`).
fn capture_pane_visible(target: &str) -> Option<String> {
    let out = exec_output(
        "tmux.capture-pane-visible",
        &["tmux", "capture-pane", "-e", "-p", "-t", target],
    )
    .ok()?;
    if out.trim().is_empty() {
        return None;
    }
    Some(out)
}

/// Port of `capturePaneAltScreen` (`internal/tmux/tmux.go:57`).
fn capture_pane_alt_screen(target: &str) -> Option<String> {
    let out = exec_output(
        "tmux.capture-pane-alt",
        &["tmux", "capture-pane", "-e", "-p", "-a", "-q", "-t", target],
    )
    .ok()?;
    if out.trim().is_empty() {
        return None;
    }
    Some(out)
}

/// Port of `SwitchClient` (`internal/tmux/tmux.go:65`).
pub fn switch_client(target: String) -> Result<(), String> {
    let target = target.trim();
    if target.is_empty() {
        return Err("empty pane target".to_string());
    }
    if parse_target_value(target).0 == TARGET_PANE {
        exec_run("tmux.select-pane", &["tmux", "select-pane", "-t", target])?;
    }
    exec_run(
        "tmux.switch-client",
        &["tmux", "switch-client", "-t", target],
    )
}

/// Port of `ClientTTYs` (`internal/tmux/tmux.go:79`).
pub fn client_tt_ys() -> Result<Vec<String>, String> {
    let out = exec_output(
        "tmux.list-clients",
        &["tmux", "list-clients", "-F", "#{client_tty}"],
    )?;
    let mut ttys: Vec<String> = Vec::new();
    for line in out.lines() {
        let line = line.trim();
        if !line.is_empty() {
            ttys.push(line.to_string());
        }
    }
    Ok(ttys)
}

/// Port of `ClientTTY` (`internal/tmux/tmux.go:95`).
pub fn client_tty(target: String) -> Result<String, String> {
    let target = target.trim();
    if !target.is_empty() {
        if let Ok(out) = exec_output(
            "tmux.client-tty",
            &[
                "tmux",
                "display-message",
                "-p",
                "-t",
                target,
                "#{client_tty}",
            ],
        ) {
            let tty = out.trim();
            if !tty.is_empty() {
                return Ok(tty.to_string());
            }
        }
    }
    let ttys = client_tt_ys()?;
    let first = ttys
        .first()
        .ok_or_else(|| "no tmux client attached".to_string())?;
    Ok(first.to_string())
}

/// Port of `SessionName` (`internal/tmux/tmux.go:114`).
pub fn session_name(target: String) -> Result<String, String> {
    let out = exec_output(
        "tmux.session-name",
        &[
            "tmux",
            "display-message",
            "-p",
            "-t",
            &target,
            "#{session_name}",
        ],
    )?;
    Ok(out.trim().to_string())
}

/// Port of `PanePath` (`internal/tmux/tmux.go:122`).
pub fn pane_path(target: String) -> Result<String, String> {
    let out = exec_output(
        "tmux.pane-path",
        &[
            "tmux",
            "display-message",
            "-p",
            "-t",
            &target,
            "#{pane_current_path}",
        ],
    )?;
    Ok(out.trim().to_string())
}

/// Port of `RenameSession` (`internal/tmux/tmux.go:130`).
pub fn rename_session(session: String, name: String) -> Result<(), String> {
    let name = name.trim();
    if name.is_empty() {
        return Err("session name cannot be empty".to_string());
    }
    exec_run(
        "tmux.rename-session",
        &["tmux", "rename-session", "-t", &session, name],
    )
}

/// Port of `KillPane` (`internal/tmux/tmux.go:138`).
pub fn kill_pane(target: String) -> Result<(), String> {
    exec_run("tmux.kill-pane", &["tmux", "kill-pane", "-t", &target])
}

/// Port of `KillSession` (`internal/tmux/tmux.go:142`).
pub fn kill_session(name: String) -> Result<(), String> {
    exec_run("tmux.kill-session", &["tmux", "kill-session", "-t", &name])
}

/// Port of `NewWindowAtPanePath` (`internal/tmux/tmux.go:146`).
pub fn new_window_at_pane_path(target: String) -> Result<(), String> {
    let path = pane_path(target.clone())?;
    let session = session_name(target)?;
    let mut args: Vec<String> = vec!["new-window".to_string(), "-t".to_string(), session];
    if !path.is_empty() {
        args.push("-c".to_string());
        args.push(path);
    }
    let mut argv: Vec<&str> = vec!["tmux"];
    argv.extend(args.iter().map(String::as_str));
    exec_run("tmux.new-window", &argv)
}

/// Port of `TargetKind` (`internal/tmux/tmux.go:163`) as the JSON int the
/// bridge records for the tuple's first element.
const TARGET_UNKNOWN: u8 = 0;
const TARGET_PANE: u8 = 1;
const TARGET_WINDOW: u8 = 2;
const TARGET_SESSION: u8 = 3;

fn parse_target_value(target: &str) -> (u8, String) {
    let target = target.trim();
    if target.is_empty() {
        return (TARGET_UNKNOWN, String::new());
    }
    if target.starts_with('%') {
        return (TARGET_PANE, target.to_string());
    }
    if target.contains(':') {
        return (TARGET_WINDOW, target.to_string());
    }
    (TARGET_SESSION, target.to_string())
}

/// Port of `ParseTarget` (`internal/tmux/tmux.go:173`).
pub fn parse_target(target: String) -> Result<Value, String> {
    let (kind, parsed) = parse_target_value(&target);
    Ok(json!([kind, parsed]))
}
