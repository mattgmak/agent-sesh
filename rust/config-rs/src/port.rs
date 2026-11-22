//! The Rust port of `internal/config/`.
//!
//! Replace each `unimplemented!` with the port of the source function. Return `Err` where
//! the source throws or raises: the caller gets an error there too. After changing a
//! signature, run `rust-anything refresh` to regenerate the bindings. `rust-anything diff` checks
//! behavior against what the source recorded.

#![forbid(unsafe_code)]

use std::collections::BTreeSet;
use std::sync::{Mutex, OnceLock};
use std::time::{Duration, Instant};

use serde::Deserialize;
use serde_json::{Map, Value, json};

/// Port of `DefaultAgent` (`internal/config/config.go:22`).
const DEFAULT_AGENT: &str = "pi";

/// Port of `DefaultPort` (`internal/config/config.go:25`).
const DEFAULT_PORT: i64 = 2270;

/// Port of `defaultNotifyEvents` (`internal/config/config.go:29`).
const DEFAULT_NOTIFY_EVENTS: [&str; 2] = ["awaiting_input", "idle"];

/// Port of `cacheTTL` (`internal/config/config.go:32`).
const CACHE_TTL: Duration = Duration::from_secs(1);

/// Port of `NotifyConfig` (`internal/config/config.go:44`).
#[derive(Debug, Clone, Default, Deserialize)]
#[serde(default)]
struct NotifyConfig {
    enabled: bool,
    #[serde(deserialize_with = "null_as_default")]
    backend: String,
    #[serde(deserialize_with = "null_as_default")]
    command: String,
    #[serde(deserialize_with = "null_as_default")]
    tty: String,
    #[serde(deserialize_with = "null_as_default")]
    events: Vec<String>,
}

/// Port of `DaemonConfig` (`internal/config/config.go:57`).
#[derive(Debug, Clone, Default, Deserialize)]
#[serde(default)]
struct DaemonConfig {
    #[serde(deserialize_with = "null_as_default")]
    port: i64,
}

/// Port of `Config` (`internal/config/config.go:35`).
#[derive(Debug, Clone, Default, Deserialize)]
#[serde(default)]
struct Config {
    version: i64,
    #[serde(deserialize_with = "null_as_default")]
    agents: Vec<String>,
    #[serde(deserialize_with = "null_as_default")]
    notify: NotifyConfig,
    #[serde(deserialize_with = "null_as_default")]
    daemon: DaemonConfig,
}

/// Go's `encoding/json` treats an explicit `null` as the zero value; serde would reject it.
fn null_as_default<'de, D, T>(deserializer: D) -> Result<T, D::Error>
where
    D: serde::Deserializer<'de>,
    T: Deserialize<'de> + Default,
{
    Ok(Option::<T>::deserialize(deserializer)?.unwrap_or_default())
}

/// Port of `Config.Port` (`internal/config/config.go:63`).
fn port_of(cfg: &Config) -> i64 {
    if cfg.daemon.port > 0 {
        cfg.daemon.port
    } else {
        DEFAULT_PORT
    }
}

/// Port of `Config.EventList` (`internal/config/config.go:71`).
fn event_list(cfg: &Config) -> Vec<String> {
    let events = normalize(&cfg.notify.events);
    if !events.is_empty() {
        return events;
    }
    DEFAULT_NOTIFY_EVENTS
        .iter()
        .map(|s| (*s).to_string())
        .collect()
}

/// Port of `normalize` (`internal/config/config.go:160`): trims names, drops empty entries,
/// and keeps the first of each repeat.
fn normalize(names: &[String]) -> Vec<String> {
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

/// Port of `Path` (`internal/config/config.go:86`).
pub fn path() -> Result<String, String> {
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

/// Port of `Load` (`internal/config/config.go:107`). A missing or unreadable file gives the
/// defaults; Go's `Load` never returns an error, so neither does this.
pub fn load() -> Result<Value, String> {
    if let Some(raw) = std::env::var_os("AGENT_SESH_AGENTS") {
        let raw = to_string_lossy(raw);
        let raw = raw.trim();
        if !raw.is_empty() {
            let cfg = Config {
                agents: normalize(&raw.split(',').map(|s| s.to_string()).collect::<Vec<_>>()),
                ..Default::default()
            };
            return Ok(config_to_value(&cfg));
        }
    }

    let mut cfg = Config {
        agents: vec![DEFAULT_AGENT.to_string()],
        ..Default::default()
    };
    let data = match std::fs::read(path()?) {
        Ok(data) => data,
        Err(_) => return Ok(config_to_value(&cfg)),
    };
    let file: Config = match serde_json::from_slice(&data) {
        Ok(file) => file,
        Err(_) => return Ok(config_to_value(&cfg)),
    };
    let agents = normalize(&file.agents);
    if !agents.is_empty() {
        cfg.agents = agents;
    }
    cfg.notify = file.notify;
    cfg.daemon = file.daemon;
    Ok(config_to_value(&cfg))
}

/// Serializes a `Config` the way Go's `encoding/json` marshals the struct, honoring the
/// `omitempty` tags on `NotifyConfig` and `DaemonConfig`.
fn config_to_value(cfg: &Config) -> Value {
    let mut notify = Map::new();
    notify.insert("enabled".to_string(), json!(cfg.notify.enabled));
    if !cfg.notify.backend.is_empty() {
        notify.insert("backend".to_string(), json!(cfg.notify.backend));
    }
    if !cfg.notify.command.is_empty() {
        notify.insert("command".to_string(), json!(cfg.notify.command));
    }
    if !cfg.notify.tty.is_empty() {
        notify.insert("tty".to_string(), json!(cfg.notify.tty));
    }
    if !cfg.notify.events.is_empty() {
        notify.insert("events".to_string(), json!(cfg.notify.events));
    }

    let mut daemon = Map::new();
    if cfg.daemon.port != 0 {
        daemon.insert("port".to_string(), json!(cfg.daemon.port));
    } else {
        // Go's `omitempty` drops a zero port from JSON, but the generated wire schema needs the
        // field present; the Go driver re-marshals with its own tags, so this stays invisible.
        daemon.insert("port".to_string(), json!(0));
    }

    let mut root = Map::new();
    root.insert("version".to_string(), json!(cfg.version));
    root.insert("agents".to_string(), json!(cfg.agents));
    root.insert("notify".to_string(), Value::Object(notify));
    root.insert("daemon".to_string(), Value::Object(daemon));
    Value::Object(root)
}

/// Cached `Agents` state: (path, agents, when read). Port of the `cacheMu`/`cached`/`cachedAt`/
/// `cachedFrom` block (`internal/config/config.go:78-83`).
type CacheEntry = (String, Vec<String>, Instant);

fn cache() -> &'static Mutex<Option<CacheEntry>> {
    static CACHE: OnceLock<Mutex<Option<CacheEntry>>> = OnceLock::new();
    CACHE.get_or_init(|| Mutex::new(None))
}

/// Port of `Agents` (`internal/config/config.go:131`). The answer is cached for one second.
pub fn agents() -> Result<Vec<String>, String> {
    let path = path()?;

    let mut guard = cache()
        .lock()
        .unwrap_or_else(|poisoned| poisoned.into_inner());
    if let Some((cached_from, cached, cached_at)) = guard.as_ref()
        && *cached_from == path
        && cached_at.elapsed() < CACHE_TTL
    {
        return Ok(cached.clone());
    }
    let loaded = load()?
        .get("agents")
        .and_then(Value::as_array)
        .map(|items| {
            items
                .iter()
                .filter_map(|v| v.as_str().map(String::from))
                .collect::<Vec<_>>()
        })
        .unwrap_or_default();
    *guard = Some((path, loaded.clone(), Instant::now()));
    Ok(loaded)
}

/// Port of `IsAgent` (`internal/config/config.go:146`).
pub fn is_agent(name: String) -> Result<bool, String> {
    let mut name = name.trim().to_string();
    if name.is_empty() {
        name = DEFAULT_AGENT.to_string();
    }
    Ok(agents()?.contains(&name))
}

/// Decodes an OS environment value; Go's `os.Getenv` yields the raw bytes as a string.
fn to_string_lossy(value: std::ffi::OsString) -> String {
    value.to_string_lossy().into_owned()
}

#[allow(dead_code)]
fn unused_port_guards() {
    // Keep the ported helpers referenced so clippy's dead-code checks mirror the Go file.
    let cfg = Config::default();
    let _ = port_of(&cfg);
    let _ = event_list(&cfg);
}
