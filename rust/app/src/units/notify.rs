//! Glue from the front door to `notify-rs`: parse the arguments, call `notify_rs::port`,
//! print what the original prints and return its exit code. A case the port does not
//! cover yet goes back with `crate::original::run(&argv[1..])`, or
//! `crate::original::run_with_stdin(&argv[1..], &input)` once stdin was read.

use std::process::ExitCode;

use clap::ArgMatches;
use serde_json::{Value, json};

/// Go `flag` semantics: `-flag v`, `--flag v`, `--flag=v`. An unknown flag returns
/// `None` so the case falls back to the original instead of guessing.
fn parse_flags(argv: &[String]) -> Option<Vec<(String, String)>> {
    let mut out = Vec::new();
    let mut i = 2; // argv[0] is the program name, argv[1] is "notify"
    while i < argv.len() {
        let arg = &argv[i];
        if !arg.starts_with('-') || arg == "--" {
            break;
        }
        let (name, inline) = match arg.split_once('=') {
            Some((name, value)) => (name.to_string(), Some(value.to_string())),
            None => (arg.clone(), None),
        };
        let known = matches!(
            name.as_str(),
            "--backend" | "-backend" | "--tty" | "-tty" | "--command" | "-command"
                | "--event" | "-event" | "--session" | "-session" | "--title" | "-title"
                | "--body" | "-body"
        );
        if !known {
            return None;
        }
        let value = match inline {
            Some(value) => value,
            None => {
                i += 1;
                match argv.get(i) {
                    Some(value) => value.clone(),
                    None => return None,
                }
            }
        };
        out.push((name.trim_start_matches('-').to_string(), value));
        i += 1;
    }
    Some(out)
}

fn flag<'a>(flags: &'a [(String, String)], name: &str) -> &'a str {
    flags
        .iter()
        .find(|(key, _)| key == name)
        .map(|(_, value)| value.as_str())
        .unwrap_or("")
}

fn first_non_empty(values: &[&str]) -> String {
    values
        .iter()
        .map(|value| value.trim())
        .find(|value| !value.is_empty())
        .unwrap_or("")
        .to_string()
}

/// Go `%q`-style quoting for the session-key error (escape non-printables the
/// same way Go's `strconv.Quote` does; printable non-ASCII stays literal).
fn go_quote(s: &str) -> String {
    let mut out = String::from("\"");
    for c in s.chars() {
        match c {
            '"' => out.push_str("\\\""),
            '\\' => out.push_str("\\\\"),
            '\t' => out.push_str("\\t"),
            '\n' => out.push_str("\\n"),
            '\r' => out.push_str("\\r"),
            c if c < '\u{80}' && c.is_control() => {
                out.push_str(&format!("\\x{:02x}", c as u32));
            }
            // Non-ASCII whitespace (except U+0020) and format characters are
            // not printable in Go's unicode.IsPrint, so %q escapes them.
            '\u{00a0}' | '\u{1680}' | '\u{2000}'..='\u{200f}' | '\u{2028}' | '\u{2029}'
            | '\u{202f}' | '\u{205f}' | '\u{2060}'..='\u{2064}' | '\u{3000}' | '\u{feff}' => {
                out.push_str(&format!("\\u{:04x}", c as u32));
            }
            c => out.push(c),
        }
    }
    out.push('"');
    out
}

pub fn run(_command: &str, _matches: &ArgMatches) -> ExitCode {
    let argv: Vec<String> = std::env::args().collect();
    let Some(flags) = parse_flags(&argv) else {
        let os_argv: Vec<std::ffi::OsString> = std::env::args_os().collect();
        return crate::original::run(&os_argv[1..]);
    };
    let cfg = match config_rs::port::load() {
        Ok(cfg) => cfg,
        Err(e) => {
            eprintln!("{e}");
            return ExitCode::FAILURE;
        }
    };
    let notify_cfg = cfg.get("notify").cloned().unwrap_or(Value::Null);
    let get = |key: &str| -> String {
        notify_cfg
            .get(key)
            .and_then(Value::as_str)
            .unwrap_or("")
            .to_string()
    };
    let mut e = json!({
        "event": flag(&flags, "event"),
        "title": flag(&flags, "title"),
        "body": flag(&flags, "body"),
    });
    let key = first_non_empty(&[flag(&flags, "session")]);
    if !key.is_empty() {
        let session = match lookup(&key) {
            Ok(Some(session)) => session,
            Ok(None) => {
                eprintln!("session {} not found", go_quote(&key));
                return ExitCode::FAILURE;
            }
            Err(e) => {
                eprintln!("{e}");
                return ExitCode::FAILURE;
            }
        };
        e["session_id"] = json!(session.get("id").and_then(Value::as_str).unwrap_or(""));
        e["target"] = json!(session.get("tmux_target").and_then(Value::as_str).unwrap_or(""));
        e["agent"] = json!(session.get("agent").and_then(Value::as_str).unwrap_or(""));
        e["project"] = json!(session.get("cwd").and_then(Value::as_str).unwrap_or(""));
        e["branch"] = json!(session.get("branch").and_then(Value::as_str).unwrap_or(""));
        if e["event"].as_str().is_none_or(|s: &str| s.is_empty()) {
            match registry_rs::port::display_status_json(session.clone()) {
                Ok(status) => e["event"] = status,
                Err(err) => {
                    eprintln!("{err}");
                    return ExitCode::FAILURE;
                }
            }
        }
    }
    let opts = json!({
        "backend": first_non_empty(&[flag(&flags, "backend"), get("backend").as_str()]),
        "tty": first_non_empty(&[flag(&flags, "tty"), get("tty").as_str()]),
        "command": first_non_empty(&[flag(&flags, "command"), get("command").as_str()]),
    });
    match notify_rs::port::send(e, opts) {
        Ok(()) => ExitCode::SUCCESS,
        Err(e) => {
            eprintln!("{e}");
            ExitCode::FAILURE
        }
    }
}

/// Port of `picker.Lookup` (`internal/picker/list.go:14`) over `registry-rs`.
fn lookup(key: &str) -> Result<Option<Value>, String> {
    let key = key.trim();
    if key.is_empty() {
        return Err("empty session key".to_string());
    }
    let sessions = registry_rs::port::load(registry_rs::port::default_path()?)?;
    Ok(sessions
        .as_array()
        .into_iter()
        .flatten()
        .find(|session| {
            ["id", "tmux_target", "title", "pi_session_name"]
                .iter()
                .any(|field| session.get(*field).and_then(Value::as_str) == Some(key))
        })
        .cloned())
}
