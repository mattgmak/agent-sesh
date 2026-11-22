//! The Rust port of `internal/notify/`.
//!
//! Mirrors `internal/notify/notify.go`: backend resolution, OSC escaping,
//! rendering, and delivery semantics match the Go source, including error
//! strings. `Options.Stdout` cannot cross the FFI, so the log backend and the
//! osc fallback write to the process stderr, exactly like the Go default.

#![forbid(unsafe_code)]
#![allow(clippy::collapsible_if, clippy::manual_retain)]

use std::process::{Command, Stdio};
use std::sync::mpsc;
use std::time::Duration;

use serde::{Deserialize, Serialize};
use serde_json::json;
use unicode_properties::{GeneralCategory, UnicodeGeneralCategory};

/// Port of `commandTimeout` (`internal/notify/notify.go:19`).
const COMMAND_TIMEOUT: Duration = Duration::from_secs(10);

/// Port of the backend names (`internal/notify/notify.go:22`).
const BACKEND_AUTO: &str = "auto";
const BACKEND_OSC: &str = "osc";
const BACKEND_NOTIFY_SEND: &str = "notify-send";
const BACKEND_OSASCRIPT: &str = "osascript";
const BACKEND_COMMAND: &str = "command";
const BACKEND_LOG: &str = "log";

/// Port of `knownBackends` (`internal/notify/notify.go:31`).
fn is_known_backend(backend: &str) -> bool {
    matches!(
        backend,
        BACKEND_AUTO
            | BACKEND_OSC
            | BACKEND_NOTIFY_SEND
            | BACKEND_OSASCRIPT
            | BACKEND_COMMAND
            | BACKEND_LOG
    )
}

/// Port of `Event` (`internal/notify/notify.go:44`).
#[derive(Clone, Default, Deserialize, Serialize)]
#[serde(default)]
struct Event {
    event: String,
    session_id: String,
    target: String,
    agent: String,
    project: String,
    branch: String,
    title: String,
    subtitle: String,
    body: String,
}

/// Port of `Options` (`internal/notify/notify.go:58`). `Stdout` is an
/// `io.Writer` that never crosses the bridge; the port uses stderr.
#[derive(Clone, Default, Deserialize)]
#[serde(default)]
struct Options {
    #[serde(rename = "Backend", alias = "backend")]
    backend: String,
    #[serde(rename = "TTY", alias = "tty")]
    tty: String,
    #[serde(rename = "Command", alias = "command")]
    command: String,
}

/// Port of `Env` (`internal/notify/notify.go:71`).
#[derive(Clone, Default, Deserialize)]
#[serde(default)]
#[allow(non_snake_case)]
struct Env {
    GOOS: String,
    OSAScript: bool,
    NotifySend: bool,
    DBUSSet: bool,
    DisplaySet: bool,
    InTmux: bool,
    HasTTY: bool,
    HasCommand: bool,
    HasStdoutOK: bool,
}

/// Whether Go's `unicode.IsPrint` accepts `c` (L, M, N, P, S categories).
fn go_is_print(c: char) -> bool {
    matches!(
        c.general_category(),
        GeneralCategory::UppercaseLetter
            | GeneralCategory::LowercaseLetter
            | GeneralCategory::TitlecaseLetter
            | GeneralCategory::ModifierLetter
            | GeneralCategory::OtherLetter
            | GeneralCategory::NonspacingMark
            | GeneralCategory::SpacingMark
            | GeneralCategory::EnclosingMark
            | GeneralCategory::DecimalNumber
            | GeneralCategory::LetterNumber
            | GeneralCategory::OtherNumber
            | GeneralCategory::ConnectorPunctuation
            | GeneralCategory::DashPunctuation
            | GeneralCategory::OpenPunctuation
            | GeneralCategory::ClosePunctuation
            | GeneralCategory::InitialPunctuation
            | GeneralCategory::FinalPunctuation
            | GeneralCategory::OtherPunctuation
            | GeneralCategory::MathSymbol
            | GeneralCategory::CurrencySymbol
            | GeneralCategory::ModifierSymbol
            | GeneralCategory::OtherSymbol
    )
}

/// The `%q` escape of one rune, without the surrounding quotes.
fn escape_rune(c: char, out: &mut String) {
    match c {
        '"' => out.push_str("\\\""),
        '\\' => out.push_str("\\\\"),
        '\0' => out.push_str("\\x00"),
        '\x07' => out.push_str("\\a"),
        '\x08' => out.push_str("\\b"),
        '\x0c' => out.push_str("\\f"),
        '\n' => out.push_str("\\n"),
        '\r' => out.push_str("\\r"),
        '\t' => out.push_str("\\t"),
        '\x0b' => out.push_str("\\v"),
        c if (c as u32) < 0x20 || c == '\u{7f}' => {
            out.push_str(&format!("\\x{:02X}", c as u32));
        }
        c if c.is_ascii() => out.push(c),
        c if go_is_print(c) => out.push(c),
        c if (c as u32) <= 0xFFFF => out.push_str(&format!("\\u{:04x}", c as u32)),
        c => out.push_str(&format!("\\U{:08x}", c as u32)),
    }
}

/// Port of Go's `%q` for the strings this package quotes.
fn go_quote(s: &str) -> String {
    let mut out = String::with_capacity(s.len() + 2);
    out.push('"');
    for c in s.chars() {
        escape_rune(c, &mut out);
    }
    out.push('"');
    out
}

/// Go's `%q` for a single rune, in single quotes (the JSON scanner style).
fn go_quote_char(c: char) -> String {
    let mut out = String::from('\'');
    escape_rune(c, &mut out);
    out.push('\'');
    out
}

/// Port of `run` (`internal/notify/notify.go:86`): execute one command with a
/// bounded lifetime and return its combined output plus the exec error text.
fn run(name: &str, args: &[String], env: &[String]) -> (String, Option<String>) {
    let owned_name = name.to_string();
    let owned_args: Vec<String> = args.to_vec();
    let owned_env: Vec<String> = env.to_vec();
    let (tx, rx) = mpsc::channel();
    let spawned = std::thread::spawn(move || {
        let mut cmd = Command::new(&owned_name);
        cmd.args(&owned_args)
            .stdin(Stdio::null())
            .stdout(Stdio::piped())
            .stderr(Stdio::piped());
        if !owned_env.is_empty() {
            cmd.envs(owned_env.iter().map(|pair| {
                let (k, v) = pair.split_once('=').unwrap_or((pair.as_str(), ""));
                (k, v)
            }));
        }
        let result = match cmd.output() {
            Ok(output) => {
                let mut out = String::from_utf8_lossy(&output.stdout).into_owned();
                let err_text = if output.status.success() {
                    None
                } else {
                    out.push_str(&String::from_utf8_lossy(&output.stderr));
                    Some(format!(
                        "exit status {}",
                        output.status.code().unwrap_or(-1)
                    ))
                };
                (out, err_text)
            }
            Err(e) if e.kind() == std::io::ErrorKind::NotFound => (
                String::new(),
                Some(format!(
                    "exec: {}: executable file not found in $PATH",
                    go_quote(&owned_name)
                )),
            ),
            Err(e) => (String::new(), Some(e.to_string())),
        };
        let _ = tx.send(result);
    });
    match rx.recv_timeout(COMMAND_TIMEOUT) {
        Ok(result) => {
            let _ = spawned.join();
            result
        }
        Err(_) => (String::new(), Some("signal: killed".to_string())),
    }
}

/// Port of `exists` (`internal/notify/notify.go:98`).
fn exists(path: &str) -> bool {
    std::fs::metadata(path).is_ok_and(|m| !m.is_dir())
}

/// Port of `lookPath` (`internal/notify/notify.go:104`): Go's `exec.LookPath`
/// semantics for the plain names this package looks up.
fn look_path(name: &str) -> Result<String, String> {
    let not_found = || {
        format!(
            "exec: {}: executable file not found in $PATH",
            go_quote(name)
        )
    };
    if name.contains('/') {
        return if is_executable_file(name) {
            Ok(name.to_string())
        } else {
            Err(not_found())
        };
    }
    let Ok(path) = std::env::var("PATH") else {
        return Err(not_found());
    };
    for dir in path.split(':') {
        if dir.is_empty() {
            continue;
        }
        let candidate = format!("{}/{}", dir, name);
        if is_executable_file(&candidate) {
            return Ok(candidate);
        }
    }
    Err(not_found())
}

/// Whether `path` names an executable file (Go's `unix` access check).
fn is_executable_file(path: &str) -> bool {
    #[cfg(unix)]
    {
        use std::os::unix::fs::PermissionsExt;
        std::fs::metadata(path)
            .map(|m| m.is_file() && m.permissions().mode() & 0o111 != 0)
            .unwrap_or(false)
    }
    #[cfg(not(unix))]
    {
        std::fs::metadata(path).is_ok_and(|m| m.is_file())
    }
}

/// Shared body of `Resolve` (`internal/notify/notify.go:108`).
fn resolve_backend(explicit: &str, env: &Env) -> Result<String, String> {
    let mut backend = explicit.trim().to_lowercase();
    if backend.is_empty() {
        backend = BACKEND_AUTO.to_string();
    }
    if !is_known_backend(&backend) {
        return Err(format!("unknown notify backend {}", go_quote(explicit)));
    }
    if backend != BACKEND_AUTO {
        if backend == BACKEND_COMMAND && !env.HasCommand {
            return Err("command backend needs a command".to_string());
        }
        return Ok(backend);
    }
    if env.GOOS == "darwin" && env.OSAScript {
        return Ok(BACKEND_OSASCRIPT.to_string());
    }
    if env.NotifySend && (env.DBUSSet || env.DisplaySet) {
        return Ok(BACKEND_NOTIFY_SEND.to_string());
    }
    if env.InTmux && env.HasTTY {
        return Ok(BACKEND_OSC.to_string());
    }
    Ok(BACKEND_LOG.to_string())
}

/// Port of `escapeOSC` (`internal/notify/notify.go:138`).
fn escape_osc(s: &str) -> String {
    let mut s = s.replace('\u{0007}', " ");
    s = s.replace('\u{001b}', " ");
    s = s.replace(';', ",");
    let chars: Vec<char> = s.chars().collect();
    if chars.len() > 500 {
        return chars.into_iter().take(500).collect();
    }
    s
}

/// Port of `Render` (`internal/notify/notify.go:152`): the shared title/body
/// computation used by `Send` as well.
fn render_pair(e: &Event) -> (String, String) {
    let title_src = e.title.trim();
    let body = e.body.trim().to_string();
    let subtitle = e.subtitle.trim();
    let title = if title_src.is_empty() {
        if !e.agent.is_empty() && !e.session_id.is_empty() {
            format!("{} {}", e.agent, short_id(&e.session_id))
        } else if !e.agent.is_empty() {
            e.agent.clone()
        } else if !e.session_id.is_empty() {
            short_id(&e.session_id)
        } else {
            "agent-sesh".to_string()
        }
    } else {
        title_src.to_string()
    };
    let mut body = body;
    if !e.event.is_empty() {
        body = format!("{body} {}", e.event).trim().to_string();
    }
    let body = if body.is_empty() {
        if !subtitle.is_empty() {
            subtitle.to_string()
        } else if !e.branch.is_empty() {
            e.branch.clone()
        } else if !e.project.is_empty() {
            e.project.clone()
        } else {
            "no detail".to_string()
        }
    } else {
        body
    };
    (title, body)
}

/// Port of `shortID` (`internal/notify/notify.go:190`): Go slices bytes, so a
/// multi-byte rune can be cut; the lossy conversion mirrors Go's JSON output.
fn short_id(id: &str) -> String {
    let id = id.trim();
    if id.len() <= 8 {
        return id.to_string();
    }
    String::from_utf8_lossy(&id.as_bytes()[..8]).into_owned()
}

/// Port of `eventEnv` (`internal/notify/notify.go:199`).
fn event_env(e: &Event, title: &str, body: &str) -> Vec<String> {
    vec![
        format!("AGENT_SESH_EVENT={}", e.event),
        format!("AGENT_SESH_SESSION_ID={}", e.session_id),
        format!("AGENT_SESH_TARGET={}", e.target),
        format!("AGENT_SESH_AGENT={}", e.agent),
        format!("AGENT_SESH_PROJECT={}", e.project),
        format!("AGENT_SESH_BRANCH={}", e.branch),
        format!("AGENT_SESH_TITLE={title}"),
        format!("AGENT_SESH_SUBTITLE={}", e.subtitle),
        format!("AGENT_SESH_BODY={body}"),
    ]
}

/// Port of `writeLog` (`internal/notify/notify.go:239`): `opts.Stdout` never
/// crosses the FFI, so this is the Go default of `os.Stderr`.
fn write_log(e: &Event, title: &str, body: &str) -> Result<(), String> {
    use std::io::Write;
    let mut line = format!("notify: {title} | {body}");
    if !e.target.is_empty() {
        line.push_str(&format!(" ({})", e.target));
    }
    let stdout = std::io::stdout();
    stdout
        .lock()
        .write_all(format!("{line}\n").as_bytes())
        .map_err(|e| e.to_string())
}

/// Port of `sendOSC` (`internal/notify/notify.go:251`).
fn send_osc(e: &Event, opts: &Options, title: &str, body: &str) -> Result<(), String> {
    let mut tty = opts.tty.trim().to_string();
    if tty.is_empty() {
        if let Ok(found) = tmux_client_tty(&e.target) {
            tty = found;
        }
    }
    let seq = osc_sequence(title.to_string(), body.to_string())?;
    if !tty.is_empty() && write_tty(&tty, &seq).is_ok() {
        return Ok(());
    }
    write_log(e, title, body)
}

/// Port of `writeTTY` (`internal/notify/notify.go:266`).
fn write_tty(tty: &str, s: &str) -> Result<(), String> {
    use std::fs::OpenOptions;
    use std::io::Write;
    let mut file = OpenOptions::new()
        .write(true)
        .open(tty)
        .map_err(|e| e.to_string())?;
    file.write_all(s.as_bytes()).map_err(|e| e.to_string())?;
    Ok(())
}

/// Port of `execOutput` (`internal/tmux/exec.go:35`), vendored privately:
/// `ClientTTY` needs it and the shared `run` returns combined output. Both
/// cdylibs export `rust_anything_call`, so notify cannot link tmux-rs.
fn tmux_exec(argv: &[&str]) -> Result<String, String> {
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
    rx.recv_timeout(COMMAND_TIMEOUT)
        .map_err(|_| "tmux.list-clients: context deadline exceeded")?
}

/// Port of `ClientTTYs` (`internal/tmux/tmux.go:79`), vendored privately.
fn tmux_client_ttys() -> Result<Vec<String>, String> {
    let out = tmux_exec(&["tmux", "list-clients", "-F", "#{client_tty}"])?;
    Ok(out
        .lines()
        .map(str::trim)
        .filter(|line| !line.is_empty())
        .map(str::to_string)
        .collect())
}

/// Port of `ClientTTY` (`internal/tmux/tmux.go:95`), vendored privately.
fn tmux_client_tty(target: &str) -> Result<String, String> {
    let target = target.trim();
    if !target.is_empty() {
        if let Ok(out) = tmux_exec(&[
            "tmux",
            "display-message",
            "-p",
            "-t",
            target,
            "#{client_tty}",
        ]) {
            let tty = out.trim();
            if !tty.is_empty() {
                return Ok(tty.to_string());
            }
        }
    }
    let ttys = tmux_client_ttys()?;
    ttys.first()
        .cloned()
        .ok_or_else(|| "no tmux client attached".to_string())
}

/// Port of `envFor` (`internal/notify/notify.go:278`).
fn env_for(opts: &Options) -> Env {
    let notify_send = look_path("notify-send").is_ok();
    let getenv = |key: &str| std::env::var(key).unwrap_or_default();
    let mut env = Env {
        GOOS: go_os().to_string(),
        NotifySend: notify_send,
        DBUSSet: !getenv("DBUS_SESSION_BUS_ADDRESS").is_empty(),
        DisplaySet: !getenv("DISPLAY").is_empty(),
        InTmux: !getenv("TMUX").is_empty(),
        HasCommand: !opts.command.trim().is_empty(),
        HasTTY: !opts.tty.trim().is_empty(),
        ..Default::default()
    };
    if exists("/usr/bin/osascript") {
        env.OSAScript = true;
    }
    if !env.InTmux || env.HasTTY {
        return env;
    }
    if tmux_client_tty("").is_ok() {
        env.HasTTY = true;
    }
    env
}

/// Go's `runtime.GOOS` name for the build target.
fn go_os() -> &'static str {
    match std::env::consts::OS {
        "macos" => "darwin",
        other => other,
    }
}

/// Port of `Resolve` (`internal/notify/notify.go:108`).
pub fn resolve(explicit: String, env: serde_json::Value) -> Result<String, String> {
    let env: Env = serde_json::from_value(env).map_err(|e| format!("argument 1: {e}"))?;
    resolve_backend(&explicit, &env)
}

/// Port of `OSCSequence` (`internal/notify/notify.go:147`).
pub fn osc_sequence(title: String, body: String) -> Result<String, String> {
    Ok(format!(
        "\u{001b}]777;notify;{};{}\u{0007}",
        escape_osc(&title),
        escape_osc(&body)
    ))
}

/// Port of `Render` (`internal/notify/notify.go:152`); returns `[title, body]`.
pub fn render(e: serde_json::Value) -> Result<serde_json::Value, String> {
    let e: Event = serde_json::from_value(e).map_err(|e| format!("argument 0: {e}"))?;
    let (title, body) = render_pair(&e);
    Ok(json!([title, body]))
}

/// Port of `Send` (`internal/notify/notify.go:210`).
pub fn send(e: serde_json::Value, opts: serde_json::Value) -> Result<(), String> {
    let e: Event = serde_json::from_value(e).map_err(|e| format!("argument 0: {e}"))?;
    let opts: Options = serde_json::from_value(opts).map_err(|e| format!("argument 1: {e}"))?;
    let env = env_for(&opts);
    let backend = resolve_backend(&opts.backend, &env)?;
    let (title, body) = render_pair(&e);
    match backend.as_str() {
        BACKEND_LOG => write_log(&e, &title, &body),
        BACKEND_OSC => send_osc(&e, &opts, &title, &body),
        BACKEND_COMMAND => {
            let (out, err) = run(
                "sh",
                &["-c".to_string(), opts.command.clone()],
                &event_env(&e, &title, &body),
            );
            if let Some(err) = err {
                return Err(format!("notify command: {err}: {}", out.trim()));
            }
            Ok(())
        }
        BACKEND_NOTIFY_SEND => {
            let bin = look_path("notify-send")?;
            if let Some(err) = run(&bin, &[title.clone(), body.clone()], &[]).1 {
                return Err(format!("notify-send: {err}"));
            }
            Ok(())
        }
        BACKEND_OSASCRIPT => {
            let bin = look_path("osascript")?;
            let script = format!(
                "display notification {} with title {}",
                go_quote(&body),
                go_quote(&title)
            );
            if let Some(err) = run(&bin, &["-e".to_string(), script], &[]).1 {
                return Err(format!("osascript: {err}"));
            }
            Ok(())
        }
        backend => Err(format!("unhandled backend {}", go_quote(backend))),
    }
}

/// Standard-alphabet base64 decode (Go's `json.Marshal` encoding of `[]byte`).
fn base64_decode(s: &str) -> Option<Vec<u8>> {
    fn val(c: u8) -> Option<u32> {
        match c {
            b'A'..=b'Z' => Some(u32::from(c - b'A')),
            b'a'..=b'z' => Some(u32::from(c - b'a') + 26),
            b'0'..=b'9' => Some(u32::from(c - b'0') + 52),
            b'+' => Some(62),
            b'/' => Some(63),
            _ => None,
        }
    }
    let bytes: Vec<u8> = s
        .bytes()
        .filter(|b| *b != b'=' && !b.is_ascii_whitespace())
        .collect();
    let mut out = Vec::with_capacity(bytes.len() * 3 / 4);
    for chunk in bytes.chunks(4) {
        if chunk.len() < 2 {
            break;
        }
        let mut accum: u32 = 0;
        let mut n = 0usize;
        for &b in chunk {
            accum = (accum << 6) | val(b)?;
            n += 1;
        }
        let total = n * 6;
        for i in 0..total / 8 {
            out.push((accum >> (total - 8 * (i + 1))) as u8);
        }
    }
    Some(out)
}

/// Go's JSON type name for a parsed value, as used in `json.Unmarshal` errors.
fn go_json_type_name(v: &serde_json::Value) -> &'static str {
    match v {
        serde_json::Value::Null => "null",
        serde_json::Value::Bool(_) => "bool",
        serde_json::Value::Number(_) => "number",
        serde_json::Value::String(_) => "string",
        serde_json::Value::Array(_) => "array",
        serde_json::Value::Object(_) => "object",
    }
}

/// The rune Go's JSON scanner would report for the byte at `i` (invalid UTF-8
/// bytes are treated as Latin-1 runes, like Go does).
fn byte_at_as_rune(raw: &[u8], i: usize) -> char {
    let b = raw[i];
    if b < 0x80 {
        return char::from(b);
    }
    std::str::from_utf8(&raw[i..])
        .ok()
        .and_then(|s| s.chars().next())
        .unwrap_or_else(|| char::from(b))
}

/// Port of `decode notify event` errors: reproduce Go's scanner messages.
fn decode_event(raw: &[u8]) -> Result<Event, String> {
    let go_err = |msg: &str| format!("decode notify event: {msg}");
    let start = raw
        .iter()
        .position(|b| !matches!(b, b' ' | b'\t' | b'\n' | b'\r'));
    let Some(i) = start else {
        return Err(go_err("unexpected end of JSON input"));
    };
    let first = raw[i];
    let valid_start = matches!(
        first,
        b'{' | b'[' | b'"' | b'-' | b'0'..=b'9' | b't' | b'f' | b'n'
    );
    if !valid_start {
        return Err(go_err(&format!(
            "invalid character {} looking for beginning of value",
            go_quote_char(byte_at_as_rune(raw, i))
        )));
    }
    let value: serde_json::Value = serde_json::from_slice(raw)
        .map_err(|e| go_err(&map_serde_json_error(raw, &e.to_string())))?;
    if value.is_null() {
        return Ok(Event::default());
    }
    let obj = match &value {
        serde_json::Value::Object(obj) => obj,
        other => {
            return Err(go_err(&format!(
                "json: cannot unmarshal {} into Go value of type notify.Event",
                go_json_type_name(other)
            )));
        }
    };
    let mut e = Event::default();
    for (key, field) in [
        ("event", &mut e.event),
        ("session_id", &mut e.session_id),
        ("target", &mut e.target),
        ("agent", &mut e.agent),
        ("project", &mut e.project),
        ("branch", &mut e.branch),
        ("title", &mut e.title),
        ("subtitle", &mut e.subtitle),
        ("body", &mut e.body),
    ] {
        if let Some(v) = obj.get(key) {
            match v.as_str() {
                Some(s) => *field = s.to_string(),
                None if v.is_null() => {}
                None => {
                    return Err(go_err(&format!(
                        "json: cannot unmarshal {} into Go struct field Event.{key} of type string",
                        go_json_type_name(v)
                    )));
                }
            }
        }
    }
    Ok(e)
}

/// Map a serde_json error message onto Go's scanner wording.
fn map_serde_json_error(raw: &[u8], msg: &str) -> String {
    let col_byte = |msg: &str| -> Option<usize> {
        let at = msg.rfind("line 1 column ")?;
        let rest = msg[at + "line 1 column ".len()..].trim_end();
        let col: usize = rest.split_whitespace().next()?.parse().ok()?;
        Some(col.saturating_sub(1))
    };
    if msg.starts_with("EOF while parsing") || msg.starts_with("EOF while parsing a value") {
        return "unexpected end of JSON input".to_string();
    }
    if let Some(at) = col_byte(msg) {
        let rune = byte_at_as_rune(raw, at.min(raw.len() - 1));
        let quoted = go_quote_char(rune);
        if msg.starts_with("expected value") || msg.starts_with("expected ident") {
            return format!("invalid character {quoted} looking for beginning of value");
        }
        if msg.starts_with("expected `:`") {
            return format!("invalid character {quoted} after object key");
        }
        if msg.starts_with("expected `,` or `}`") {
            return format!("invalid character {quoted} after object key:value pair");
        }
        if msg.starts_with("expected `,` or `]`") {
            return format!("invalid character {quoted} after array element");
        }
        if msg.starts_with("trailing characters") {
            return format!("invalid character {quoted} after top-level value");
        }
    }
    msg.to_string()
}

/// Port of `SendEventJSON` (`internal/notify/notify.go:253`).
pub fn send_event_json(raw: serde_json::Value, opts: serde_json::Value) -> Result<(), String> {
    let bytes: Vec<u8> = match &raw {
        serde_json::Value::String(s) => {
            base64_decode(s).ok_or_else(|| "argument 0: invalid base64".to_string())?
        }
        serde_json::Value::Array(items) => items
            .iter()
            .map(|v| {
                v.as_i64()
                    .filter(|n| (0..=255).contains(n))
                    .map(|n| n as u8)
                    .ok_or_else(|| format!("argument 0: invalid byte {v}"))
            })
            .collect::<Result<Vec<u8>, String>>()?,
        serde_json::Value::Null => Vec::new(),
        other => {
            return Err(format!(
                "argument 0: invalid type: {}",
                go_json_type_name(other)
            ));
        }
    };
    let e = decode_event(&bytes)?;
    send(serde_json::to_value(&e).map_err(|e| e.to_string())?, opts)
}
