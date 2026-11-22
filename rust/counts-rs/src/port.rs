//! The Rust port of `internal/counts/`.
//!
//! Go writes each report to an `io.Writer`; the bridge returns the same bytes as a
//! string. Output formats and error strings match Go byte-for-byte. The live registry
//! and tmux reads go through the `registry-rs` and `tmux-rs` ports, mirroring Go's
//! call graph (`registry.Sanitize` with a pane snapshot, `registry.CountByCategory`).

#![forbid(unsafe_code)]

use serde_json::{Value, json};

/// Port of `snapshotMaxAge` (`internal/counts/counts.go:17`): the pane snapshot TTL.
/// The bridge reads live panes through `tmux-rs` directly, which keeps its own
/// process-local snapshot with the same TTL semantics.
#[allow(dead_code)]
const SNAPSHOT_MAX_AGE_SECS: u64 = 10;

/// Port of `iconAttention` (`internal/counts/counts.go:21`).
const ICON_ATTENTION: &str = "\u{F0377}";

/// Port of `iconActive` (`internal/counts/counts.go:22`).
const ICON_ACTIVE: &str = "\u{F09D1}";

/// Port of `iconIdle` (`internal/counts/counts.go:23`).
const ICON_IDLE: &str = "\u{F04B2}";

/// Port of `DefaultFormat` (`internal/counts/counts.go:27`).
pub const DEFAULT_FORMAT: &str =
    "{{.AttentionIcon}} {{.Attention}} {{.ActiveIcon}} {{.Active}} {{.IdleIcon}} {{.Idle}}";

/// Port of `LoadAndCount` (`internal/counts/counts.go:46`): category totals for live
/// pi sessions, as `{attention, active, idle}` JSON (`registry.CategoryCounts`).
pub fn load_and_count() -> Result<Value, String> {
    let path = registry_rs::port::default_path()?;
    let sessions = registry_rs::port::load(path)?;
    let sanitized = registry_rs::port::sanitize(sessions, live_panes_opts())?;
    registry_rs::port::count_by_category(sanitized.get(0).cloned().unwrap_or(Value::Null))
}

/// Live pane ids for `sanitize`'s `paneSnapshot()` stand-in (`internal/counts/counts.go:61`);
/// `Null` when tmux is unavailable (then no pane pruning happens, same as Go's nil snapshot).
fn live_panes_opts() -> Value {
    match tmux_rs::port::list_pane_i_ds() {
        Ok(ids) => json!({ "live_panes": ids }),
        Err(_) => Value::Null,
    }
}

/// Port of `templateData` (`internal/counts/counts.go:73`): counts and icons for the
/// `--format` template fields.
fn template_data(counts: &Value) -> Value {
    json!({
        "Attention": counts.get("attention").cloned().unwrap_or(json!(0)),
        "Active": counts.get("active").cloned().unwrap_or(json!(0)),
        "Idle": counts.get("idle").cloned().unwrap_or(json!(0)),
        "AttentionIcon": ICON_ATTENTION,
        "ActiveIcon": ICON_ACTIVE,
        "IdleIcon": ICON_IDLE,
    })
}

/// Port of `Write` (`internal/counts/counts.go:85`): format category totals to a string.
/// `opts` carries `{json, format}`; JSON output ends with a newline, template output does
/// not, exactly like Go's `json.Encoder` versus `template.Execute`.
pub fn write(opts: &Value, counts: &Value) -> Result<String, String> {
    if opts.get("json").and_then(Value::as_bool).unwrap_or(false) {
        // Go: json.Encoder with SetEscapeHTML(false) on CategoryCounts.
        return Ok(format!(
            "{{\"attention\":{},\"active\":{},\"idle\":{}}}\n",
            counts.get("attention").cloned().unwrap_or(json!(0)),
            counts.get("active").cloned().unwrap_or(json!(0)),
            counts.get("idle").cloned().unwrap_or(json!(0)),
        ));
    }
    let mut format = opts
        .get("format")
        .and_then(Value::as_str)
        .unwrap_or("")
        .to_string();
    if format.is_empty() {
        format = DEFAULT_FORMAT.to_string();
    }
    render_template(&format, &template_data(counts))
}

/// Port of `Run` (`internal/counts/counts.go:105`): load the registry and format counts.
pub fn run(opts: &Value) -> Result<String, String> {
    let counts = load_and_count()?;
    write(opts, &counts)
}

/// Renders the `text/template` subset the command exercises: literal text plus
/// `{{.Field}}` references (optional inner whitespace) into `data`. Anything else
/// reports an error so the front door falls back to the original instead of guessing.
fn render_template(format: &str, data: &Value) -> Result<String, String> {
    let mut out = String::new();
    let bytes = format.as_bytes();
    let mut rest = format;
    while let Some(at) = rest.find("{{") {
        out.push_str(&rest[..at]);
        let after = &rest[at + 2..];
        let Some(end) = after.find("}}") else {
            return Err(format!(
                "parse format template: template: counts:1: unexpected unclosed action in format"
            ));
        };
        let action = after[..end].trim();
        let value = match action.strip_prefix('.') {
            Some(field) if is_simple_field(field) => data
                .get(field)
                .cloned()
                .ok_or_else(|| format!("parse format template: template: counts:1: can't evaluate field {field} in type counts.TemplateData"))?,
            _ => return Err("rust-anything: unsupported template action".to_string()),
        };
        out.push_str(&match value {
            Value::String(text) => text.clone(),
            other => other.to_string(),
        });
        rest = &after[end + 2..];
    }
    out.push_str(rest);
    let _ = bytes;
    Ok(out)
}

fn is_simple_field(action: &str) -> bool {
    !action.is_empty()
        && action.chars().all(|c| c.is_ascii_alphanumeric())
        && action.chars().next().is_some_and(|c| c.is_ascii_alphabetic())
}
