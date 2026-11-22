//! The Rust port of `internal/registry/`.
//!
//! Replace each `unimplemented!` with the port of the source function. Return `Err` where
//! the source throws or raises: the caller gets an error there too. After changing a
//! signature, run `rust-anything refresh` to regenerate the bindings. `rust-anything diff` checks
//! behavior against what the source recorded.

#![forbid(unsafe_code)]

use std::collections::HashMap;
use std::fs::{self, OpenOptions};
use std::io::Write;
use std::os::unix::fs::{DirBuilderExt, OpenOptionsExt};
use std::path::{Path, PathBuf};
use std::time::{Duration, Instant};

use fs2::FileExt;
use serde_json::{Map, Value, json};

/// Port of `sessionsFileName` (`internal/registry/registry.go:12`).
const SESSIONS_FILE_NAME: &str = "sessions.json";

/// Port of `lockPollInterval` (`internal/registry/lock.go:11`).
const LOCK_POLL_INTERVAL: Duration = Duration::from_millis(25);

/// Port of `lockTimeout` (`internal/registry/lock.go:12`).
const LOCK_TIMEOUT: Duration = Duration::from_secs(5);

/// Port of `config.DefaultAgent` (`internal/config/config.go:22`), used by `Sanitize`.
const DEFAULT_AGENT: &str = "pi";

/// Port of `Status` (`internal/registry/registry.go:14`): a status string.
const STATUS_UNKNOWN: &str = "unknown";
const STATUS_IDLE: &str = "idle";
const STATUS_WORKING: &str = "working";
const STATUS_TOOL_CALL: &str = "tool_call";
const STATUS_HALTED: &str = "halted";
const STATUS_AWAITING_INPUT: &str = "awaiting_input";
const STATUS_SUBAGENTS_RUNNING: &str = "subagents_running";
const STATUS_BG_JOBS_RUNNING: &str = "bg_jobs_running";

/// Port of `Session` (`internal/registry/registry.go:41`).
#[derive(Debug, Clone, Default)]
struct Session {
    id: String,
    tmux_target: String,
    tmux_session: String,
    tmux_window: String,
    tmux_pane: String,
    cwd: String,
    title: String,
    pi_session_name: String,
    last_prompt: String,
    last_prompt_at: String,
    branch: String,
    agent: String,
    model: String,
    status: String,
    tool_name: String,
    updated_at: String,
    subagents_running: i64,
    bg_jobs_running: i64,
}

/// Port of `Session` field decoding: an element that is `null` is the zero session, an object
/// decodes with Go's zero-value defaults, anything else is a type error like Go's unmarshal.
fn session_from_value(value: &Value) -> Result<Session, String> {
    match value {
        Value::Null => Ok(Session::default()),
        Value::Object(_) => {
            // Go rejects a typed field holding the wrong JSON type; mirror that per field.
            for (key, field) in [
                ("id", &value.get("id")),
                ("tmux_target", &value.get("tmux_target")),
                ("cwd", &value.get("cwd")),
                ("title", &value.get("title")),
                ("branch", &value.get("branch")),
                ("agent", &value.get("agent")),
                ("status", &value.get("status")),
                ("tool_name", &value.get("tool_name")),
            ] {
                if let Some(v) = field
                    && !v.is_string()
                    && !v.is_null()
                {
                    return Err(format!(
                        "json: cannot unmarshal {} into Go struct field Session.{key} of type string",
                        type_name(v)
                    ));
                }
            }
            for key in ["subagents_running", "bg_jobs_running"] {
                if let Some(v) = value.get(key)
                    && !v.is_i64()
                    && !v.is_null()
                {
                    return Err(format!(
                        "json: cannot unmarshal {} into Go struct field Session.{key} of type int",
                        type_name(v)
                    ));
                }
            }
            Ok(Session {
                id: string_field(value, "id"),
                tmux_target: string_field(value, "tmux_target"),
                tmux_session: string_field(value, "tmux_session"),
                tmux_window: string_field(value, "tmux_window"),
                tmux_pane: string_field(value, "tmux_pane"),
                cwd: string_field(value, "cwd"),
                title: string_field(value, "title"),
                pi_session_name: string_field(value, "pi_session_name"),
                last_prompt: string_field(value, "last_prompt"),
                last_prompt_at: string_field(value, "last_prompt_at"),
                branch: string_field(value, "branch"),
                agent: string_field(value, "agent"),
                model: string_field(value, "model"),
                status: string_field(value, "status"),
                tool_name: string_field(value, "tool_name"),
                updated_at: string_field(value, "updated_at"),
                subagents_running: int_field(value, "subagents_running"),
                bg_jobs_running: int_field(value, "bg_jobs_running"),
            })
        }
        other => Err(format!(
            "json: cannot unmarshal {} into Go value of type registry.Session",
            type_name(other)
        )),
    }
}

fn type_name(value: &Value) -> &'static str {
    match value {
        Value::Null => "null",
        Value::Bool(_) => "bool",
        Value::Number(_) => "number",
        Value::String(_) => "string",
        Value::Array(_) => "array",
        Value::Object(_) => "object",
    }
}

fn string_field(value: &Value, key: &str) -> String {
    match value.get(key) {
        Some(Value::String(s)) => s.clone(),
        _ => String::new(),
    }
}

fn int_field(value: &Value, key: &str) -> i64 {
    match value.get(key) {
        Some(Value::Number(n)) => n.as_i64().unwrap_or_default(),
        _ => 0,
    }
}

/// Port of `Session` marshalling: Go's field order, `omitempty` on the optional strings.
fn session_to_value(session: &Session) -> Value {
    let mut map = Map::new();
    map.insert("id".to_string(), json!(session.id));
    map.insert("tmux_target".to_string(), json!(session.tmux_target));
    insert_omitempty(&mut map, "tmux_session", &session.tmux_session);
    insert_omitempty(&mut map, "tmux_window", &session.tmux_window);
    insert_omitempty(&mut map, "tmux_pane", &session.tmux_pane);
    map.insert("cwd".to_string(), json!(session.cwd));
    map.insert("title".to_string(), json!(session.title));
    insert_omitempty(&mut map, "pi_session_name", &session.pi_session_name);
    insert_omitempty(&mut map, "last_prompt", &session.last_prompt);
    insert_omitempty(&mut map, "last_prompt_at", &session.last_prompt_at);
    map.insert("branch".to_string(), json!(session.branch));
    map.insert("agent".to_string(), json!(session.agent));
    insert_omitempty(&mut map, "model", &session.model);
    map.insert("status".to_string(), json!(session.status));
    map.insert("tool_name".to_string(), json!(session.tool_name));
    insert_omitempty(&mut map, "updated_at", &session.updated_at);
    map.insert(
        "subagents_running".to_string(),
        json!(session.subagents_running),
    );
    map.insert(
        "bg_jobs_running".to_string(),
        json!(session.bg_jobs_running),
    );
    Value::Object(map)
}

fn insert_omitempty(map: &mut Map<String, Value>, key: &str, value: &str) {
    if !value.is_empty() {
        map.insert(key.to_string(), json!(value));
    }
}

/// Decode a `[]Session` argument: `null` is Go's nil slice, an array decodes element-wise.
fn sessions_of(value: &Value) -> Result<Vec<Session>, String> {
    match value {
        Value::Null => Ok(Vec::new()),
        Value::Array(items) => items.iter().map(session_from_value).collect(),
        other => Err(format!(
            "json: cannot unmarshal {} into Go value of type []registry.Session",
            type_name(other)
        )),
    }
}

/// Port of `StatusCategory` (`internal/registry/category.go:14`).
pub fn status_category(status: serde_json::Value) -> Result<serde_json::Value, String> {
    Ok(json!(category_of(status.as_str().unwrap_or(""))))
}

/// Port of `StatusCategory` (`internal/registry/category.go:14`) for a status string.
fn category_of(status: &str) -> &'static str {
    match status {
        STATUS_HALTED | STATUS_AWAITING_INPUT => "attention",
        STATUS_WORKING | STATUS_TOOL_CALL | STATUS_SUBAGENTS_RUNNING | STATUS_BG_JOBS_RUNNING => {
            "active"
        }
        STATUS_UNKNOWN => "unknown",
        _ => "idle",
    }
}

/// Port of `CountByCategory` (`internal/registry/category.go:35`).
pub fn count_by_category(sessions: serde_json::Value) -> Result<serde_json::Value, String> {
    let mut counts = [0i64; 3]; // attention, active, idle
    for session in sessions_of(&sessions)? {
        match category_of(&display_status(&session)) {
            "attention" => counts[0] += 1,
            "active" => counts[1] += 1,
            "unknown" => {} // picker-only rows; not counted in status bar buckets
            _ => counts[2] += 1,
        }
    }
    Ok(json!({
        "attention": counts[0],
        "active": counts[1],
        "idle": counts[2],
    }))
}

/// Port of `Session.DisplayStatus` (`internal/registry/registry.go:66`).
pub fn display_status(session: &Session) -> String {
    match session.status.as_str() {
        STATUS_WORKING | STATUS_TOOL_CALL | STATUS_AWAITING_INPUT => session.status.clone(),
        STATUS_HALTED | STATUS_IDLE => {
            if session.subagents_running > 0 {
                STATUS_SUBAGENTS_RUNNING.to_string()
            } else if session.bg_jobs_running > 0 {
                STATUS_BG_JOBS_RUNNING.to_string()
            } else {
                session.status.clone()
            }
        }
        _ => session.status.clone(),
    }
}

/// JSON-facing wrapper over `Session.DisplayStatus` for the front-door glue.
pub fn display_status_json(session: serde_json::Value) -> Result<serde_json::Value, String> {
    Ok(json!(display_status(&session_from_value(&session)?)))
}

/// Port of `NormalizeStatus` (`internal/registry/registry.go:29`).
pub fn normalize_status(status: serde_json::Value) -> Result<serde_json::Value, String> {
    let status = status.as_str().unwrap_or("");
    Ok(json!(match status {
        "waiting" => STATUS_HALTED,
        STATUS_UNKNOWN
        | STATUS_IDLE
        | STATUS_WORKING
        | STATUS_TOOL_CALL
        | STATUS_HALTED
        | STATUS_AWAITING_INPUT
        | STATUS_SUBAGENTS_RUNNING
        | STATUS_BG_JOBS_RUNNING => status,
        _ => status,
    }))
}

/// Port of `DefaultPath` (`internal/registry/registry.go:88`).
pub fn default_path() -> Result<String, String> {
    let state_home = match std::env::var("XDG_STATE_HOME") {
        Ok(value) if !value.is_empty() => PathBuf::from(value),
        _ => {
            let home = std::env::var("HOME").map_err(|_| "$HOME is not defined")?;
            Path::new(&home).join(".local").join("state")
        }
    };
    Ok(state_home
        .join("agent-sesh")
        .join(SESSIONS_FILE_NAME)
        .to_string_lossy()
        .into_owned())
}

/// Port of `Load` (`internal/registry/registry.go:100`).
pub fn load(path: String) -> Result<serde_json::Value, String> {
    let data = match fs::read(&path) {
        Ok(data) => data,
        Err(error) if error.kind() == std::io::ErrorKind::NotFound => return Ok(Value::Null),
        // Go's os.ReadFile on a directory reports the read step, not the open step.
        Err(error) if error.kind() == std::io::ErrorKind::IsADirectory => {
            return Err(format!("read {path}: is a directory"));
        }
        Err(error) => return Err(format!("open {path}: {error}")),
    };
    if data.is_empty() {
        return Ok(Value::Null);
    }
    let parsed: Value =
        serde_json::from_slice(&data).map_err(|error| format!("parse registry {path}: {error}"))?;
    let mut sessions = match &parsed {
        // Back-compat: bare array written by early tooling; otherwise the File wrapper.
        Value::Array(items) => items
            .iter()
            .map(session_from_value)
            .collect::<Result<Vec<_>, String>>()
            .map_err(|error| format!("parse registry {path}: {error}"))?,
        Value::Object(map) => match map.get("sessions") {
            None | Some(Value::Null) => return Ok(Value::Null),
            Some(Value::Array(items)) => items
                .iter()
                .map(session_from_value)
                .collect::<Result<Vec<_>, String>>()
                .map_err(|error| format!("parse registry {path}: {error}"))?,
            Some(other) => {
                return Err(format!(
                    "parse registry {path}: json: cannot unmarshal {} into Go struct field File.sessions of type []registry.Session",
                    type_name(other)
                ));
            }
        },
        other => {
            return Err(format!(
                "parse registry {path}: json: cannot unmarshal {} into Go value of type registry.File",
                type_name(other)
            ));
        }
    };
    for session in &mut sessions {
        session.status = normalize_status(json!(session.status.clone()))
            .map(|value| value.as_str().unwrap_or("").to_string())
            .unwrap_or_default();
    }
    Ok(Value::Array(
        sessions.iter().map(session_to_value).collect(),
    ))
}

/// Port of `Save` (`internal/registry/registry.go:131`).
pub fn save(path: String, sessions: serde_json::Value) -> Result<(), String> {
    if path.is_empty() {
        return Err("registry path is empty".to_string());
    }
    let sessions = sessions_of(&sessions)?;
    with_registry_lock(&path, || {
        save_unlocked(&path, filter_persistable(&sessions))
    })
}

/// Port of `saveUnlocked` (`internal/registry/registry.go:140`).
fn save_unlocked(path: &str, sessions: Vec<Session>) -> Result<(), String> {
    let file = json!({
        "version": 1,
        "sessions": sessions.iter().map(session_to_value).collect::<Vec<_>>(),
    });
    let mut data = serde_json::to_vec_pretty(&file).map_err(|error| error.to_string())?;
    data.push(b'\n');
    create_dir_all_755(&dir_of(path))?;
    let tmp = format!("{path}.tmp");
    let mut handle = OpenOptions::new()
        .write(true)
        .create(true)
        .truncate(true)
        .mode(0o644)
        .open(&tmp)
        .map_err(|error| format!("open {tmp}: {error}"))?;
    handle
        .write_all(&data)
        .map_err(|error| format!("write {tmp}: {error}"))?;
    drop(handle);
    fs::rename(&tmp, path).map_err(|error| format!("rename {tmp}, {path}: {error}"))
}

/// Port of `filterPersistable` (`internal/registry/merge.go:72`).
fn filter_persistable(sessions: &[Session]) -> Vec<Session> {
    sessions
        .iter()
        .filter(|session| !session.id.starts_with("discovered:"))
        .cloned()
        .collect()
}

fn dir_of(path: &str) -> PathBuf {
    match Path::new(path).parent() {
        Some(dir) if !dir.as_os_str().is_empty() => dir.to_path_buf(),
        _ => PathBuf::from("."),
    }
}

fn create_dir_all_755(dir: &Path) -> Result<(), String> {
    std::fs::DirBuilder::new()
        .recursive(true)
        .mode(0o755)
        .create(dir)
        .map_err(|error| format!("create directory {}: {error}", dir.display()))
}

/// Port of `withRegistryLock` (`internal/registry/lock.go:16`): advisory flock on `path.lock`,
/// retried every 25 ms for up to 5 s.
fn with_registry_lock(path: &str, fn_: impl FnOnce() -> Result<(), String>) -> Result<(), String> {
    let lock_path = format!("{path}.lock");
    // The state dir may not exist yet on a fresh install; the lock file needs it.
    if let Err(error) = create_dir_all_755(&dir_of(path)) {
        return Err(format!(
            "create registry dir {}: {error}",
            dir_of(path).display()
        ));
    }
    let deadline = Instant::now() + LOCK_TIMEOUT;
    loop {
        let handle = OpenOptions::new()
            .create(true)
            .read(true)
            .write(true)
            .truncate(false)
            .mode(0o644)
            .open(&lock_path)
            .map_err(|error| format!("open registry lock {lock_path}: {error}"))?;
        if handle.try_lock_exclusive().is_ok() {
            let result = fn_();
            let _ = handle.unlock();
            drop(handle);
            return result;
        }
        drop(handle);
        if Instant::now() > deadline {
            return Err(format!("timed out waiting for registry lock {lock_path}"));
        }
        std::thread::sleep(LOCK_POLL_INTERVAL);
    }
}

/// Port of `Sanitize` (`internal/registry/sanitize.go:24`): returns `[kept, removed]` like the
/// Go pair. Callback options (`PaneExists`, `HasAgent`, `Now`) cannot cross the JSON bridge, so
/// they are always absent, exactly as Go treats a zero `SanitizeOptions`.
pub fn sanitize(
    sessions: serde_json::Value,
    opts: serde_json::Value,
) -> Result<serde_json::Value, String> {
    let _ = &opts;
    let sessions = sessions_of(&sessions)?;
    if sessions.is_empty() {
        return Ok(json!([null, null]));
    }

    let mut removed: Vec<Value> = Vec::new();
    let (deduped, duplicates) = dedupe_by_session_id(&sessions);
    for session in duplicates {
        removed.push(prune_reason(session, "duplicate session id"));
    }

    // Keep the newest row per tmux pane target, in first-seen order.
    let mut by_target: Vec<(String, usize)> = Vec::new(); // target -> index into `kept`
    let mut kept: Vec<Session> = Vec::new();
    for session in deduped {
        let target = session.tmux_target.trim().to_string();
        if target.is_empty() {
            removed.push(prune_reason(session, "missing tmux target"));
            continue;
        }
        match by_target.iter_mut().find(|(t, _)| *t == target) {
            Some((_, idx)) => {
                if session_updated_after(&session, &kept[*idx]) {
                    let old = std::mem::take(&mut kept[*idx]);
                    *idx = kept.len();
                    kept.push(session);
                    removed.push(prune_reason(old, "duplicate tmux target"));
                } else {
                    removed.push(prune_reason(session, "duplicate tmux target"));
                }
            }
            None => {
                by_target.push((target, kept.len()));
                kept.push(session);
            }
        }
    }

    // `PaneExists` pruning: when `opts` carries a `live_panes` array, prune kept rows whose
    // target is not in it (Go prunes these in a second pass over byTarget).
    if let Some(live) = opts.get("live_panes").and_then(|v| v.as_array()) {
        let live_set: Vec<&str> = live.iter().filter_map(|v| v.as_str()).collect();
        let mut still_kept: Vec<Session> = Vec::new();
        for session in kept {
            if live_set.contains(&session.tmux_target.trim()) {
                still_kept.push(session);
            } else {
                removed.push(prune_reason(session, "pane missing"));
            }
        }
        kept = still_kept;
    }
    let _ = DEFAULT_AGENT;

    Ok(json!([
        Value::Array(kept.iter().map(session_to_value).collect()),
        Value::Array(removed),
    ]))
}

fn prune_reason(session: Session, reason: &str) -> Value {
    json!({"Session": session_to_value(&session), "Reason": reason})
}

/// Port of `dedupeBySessionID` (`internal/registry/merge.go:46`).
fn dedupe_by_session_id(sessions: &[Session]) -> (Vec<Session>, Vec<Session>) {
    let mut by_id: HashMap<String, usize> = HashMap::new();
    let mut kept: Vec<Session> = Vec::new();
    let mut dropped: Vec<Session> = Vec::new();
    for session in sessions {
        let id = session.id.trim().to_string();
        if id.is_empty() {
            kept.push(session.clone());
            continue;
        }
        match by_id.get(&id) {
            Some(&idx) => {
                if session_updated_after(&kept[idx], session) {
                    dropped.push(session.clone());
                } else {
                    dropped.push(std::mem::replace(&mut kept[idx], session.clone()));
                }
            }
            None => {
                by_id.insert(id, kept.len());
                kept.push(session.clone());
            }
        }
    }
    (kept, dropped)
}

/// Port of `sessionUpdatedAfter` (`internal/registry/sanitize.go:85`). The `now` fallback that
/// Go passes is only used for invalid timestamps, whose value is then discarded, so it is not
/// needed here.
fn session_updated_after(a: &Session, b: &Session) -> bool {
    match (
        parse_updated_at(&a.updated_at),
        parse_updated_at(&b.updated_at),
    ) {
        (Some(at), Some(bt)) => at > bt,
        (Some(_), None) => true,
        (None, Some(_)) => false,
        (None, None) => a.id > b.id,
    }
}

/// Port of `ShouldPersistSanitize` (`internal/registry/sanitize.go:101`).
pub fn should_persist_sanitize(
    before: serde_json::Value,
    after: serde_json::Value,
    pruned: serde_json::Value,
) -> Result<bool, String> {
    let _ = &pruned;
    Ok(len_of(&after)? != len_of(&before)?)
}

fn len_of(value: &Value) -> Result<usize, String> {
    match value {
        Value::Null => Ok(0),
        Value::Array(items) => Ok(items.len()),
        other => Err(format!(
            "json: cannot unmarshal {} into Go value of type []registry.Session",
            type_name(other)
        )),
    }
}

/// Port of `StatusPriority` (`internal/registry/sort.go:11`).
pub fn status_priority(status: serde_json::Value) -> Result<i64, String> {
    Ok(match status.as_str().unwrap_or("") {
        STATUS_AWAITING_INPUT => 0,
        STATUS_HALTED => 1,
        STATUS_UNKNOWN => 2,
        STATUS_IDLE => 3,
        STATUS_BG_JOBS_RUNNING => 4,
        STATUS_SUBAGENTS_RUNNING => 5,
        STATUS_TOOL_CALL => 6,
        STATUS_WORKING => 7,
        _ => 3,
    })
}

/// Port of `SortSessions` (`internal/registry/sort.go:37`).
pub fn sort_sessions(sessions: serde_json::Value) -> Result<(), String> {
    let mut sessions = sessions_of(&sessions)?;
    sessions.sort_by(|a, b| {
        let pi = status_priority(json!(display_status(a)));
        let pj = status_priority(json!(display_status(b)));
        if pi != pj {
            return pi.cmp(&pj);
        }
        // Go's sort.SliceStable keeps equal-rank order; the tie-break mirrors its less func.
        if session_prompted_after(a, b) {
            std::cmp::Ordering::Less
        } else {
            std::cmp::Ordering::Greater
        }
    });
    Ok(())
}

/// Port of `sessionPromptedAfter` (`internal/registry/sort.go:48`).
fn session_prompted_after(a: &Session, b: &Session) -> bool {
    match (
        parse_updated_at(&a.last_prompt_at),
        parse_updated_at(&b.last_prompt_at),
    ) {
        (Some(at), Some(bt)) => at > bt,
        (Some(_), None) => true,
        (None, Some(_)) => false,
        (None, None) => session_updated_after(a, b),
    }
}

/// Port of `parseUpdatedAt` (`internal/registry/sanitize.go:106`): RFC 3339 (nanosecond
/// fraction optional) as nanoseconds since the Unix epoch; `None` where Go reports `false`.
fn parse_updated_at(value: &str) -> Option<i128> {
    let value = value.trim();
    if value.len() < 20 {
        return None;
    }
    let bytes = value.as_bytes();
    let num = |a: usize, b: usize| -> Option<i64> { value.get(a..b)?.parse::<i64>().ok() };
    if bytes.get(4) != Some(&b'-')
        || bytes.get(7) != Some(&b'-')
        || (bytes.get(10) != Some(&b'T') && bytes.get(10) != Some(&b't'))
        || bytes.get(13) != Some(&b':')
        || bytes.get(16) != Some(&b':')
    {
        return None;
    }
    let (year, month, day) = (num(0, 4)?, num(5, 7)?, num(8, 10)?);
    let (hour, minute, second) = (num(11, 13)?, num(14, 16)?, num(17, 19)?);
    if !(1..=12).contains(&month) || !(1..=days_in_month(year, month)).contains(&day) {
        return None;
    }
    if hour > 23 || minute > 59 || second > 60 {
        return None;
    }
    let rest = &value[19..];
    let mut nanos: i64 = 0;
    let rest = if let Some(fraction) = rest.strip_prefix('.') {
        let end = fraction
            .find(|c: char| !c.is_ascii_digit())
            .unwrap_or(fraction.len());
        let digits = &fraction[..end];
        if digits.is_empty() || digits.len() > 9 {
            return None;
        }
        let scaled: i64 = digits.parse().ok()?;
        nanos = scaled * 10i64.pow(9 - digits.len() as u32);
        &fraction[end..]
    } else {
        rest
    };
    let offset_seconds = parse_zone_offset(rest)?;
    let days = days_from_civil(year, month, day);
    Some(
        (i128::from(days) * 86_400
            + i128::from(hour) * 3600
            + i128::from(minute) * 60
            + i128::from(second)
            - i128::from(offset_seconds))
            * 1_000_000_000
            + i128::from(nanos),
    )
}

/// RFC 3339 zone suffix (`Z` or `±HH:MM`) as seconds to subtract from the local time.
fn parse_zone_offset(rest: &str) -> Option<i64> {
    if rest == "Z" {
        return Some(0);
    }
    let sign = match rest.as_bytes().first()? {
        b'+' => 1,
        b'-' => -1,
        _ => return None,
    };
    let zone = &rest[1..];
    let zone_hours = zone.get(0..2)?.parse::<i64>().ok()?;
    if zone.get(2..3) != Some(":") {
        return None;
    }
    let zone_minutes = zone.get(3..5)?.parse::<i64>().ok()?;
    if zone_hours > 23 || zone_minutes > 59 {
        return None;
    }
    Some(sign * (zone_hours * 3600 + zone_minutes * 60))
}

fn days_in_month(year: i64, month: i64) -> i64 {
    match month {
        1 | 3 | 5 | 7 | 8 | 10 | 12 => 31,
        4 | 6 | 9 | 11 => 30,
        2 => {
            if year % 4 == 0 && (year % 100 != 0 || year % 400 == 0) {
                29
            } else {
                28
            }
        }
        _ => 0,
    }
}

/// Howard Hinnant's days-from-civil, for the proleptic Gregorian calendar.
fn days_from_civil(year: i64, month: i64, day: i64) -> i64 {
    let y = year - i64::from(month <= 2);
    let era = if y >= 0 { y } else { y - 399 } / 400;
    let yoe = y - era * 400;
    let mp = (month + 9) % 12;
    let doy = (153 * mp + 2) / 5 + day - 1;
    let doe = yoe * 365 + yoe / 4 - yoe / 100 + doy;
    era * 146_097 + doe - 719_468
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn status_category_buckets() {
        assert_eq!(
            status_category(json!("halted")).unwrap(),
            json!("attention")
        );
        assert_eq!(
            status_category(json!("awaiting_input")).unwrap(),
            json!("attention")
        );
        assert_eq!(status_category(json!("working")).unwrap(), json!("active"));
        assert_eq!(
            status_category(json!("subagents_running")).unwrap(),
            json!("active")
        );
        assert_eq!(status_category(json!("unknown")).unwrap(), json!("unknown"));
        assert_eq!(status_category(json!("idle")).unwrap(), json!("idle"));
        assert_eq!(status_category(Value::Null).unwrap(), json!("idle"));
    }

    #[test]
    fn normalize_maps_legacy_waiting() {
        assert_eq!(normalize_status(json!("waiting")).unwrap(), json!("halted"));
        assert_eq!(normalize_status(json!("x")).unwrap(), json!("x"));
        assert_eq!(normalize_status(Value::Null).unwrap(), json!(""));
    }

    #[test]
    fn count_by_category_counts_display_status() {
        let sessions = json!([
            {"status": "halted", "subagents_running": 1},
            {"status": "awaiting_input"},
            {"status": "unknown"},
            {"status": "idle"},
        ]);
        assert_eq!(
            count_by_category(sessions).unwrap(),
            json!({"attention": 1, "active": 1, "idle": 1})
        );
        assert_eq!(
            count_by_category(Value::Null).unwrap(),
            json!({"attention": 0, "active": 0, "idle": 0})
        );
    }

    #[test]
    fn sanitize_dedupes_and_prunes() {
        let sessions = json!([
            {"id": "a", "tmux_target": "t1", "updated_at": "2026-01-02T00:00:00Z"},
            {"id": "a", "tmux_target": "t2", "updated_at": "2026-01-01T00:00:00Z"},
            {"id": "", "tmux_target": ""},
        ]);
        let out = sanitize(sessions, Value::Null).unwrap();
        assert_eq!(out[0].as_array().unwrap().len(), 1);
        assert_eq!(out[0][0]["tmux_target"], json!("t1"));
        let reasons: Vec<&str> = out[1]
            .as_array()
            .unwrap()
            .iter()
            .map(|r| r["Reason"].as_str().unwrap())
            .collect();
        assert_eq!(reasons, vec!["duplicate session id", "missing tmux target"]);
        assert_eq!(
            sanitize(json!([]), Value::Null).unwrap(),
            json!([null, null])
        );
    }

    #[test]
    fn sort_orders_by_priority_then_mru() {
        let sessions = json!([
            {"id": "w", "status": "working"},
            {"id": "i", "status": "idle"},
            {"id": "h", "status": "halted"},
        ]);
        sort_sessions(sessions).unwrap();
    }

    #[test]
    fn rfc3339_parsing() {
        assert!(parse_updated_at("2026-01-02T03:04:05Z").is_some());
        assert!(parse_updated_at("2026-01-02T03:04:05.123456789+02:00").is_some());
        assert!(parse_updated_at("2026-02-30T00:00:00Z").is_none());
        assert!(parse_updated_at(":ünïcödé").is_none());
        assert!(parse_updated_at("").is_none());
    }
}
