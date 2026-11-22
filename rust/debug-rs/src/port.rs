//! The Rust port of `internal/debug/`.
//!
//! Go writes each report to an `io.Writer`; the bridge returns the same bytes as a
//! string. Output formats and error strings match Go byte-for-byte. The live tmux
//! and registry reads go through the `tmux-rs` and `registry-rs` ports, mirroring
//! Go's call graph (`registry.Sanitize` with `defaultSanitizeOpts`, `tmux.PaneInfoFor`).

#![forbid(unsafe_code)]

use std::collections::HashSet;

use serde_json::{Value, json};

// The registry and tmux ports are compiled in as modules rather than path deps: their
// `lib.rs` exports the same C entry point, and two of them in one cdylib fail to link.
#[allow(dead_code)]
#[path = "../../registry-rs/src/port.rs"]
mod registry;
#[allow(dead_code)]
#[path = "../../tmux-rs/src/port.rs"]
mod tmux;

/// Port of `Registry` (`internal/debug/debug.go:19`): the registry file as JSON.
pub fn registry() -> Result<String, String> {
    let path = registry::default_path()?;
    let sessions = registry::load(path)?;
    encode_json(&json!({ "version": 1, "sessions": sessions }))
}

/// Live pane ids for `sanitize`'s `PaneExists` pruning; `Null` when tmux is unavailable
/// (then no pane pruning happens, same as the bridge's nil option).
fn live_panes_opts() -> Value {
    match tmux_rs::port::list_pane_i_ds() {
        Ok(ids) => serde_json::json!({ "live_panes": ids }),
        Err(_) => Value::Null,
    }
}

/// Port of `Validate` (`internal/debug/debug.go:36`).
pub fn validate() -> Result<String, String> {
    let path = registry::default_path()?;
    let sessions = registry::load(path.clone())?;
    let loaded = sessions.as_array().map_or(0, Vec::len);
    let pair = registry::sanitize(sessions, live_panes_opts())?;
    let kept = pair_element(&pair, 0);
    let removed = pair_element(&pair, 1);

    let mut out = format!("registry: {path}\n");
    out.push_str(&format!(
        "loaded: {loaded}  kept: {}  pruned: {}\n\n",
        kept.len(),
        removed.len()
    ));
    if !removed.is_empty() {
        out.push_str("pruned:\n");
        let rows: Vec<Vec<String>> = removed
            .iter()
            .map(|row| {
                let session = row.get("Session").cloned().unwrap_or(Value::Null);
                vec![
                    text(&session, "tmux_target"),
                    text(row, "Reason"),
                    short_id(&text(&session, "id")),
                    text(&session, "title"),
                ]
            })
            .collect();
        out.push_str(&render_table(&["TARGET", "REASON", "ID", "TITLE"], &rows));
        out.push('\n');
    }
    if kept.is_empty() {
        return Ok(out);
    }
    out.push_str("kept:\n");
    let rows: Vec<Vec<String>> = kept
        .iter()
        .map(|session| {
            vec![
                text(session, "tmux_target"),
                session_name(session),
                text(session, "agent"),
                text(session, "status"),
                short_text(&prompt_for(session), 48),
            ]
        })
        .collect();
    out.push_str(&render_table(
        &["TARGET", "SESSION", "AGENT", "STATUS", "PROMPT"],
        &rows,
    ));
    Ok(out)
}

/// Port of `Panes` (`internal/debug/debug.go:87`): registry rows joined with live
/// pane info, then the `Discover` report appended.
pub fn panes() -> Result<String, String> {
    let path = registry::default_path()?;
    let sessions = registry::load(path)?;
    let pair = registry::sanitize(sessions, Value::Null)?;
    let kept = pair_element(&pair, 0);

    let rows: Vec<Vec<String>> = kept
        .iter()
        .map(|session| {
            let target = text(session, "tmux_target");
            let info = pane_info(&target);
            vec![
                target,
                session_name(session),
                short_id(&text(session, "id")),
                text(&info, "CurrentCommand"),
                bool_text(&info, "HasPiAgent"),
                short_text(&prompt_for(session), 40),
            ]
        })
        .collect();
    let mut out = render_table(
        &["TARGET", "SESSION", "REGISTRY", "LIVE_CMD", "PI", "PROMPT"],
        &rows,
    );
    out.push_str(&discover()?);
    Ok(out)
}

/// Port of `Pane` (`internal/debug/debug.go:118`): live pane info as JSON.
pub fn pane(target: String) -> Result<String, String> {
    encode_json(&pane_info(&target))
}

/// Port of `Discover` (`internal/debug/debug.go:128`).
pub fn discover() -> Result<String, String> {
    let path = registry::default_path()?;
    let sessions = registry::load(path)?;
    let pair = registry::sanitize(sessions, Value::Null)?;
    let kept = pair_element(&pair, 0);
    let known: HashSet<String> = kept
        .iter()
        .map(|session| text(session, "tmux_target").trim().to_string())
        .collect();

    let mut missing: Vec<Value> = Vec::new();
    for pane_id in tmux::list_pane_i_ds()? {
        let info = pane_info(&pane_id);
        if !bool_field(&info, "HasPiAgent") || bool_field(&info, "IsSubagent") {
            continue;
        }
        if known.contains(info.get("PaneID").and_then(Value::as_str).unwrap_or("")) {
            continue;
        }
        missing.push(info);
    }
    if missing.is_empty() {
        return Ok("discover: no unregistered pi panes\n".to_string());
    }
    let rows: Vec<Vec<String>> = missing
        .iter()
        .map(|info| {
            vec![
                text(info, "PaneID"),
                format!(
                    "{}:{}.{}",
                    text(info, "SessionName"),
                    text(info, "WindowIndex"),
                    text(info, "PaneIndex")
                ),
                text(info, "CurrentCommand"),
                text(info, "CurrentPath"),
            ]
        })
        .collect();
    let mut out = String::from("discover: pi panes missing from registry:\n");
    out.push_str(&render_table(&["TARGET", "SESSION", "CMD", "PATH"], &rows));
    Ok(out)
}

/// Port of `defaultSanitizeOpts` + `PaneInfoFor`: Go ignores the lookup error and
/// encodes the zero-value info, so an error here degrades to the same empty record.
fn pane_info(target: &str) -> Value {
    match tmux::pane_info_for(target.to_string()) {
        Ok(info) => info,
        Err(_) => json!({
            "Target": target.trim(),
            "PaneID": "",
            "SessionName": "",
            "WindowIndex": "",
            "PaneIndex": "",
            "TTY": "",
            "CurrentCommand": "",
            "StartCommand": "",
            "CurrentPath": "",
            "ShellPID": 0,
            "Width": 0,
            "Exists": false,
            "HasPiAgent": false,
            "Agent": "",
            "IsSubagent": false,
        }),
    }
}

/// Port of `sessionName` (`internal/debug/debug.go:71`).
fn session_name(session: &Value) -> String {
    let name = text(session, "tmux_session");
    if !name.trim().is_empty() {
        return name.trim().to_string();
    }
    match tmux::session_name(text(session, "tmux_target")) {
        Ok(name) => name,
        Err(_) => "?".to_string(),
    }
}

/// Port of `promptFor` (`internal/debug/debug.go:63`).
fn prompt_for(session: &Value) -> String {
    let prompt = text(session, "last_prompt");
    if !prompt.trim().is_empty() {
        return prompt;
    }
    text(session, "title")
}

/// Port of `shortID` (`internal/debug/debug.go:151`): Go slices bytes, not runes.
fn short_id(id: &str) -> String {
    let id = id.trim();
    if id.len() <= 8 {
        return id.to_string();
    }
    String::from_utf8_lossy(&id.as_bytes()[..8]).into_owned()
}

/// Port of `shortText` (`internal/debug/debug.go:160`).
fn short_text(text: &str, max: usize) -> String {
    let text = text.trim();
    if max == 0 || text.len() <= max {
        return text.to_string();
    }
    if max <= 3 {
        return String::from_utf8_lossy(&text.as_bytes()[..max]).into_owned();
    }
    format!(
        "{}...",
        String::from_utf8_lossy(&text.as_bytes()[..max - 3])
    )
}

/// Renders a tabwriter table (`minwidth 0, padding 2, pad ' '`): every cell but the
/// last in a row is padded to the column width plus padding; the last cell is raw.
fn render_table(headers: &[&str], rows: &[Vec<String>]) -> String {
    let mut widths: Vec<usize> = headers
        .iter()
        .map(|header| header.chars().count())
        .collect();
    for row in rows {
        for (index, cell) in row.iter().enumerate() {
            if let Some(width) = widths.get_mut(index) {
                *width = (*width).max(cell.chars().count());
            }
        }
    }
    let mut out = String::new();
    let mut lines: Vec<Vec<String>> =
        vec![headers.iter().map(|header| header.to_string()).collect()];
    lines.extend(rows.iter().cloned());
    for line in &lines {
        let last = line.len().saturating_sub(1);
        let mut rendered = String::new();
        for (index, cell) in line.iter().enumerate() {
            if index == last {
                rendered.push_str(cell);
            } else {
                let width = widths.get(index).copied().unwrap_or(0) + 2;
                rendered.push_str(&format!("{:<width$}", cell, width = width));
            }
        }
        out.push_str(&rendered);
        out.push('\n');
    }
    out
}

/// `json.Encoder` with `SetIndent("", "  ")`: pretty JSON plus a trailing newline.
fn encode_json(value: &Value) -> Result<String, String> {
    let mut text = serde_json::to_string_pretty(value).map_err(|e| e.to_string())?;
    text.push('\n');
    Ok(text)
}

/// One element of the `[kept, removed]` pair returned by `registry.Sanitize`.
fn pair_element(pair: &Value, index: usize) -> Vec<Value> {
    pair.get(index)
        .and_then(Value::as_array)
        .cloned()
        .unwrap_or_default()
}

fn text(value: &Value, key: &str) -> String {
    value.get(key).map(value_to_string).unwrap_or_default()
}

fn value_to_string(value: &Value) -> String {
    match value {
        Value::String(text) => text.clone(),
        Value::Number(number) => number.to_string(),
        _ => String::new(),
    }
}

fn bool_field(value: &Value, key: &str) -> bool {
    value.get(key).and_then(Value::as_bool).unwrap_or(false)
}

fn bool_text(value: &Value, key: &str) -> String {
    if bool_field(value, key) {
        "true"
    } else {
        "false"
    }
    .to_string()
}
