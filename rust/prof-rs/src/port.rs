//! The Rust port of `internal/prof/`.
//!
//! Replace each `unimplemented!` with the port of the source function. Return `Err` where
//! the source throws or raises: the caller gets an error there too. After changing a
//! signature, run `rust-anything refresh` to regenerate the bindings. `rust-anything diff` checks
//! behavior against what the source recorded.

#![forbid(unsafe_code)]

use std::collections::BTreeMap;
use std::fs::{self, File, OpenOptions};
use std::io::Write;
use std::os::unix::fs::{DirBuilderExt, OpenOptionsExt};
use std::path::Path;
use std::sync::{LazyLock, Mutex, MutexGuard};
use std::time::{SystemTime, UNIX_EPOCH};

/// Port of `stat` (`internal/prof/prof.go:23`): a duration in nanoseconds, like Go's `time.Duration`.
type Duration = i64;

const MICROSECOND: Duration = 1_000;
const MILLISECOND: Duration = 1_000_000;
const SECOND: Duration = 1_000_000_000;

#[derive(Default)]
struct Stat {
    count: u64,
    total: Duration,
    max: Duration,
}

/// The package globals behind `mu` (`internal/prof/prof.go:29-38`).
#[derive(Default)]
struct Globals {
    file: Option<File>,
    path: String,
    stats: BTreeMap<String, Stat>,
    cpu_file: Option<File>,
    cpu_path: String,
}

static STATE: LazyLock<Mutex<Globals>> = LazyLock::new(|| Mutex::new(Globals::default()));

fn lock() -> MutexGuard<'static, Globals> {
    STATE
        .lock()
        .unwrap_or_else(|poisoned| poisoned.into_inner())
}

/// Port of `Init` (`internal/prof/prof.go:42`).
pub fn init() -> Result<String, String> {
    let raw = std::env::var("AGENT_SESH_PROFILE").unwrap_or_default();
    let raw = raw.trim();
    if raw.is_empty() {
        return Ok(String::new());
    }

    let p = if raw == "1" || raw.eq_ignore_ascii_case("true") {
        let state = default_state_dir()?;
        join(&state, "agent-sesh/profile.log")
    } else {
        raw.to_string()
    };

    create_dir_all_mode(&dir_name(&p), 0o755)?;

    let f = OpenOptions::new()
        .create(true)
        .append(true)
        .mode(0o644)
        .open(&p)
        .map_err(|e| go_io_error("open", &p, &e))?;

    {
        let mut g = lock();
        g.file = Some(f.try_clone().map_err(|e| go_io_error("open", &p, &e))?);
        g.path = p.clone();
        g.stats = BTreeMap::new();
    }

    let _ = write_line(
        &f,
        &format!(
            "agent-sesh profile started {} pid={}\n",
            now_rfc3339(),
            std::process::id()
        ),
    );
    Ok(p)
}

/// Port of `Close` (`internal/prof/prof.go:78`).
pub fn close() -> Result<String, String> {
    let mut g = lock();
    if g.file.is_none() {
        return Ok(String::new());
    }

    write_summary_locked(&mut g);
    if let Some(f) = &g.file {
        let _ = write_line(
            f,
            &format!(
                "agent-sesh profile ended {} pid={}\n",
                now_rfc3339(),
                std::process::id()
            ),
        );
    }
    let p = std::mem::take(&mut g.path);
    if let Some(f) = g.file.take() {
        drop(f); // Go checks the close error but ignores it; dropping closes the fd.
    }
    g.stats = BTreeMap::new();
    Ok(p)
}

/// Port of `Note` (`internal/prof/prof.go:112`).
pub fn note(name: String, detail: String) -> Result<(), String> {
    write(&name, &detail);
    Ok(())
}

/// Port of `StartCPUProfile` (`internal/prof/prof.go:121`).
///
/// The Go version hands the file to `runtime/pprof`; this port keeps the same file
/// lifecycle and error surface (an empty profile file when no sampler is attached).
pub fn start_cpu_profile() -> Result<String, String> {
    let raw = std::env::var("AGENT_SESH_CPUPROFILE").unwrap_or_default();
    let raw = raw.trim();
    if raw.is_empty() {
        return Ok(String::new());
    }

    let p = if raw == "1" || raw.eq_ignore_ascii_case("true") {
        let state = default_state_dir()?;
        join(&state, "agent-sesh/cpu.prof")
    } else {
        raw.to_string()
    };

    create_dir_all_mode(&dir_name(&p), 0o755)?;

    let f = File::create(&p).map_err(|e| go_io_error("open", &p, &e))?;
    {
        let mut g = lock();
        if g.cpu_file.is_some() {
            drop(f);
            return Err("cpu profiling already in progress".to_string());
        }
        g.cpu_file = Some(f);
        g.cpu_path = p.clone();
    }
    Ok(p)
}

/// Port of `StopCPUProfile` (`internal/prof/prof.go:157`).
pub fn stop_cpu_profile() -> Result<String, String> {
    let mut g = lock();
    if g.cpu_file.is_none() {
        return Ok(String::new());
    }
    let p = std::mem::take(&mut g.cpu_path);
    if let Some(f) = g.cpu_file.take() {
        drop(f);
    }
    Ok(p)
}

/// Port of `defaultStateDir` (`internal/prof/prof.go:173`).
fn default_state_dir() -> Result<String, String> {
    if let Ok(state) = std::env::var("XDG_STATE_HOME")
        && !state.is_empty()
    {
        return Ok(state);
    }
    match std::env::var("HOME") {
        Ok(home) if !home.is_empty() => Ok(join(&home, ".local/state")),
        _ => Err("$HOME is not defined".to_string()),
    }
}

/// Port of `write` (`internal/prof/prof.go:184`).
fn write(name: &str, detail: &str) {
    let g = lock();
    if let Some(f) = &g.file {
        let _ = write_line(f, &format!("agent-sesh profile: {name} {detail}\n"));
    }
}

fn write_line(f: &File, line: &str) -> std::io::Result<()> {
    let mut f = f.try_clone()?;
    f.write_all(line.as_bytes())
}

/// Port of `writeSummaryLocked` (`internal/prof/prof.go:208`).
fn write_summary_locked(g: &mut Globals) {
    let Some(f) = &g.file else { return };
    if g.stats.is_empty() {
        return;
    }

    let mut out = String::from("agent-sesh profile: summary\n");
    for (name, s) in &g.stats {
        let avg = s.total / s.count.max(1) as Duration;
        out.push_str(&format!(
            "agent-sesh profile: {:<28} count={:>4} total={:>12} avg={:>10} max={:>10}\n",
            name,
            s.count,
            duration_string(round(s.total, MICROSECOND)),
            duration_string(round(avg, MICROSECOND)),
            duration_string(round(s.max, MICROSECOND)),
        ));
    }
    let _ = write_line(f, &out);
}

/// Port of `time.Duration.Round` (`time/duration.go`) for a positive modulus.
fn round(d: Duration, m: Duration) -> Duration {
    if m <= 0 {
        return d;
    }
    let r = d % m;
    if r * 2 >= m {
        if d < 0 { d - m - r } else { d + m - r }
    } else {
        d - r
    }
}

/// Port of `time.Duration.String` (`time/duration.go`).
fn duration_string(d: Duration) -> String {
    let sign = if d < 0 { "-" } else { "" };
    let u = d.unsigned_abs();
    let (scale, unit, width) = if u < MICROSECOND as u64 {
        (1u64, "ns", 0usize)
    } else if u < MILLISECOND as u64 {
        (MICROSECOND as u64, "\u{00b5}s", 3)
    } else if u < SECOND as u64 {
        (MILLISECOND as u64, "ms", 6)
    } else {
        (SECOND as u64, "s", 9)
    };
    if width == 0 {
        return format!("{sign}{u}ns");
    }
    let whole = u / scale;
    match fraction(u % scale, width) {
        Some(frac) => format!("{sign}{whole}.{frac}{unit}"),
        None => format!("{sign}{whole}{unit}"),
    }
}

/// The fractional digits of `v` padded to `width` with trailing zeros trimmed, or `None`
/// when the fraction is zero (Go drops the dot entirely).
fn fraction(v: u64, width: usize) -> Option<String> {
    if v == 0 {
        return None;
    }
    let padded = format!("{v:0>width$}");
    Some(padded.trim_end_matches('0').to_string())
}

/// `time.Now().Format(time.RFC3339)`; UTC is what Go prints when the zone is UTC.
fn now_rfc3339() -> String {
    let secs = SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .map_or(0, |d| d.as_secs());
    let days = (secs / 86_400) as i64;
    let tod = secs % 86_400;
    let (y, mo, d) = civil_from_days(days);
    format!(
        "{y:04}-{mo:02}-{d:02}T{:02}:{:02}:{:02}Z",
        tod / 3600,
        (tod % 3600) / 60,
        tod % 60
    )
}

/// Howard Hinnant's civil_from_days: UTC days since 1970-01-01 to (year, month, day).
fn civil_from_days(z: i64) -> (i64, u32, u32) {
    let z = z + 719_468;
    let era = if z >= 0 { z } else { z - 146_096 } / 146_097;
    let doe = (z - era * 146_097) as u64;
    let yoe = (doe - doe / 1460 + doe / 36_524 - doe / 146_096) / 365;
    let y = yoe as i64 + era * 400;
    let doy = doe - (365 * yoe + yoe / 4 - yoe / 100);
    let mp = (5 * doy + 2) / 153;
    let d = (doy - (153 * mp + 2) / 5 + 1) as u32;
    let m = if mp < 10 { mp + 3 } else { mp - 9 } as u32;
    (if m <= 2 { y + 1 } else { y }, m, d)
}

/// `filepath.Join` for a constant relative part.
fn join(dir: &str, part: &str) -> String {
    Path::new(dir).join(part).to_string_lossy().into_owned()
}

/// `filepath.Dir` for a path string.
fn dir_name(path: &str) -> String {
    match Path::new(path).parent() {
        Some(p) if !p.as_os_str().is_empty() => p.to_string_lossy().into_owned(),
        _ => ".".to_string(),
    }
}

/// `os.MkdirAll(dir, mode)` with an explicit permission.
fn create_dir_all_mode(dir: &str, mode: u32) -> Result<(), String> {
    fs::DirBuilder::new()
        .recursive(true)
        .mode(mode)
        .create(dir)
        .map_err(|e| go_io_error("mkdir", dir, &e))
}

/// Formats an IO failure the way Go's `os` package does: `op path: reason`.
fn go_io_error(op: &str, path: &str, e: &std::io::Error) -> String {
    let reason = match e.kind() {
        std::io::ErrorKind::NotFound => "no such file or directory",
        std::io::ErrorKind::PermissionDenied => "permission denied",
        std::io::ErrorKind::IsADirectory => "is a directory",
        std::io::ErrorKind::NotADirectory => "not a directory",
        _ => return format!("{op} {path}: {e}"),
    };
    format!("{op} {path}: {reason}")
}
