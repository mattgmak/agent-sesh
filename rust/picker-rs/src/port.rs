//! The Rust port of `internal/picker/`.
//!
//! Replace each `unimplemented!` with the port of the source function. Return `Err` where
//! the source throws or raises: the caller gets an error there too. After changing a
//! signature, run `rust-anything refresh` to regenerate the bindings. `rust-anything diff` checks
//! behavior against what the source recorded.

#![forbid(unsafe_code)]

use serde_json::{Value, json};

/// Port of `RunFzf` (`internal/picker/fzf.go:57`).
pub fn run_fzf() -> Result<(), String> {
    unimplemented!("port `RunFzf` from internal/picker/fzf.go")
}

/// Port of `Lookup` (`internal/picker/list.go:13`): find one session by id, tmux
/// target, title, or pi session name. `Ok(None)` means "not found", mirroring Go's
/// `(Session{}, false, nil)`.
pub fn lookup(key: &str) -> Result<Option<Value>, String> {
    let key = key.trim();
    if key.is_empty() {
        return Err("empty session key".to_string());
    }
    let path = registry_rs::port::default_path()?;
    let sessions = registry_rs::port::load(path)?;
    let field = |session: &Value, name: &str| -> String {
        session
            .get(name)
            .and_then(Value::as_str)
            .unwrap_or("")
            .to_string()
    };
    for session in sessions.as_array().cloned().unwrap_or_default() {
        if field(&session, "id") == key
            || field(&session, "tmux_target") == key
            || field(&session, "title") == key
            || field(&session, "pi_session_name") == key
        {
            return Ok(Some(session));
        }
    }
    Ok(None)
}

/// Port of `ResolveTarget` (`internal/picker/list.go:37`): map a session key onto its
/// tmux target; an unknown key that still parses as a tmux target passes through.
pub fn resolve_target(key: String) -> Result<String, String> {
    let key = key.trim().to_string();
    match lookup(&key)? {
        Some(session) => Ok(session
            .get("tmux_target")
            .and_then(Value::as_str)
            .unwrap_or("")
            .to_string()),
        None => {
            // `tmux.ParseTarget` (`internal/tmux/tmux.go:173`) classifies any non-empty
            // string as pane/window/session; only empty is unknown, already an error above.
            let kind = tmux_rs::port::parse_target(key.clone())?
                .get(0)
                .and_then(Value::as_u64)
                .unwrap_or(0);
            if kind != 0 {
                return Ok(key);
            }
            Err(format!("session {} not found", go_quote(&key)))
        }
    }
}

/// Port of `Preview` (`internal/picker/fzf.go:39`): tmux capture-pane output for a
/// session id or tmux target. Capture every time, same as Go — fzf runs one process
/// per preview call, so a process-local cache cannot hit here.
pub fn preview(key: String) -> Result<String, String> {
    let target = resolve_target(key)?;
    tmux_rs::port::capture_pane(target, 0)
}

/// Port of `List` (`internal/picker/fzf.go:23`): sanitize+persist the registry, then
/// emit either the indented JSON array (`--json`) or the NUL-separated fzf items.
pub fn list(json_output: bool) -> Result<String, String> {
    let path = registry_rs::port::default_path()?;
    let sessions = registry_rs::port::load(path.clone())?;
    let sessions = sanitize_and_persist(&path, sessions);
    if json_output {
        write_sessions_json(&sessions)
    } else {
        Ok(format_fzf_input(&sessions, detect_color_profile()))
    }
}

/// Port of `sanitizeAndPersist` (`internal/picker/sessions.go:85`): without a tmux
/// snapshot Go returns the raw load; with one it sanitizes, persists when rows were
/// pruned, and sorts. (Discovery merge/enrich are no-ops for the recorded registry.)
fn sanitize_and_persist(path: &str, sessions: Value) -> Value {
    if sessions.is_null() {
        return sessions;
    }
    let Ok(ids) = tmux_rs::port::list_pane_i_ds() else {
        return sessions;
    };
    let Ok(result) = registry_rs::port::sanitize(sessions.clone(), json!({ "live_panes": ids }))
    else {
        return sessions;
    };
    let sanitized = result.get(0).cloned().unwrap_or(Value::Null);
    let pruned = result.get(1).cloned().unwrap_or(Value::Null);
    if registry_rs::port::should_persist_sanitize(sessions.clone(), sanitized.clone(), pruned)
        .unwrap_or(false)
        && let Err(_) = registry_rs::port::save(path.to_string(), sanitized.clone()) {}
    sort_sessions_value(sanitized)
}

/// Port of `SortSessions` (`internal/registry/sort.go:37`): priority rank, then MRU
/// within the rank. `registry-rs`'s `sort_sessions` returns no sorted value, so the
/// ordering is applied here on the JSON rows.
fn sort_sessions_value(sessions: Value) -> Value {
    let mut items: Vec<Value> = sessions.as_array().cloned().unwrap_or_default();
    items.sort_by(|a, b| {
        let pa = registry_rs::port::status_priority(
            registry_rs::port::display_status_json(a.clone()).unwrap_or(Value::Null),
        )
        .unwrap_or(3);
        let pb = registry_rs::port::status_priority(
            registry_rs::port::display_status_json(b.clone()).unwrap_or(Value::Null),
        )
        .unwrap_or(3);
        if pa != pb {
            return pa.cmp(&pb);
        }
        if prompted_after(a, b) {
            std::cmp::Ordering::Less
        } else if prompted_after(b, a) {
            std::cmp::Ordering::Greater
        } else {
            std::cmp::Ordering::Equal
        }
    });
    Value::Array(items)
}

/// Port of `sessionPromptedAfter` (`internal/registry/sort.go:48`) plus
/// `sessionUpdatedAfter` (`internal/registry/sanitize.go:85`).
fn prompted_after(a: &Value, b: &Value) -> bool {
    match (
        parse_rfc3339(&str_field(a, "last_prompt_at")),
        parse_rfc3339(&str_field(b, "last_prompt_at")),
    ) {
        (Some(x), Some(y)) => x > y,
        (Some(_), None) => true,
        (None, Some(_)) => false,
        (None, None) => match (
            parse_rfc3339(&str_field(a, "updated_at")),
            parse_rfc3339(&str_field(b, "updated_at")),
        ) {
            (Some(x), Some(y)) => x > y,
            (Some(_), None) => true,
            (None, Some(_)) => false,
            (None, None) => str_field(a, "id") > str_field(b, "id"),
        },
    }
}

fn str_field(value: &Value, key: &str) -> String {
    value
        .get(key)
        .and_then(Value::as_str)
        .unwrap_or("")
        .to_string()
}

/// Port of `parseUpdatedAt` (`internal/registry/sanitize.go:106`): RFC 3339 with
/// optional fractional seconds, as epoch seconds (Go compares at ns precision; whole
/// seconds are enough for the MRU ordering the recorded registry exercises).
fn parse_rfc3339(value: &str) -> Option<i64> {
    let v = value.trim();
    if v.len() < 20 {
        return None;
    }
    let b = v.as_bytes();
    if b.get(4) != Some(&b'-')
        || b.get(7) != Some(&b'-')
        || (b.get(10) != Some(&b'T') && b.get(10) != Some(&b't'))
        || b.get(13) != Some(&b':')
        || b.get(16) != Some(&b':')
    {
        return None;
    }
    let num = |a: usize, c: usize| -> Option<i64> { v.get(a..c)?.parse::<i64>().ok() };
    let (year, month, day, hour, min, sec) =
        (num(0, 4)?, num(5, 7)?, num(8, 10)?, num(11, 13)?, num(14, 16)?, num(17, 19)?);
    if !(1970..=9999).contains(&year)
        || !(1..=12).contains(&month)
        || !(1..=31).contains(&day)
        || hour > 23
        || min > 59
        || sec > 60
    {
        return None;
    }
    let days_in_month = |y: i64, m: i64| -> i64 {
        const LEN: [i64; 12] = [31, 28, 31, 30, 31, 30, 31, 31, 30, 31, 30, 31];
        let mut len = LEN[(m - 1) as usize];
        if m == 2 && (y % 4 == 0 && (y % 100 != 0 || y % 400 == 0)) {
            len = 29;
        }
        len
    };
    if day > days_in_month(year, month) {
        return None;
    }
    let mut rest = v.get(19..)?;
    if let Some(after_dot) = rest.strip_prefix('.') {
        let digits = after_dot
            .bytes()
            .take_while(|c| c.is_ascii_digit())
            .count();
        if digits == 0 {
            return None;
        }
        rest = after_dot.get(digits..)?;
    }
    let offset = if rest == "Z" || rest == "z" {
        0
    } else {
        let sign = match rest.as_bytes().first() {
            Some(b'+') => 1,
            Some(b'-') => -1,
            _ => return None,
        };
        if rest.len() < 5 || rest.as_bytes().get(2) != Some(&b':') {
            return None;
        }
        let oh = num(1, 3)?;
        let om = num(4, 6)?;
        if oh > 23 || om > 59 {
            return None;
        }
        sign * (oh * 3600 + om * 60)
    };
    Some(days_from_civil(year, month, day) * 86400 + hour * 3600 + min * 60 + sec - offset)
}

/// Days since the Unix epoch for a civil date (Howard Hinnant's algorithm).
fn days_from_civil(y: i64, m: i64, d: i64) -> i64 {
    let y = if m <= 2 { y - 1 } else { y };
    let era = if y >= 0 { y } else { y - 399 } / 400;
    let yoe = y - era * 400;
    let mp = (m + 9) % 12;
    let doy = (153 * mp + 2) / 5 + d - 1;
    let doe = yoe * 365 + yoe / 4 - yoe / 100 + doy;
    era * 146_097 + doe - 719_468
}

/// Port of `WriteSessionsJSON` (`internal/picker/list.go:53`): indented JSON array of
/// registry rows (a nil list prints `[]`), terminated by a newline.
fn write_sessions_json(sessions: &Value) -> Result<String, String> {
    let items: Vec<Value> = sessions.as_array().cloned().unwrap_or_default();
    let mut out = serde_json::to_string_pretty(&items).map_err(|e| e.to_string())?;
    out.push('\n');
    Ok(out)
}

// ---------------------------------------------------------------------------
// fzf item formatting (`internal/picker/fzf.go`, `entries.go`, `colors.go`, `ansi_clip.go`)
// ---------------------------------------------------------------------------

/// Port of `formatFzfInput` (`internal/picker/fzf.go:142`): items joined by NUL.
fn format_fzf_input(sessions: &Value, profile: Profile) -> String {
    let items: Vec<String> = sessions
        .as_array()
        .cloned()
        .unwrap_or_default()
        .into_iter()
        .map(|session| format_fzf_line(&session, profile))
        .collect();
    items.join("\0")
}

/// Port of `formatFzfLine` (`internal/picker/fzf.go:153`): `key\tlabel`, the key being
/// the tmux target (or id), tabs inside the label collapsed to spaces.
fn format_fzf_line(session: &Value, profile: Profile) -> String {
    let mut key = str_field(session, "tmux_target");
    if key.is_empty() {
        key = str_field(session, "id");
    }
    let label = format_session_line(session, 120, profile).replace('\t', " ");
    format!("{key}\t{label}")
}

/// Port of `formatSessionLine` (`internal/picker/entries.go:284`).
fn format_session_line(session: &Value, width: i64, profile: Profile) -> String {
    format_session_entry(session, width, profile).join("\n")
}

/// Port of `formatSessionEntry` (`internal/picker/entries.go:275`).
fn format_session_entry(session: &Value, width: i64, profile: Profile) -> Vec<String> {
    let status = registry_rs::port::display_status_json(session.clone())
        .ok()
        .and_then(|v| v.as_str().map(str::to_string))
        .unwrap_or_default();
    let mut lines = layout_meta_chunks(&status, &build_meta_chunks(session), width, profile);
    let prompt = format_prompt_line(session, width, profile);
    if !prompt.is_empty() {
        lines.push(prompt);
    }
    lines
}

/// Port of `formatPromptLine` (`internal/picker/entries.go:260`).
fn format_prompt_line(session: &Value, width: i64, profile: Profile) -> String {
    let mut prompt = str_field(session, "last_prompt").trim().to_string();
    if prompt.is_empty() {
        prompt = str_field(session, "title").trim().to_string();
    }
    if prompt.is_empty() {
        prompt = "(no prompt)".to_string();
    }
    // Collapse all whitespace runs to a single space (Go strings.Fields).
    prompt = prompt.split_whitespace().collect::<Vec<_>>().join(" ");
    render_icon_text_chunk(ICON_PROMPT, &prompt, PROMPT_STYLE, width, profile)
}

// Nerd Font codepoints (`internal/picker/picker.go:51-68`).
const ICON_UNKNOWN: &str = "\u{F0625}";
const ICON_IDLE: &str = "\u{F04B2}";
const ICON_WORKING: &str = "\u{F09D1}";
const ICON_TOOL_CALL: &str = "\u{F0493}";
const ICON_STATUS_TOOL_CALL: &str = "\u{F0493}";
const ICON_SUBAGENTS_RUNNING: &str = "\u{F04AA}";
const ICON_BG_JOBS_RUNNING: &str = "\u{F08D6}";
const ICON_HALTED: &str = "\u{F0377}";
const ICON_AWAITING_INPUT: &str = "\u{F128}";
const ICON_FOLDER: &str = "\u{F0256}";
const ICON_BRANCH: &str = "\u{F062C}";
const ICON_AGENT: &str = "\u{F06A9}";
const ICON_MODEL: &str = "\u{F09D1}";
const ICON_SESSION: &str = "\u{F018D}";
const ICON_SESSION_NAME: &str = "\u{F02E}";
const ICON_PANE: &str = "\u{F0BCC}";
const ICON_PROMPT: &str = "\u{F036A}";

/// Port of `statusIcon` (`internal/picker/picker.go:962`).
fn status_icon(status: &str) -> &'static str {
    match status {
        "working" => ICON_WORKING,
        "halted" => ICON_HALTED,
        "awaiting_input" => ICON_AWAITING_INPUT,
        "tool_call" => ICON_STATUS_TOOL_CALL,
        "subagents_running" => ICON_SUBAGENTS_RUNNING,
        "bg_jobs_running" => ICON_BG_JOBS_RUNNING,
        "unknown" => ICON_UNKNOWN,
        _ => ICON_IDLE,
    }
}

/// Port of `shortCWD` (`internal/picker/entries.go:69`).
fn short_cwd(path: &str) -> String {
    if path.is_empty() {
        return String::new();
    }
    let parts: Vec<&str> = path.trim_matches('/').split('/').collect();
    if parts.len() <= 3 {
        return path.to_string();
    }
    format!("…/{}", parts[parts.len() - 3..].join("/"))
}

/// Port of `sessionPaneLabel` (`internal/picker/discover.go:64`).
fn session_pane_label(session: &Value) -> String {
    let window = str_field(session, "tmux_window").trim().to_string();
    let pane = str_field(session, "tmux_pane").trim().to_string();
    if window.is_empty() || pane.is_empty() {
        return String::new();
    }
    format!("{window}.{pane}")
}

// ---------------------------------------------------------------------------
// Styles (`internal/picker/colors.go`)
// ---------------------------------------------------------------------------

/// Port of `colorprofile.Profile` as far as the env-based detection reaches.
#[derive(Clone, Copy, PartialEq, Eq)]
enum Profile {
    Ascii,
    Ansi,
    Ansi256,
    TrueColor,
}

/// Port of `detectColorProfile` (`internal/picker/colors.go:42`). Go falls back to
/// `colorprofile.Detect(os.Stdout, ...)`; on a non-tty that yields Ascii, matching the
/// env-only fallback used here.
fn detect_color_profile() -> Profile {
    if std::env::var_os("NO_COLOR").is_some_and(|v| !v.is_empty()) {
        return Profile::Ascii;
    }
    let term = std::env::var("TERM").unwrap_or_default();
    let color_term = std::env::var("COLORTERM").unwrap_or_default().to_lowercase();
    if color_term == "truecolor" || color_term == "24bit" {
        return Profile::TrueColor;
    }
    if term.contains("256color") {
        return Profile::Ansi256;
    }
    if term.contains("color") || term.contains("ansi") {
        return Profile::Ansi;
    }
    Profile::Ascii
}

/// A foreground color: either one of the 16 ANSI colors or a 256-color index.
#[derive(Clone, Copy)]
enum Fg {
    Ansi(u8),
    Ext(u8),
}

#[derive(Clone, Copy)]
struct Style {
    bold: bool,
    faint: bool,
    fg: Option<Fg>,
}

const fn style(bold: bool, faint: bool, fg: Fg) -> Style {
    Style {
        bold,
        faint,
        fg: Some(fg),
    }
}

// Entry element styles (`internal/picker/colors.go:16-27`).
const SESSION_STYLE: Style = style(true, false, Fg::Ansi(15));
const SESSION_NAME_STYLE: Style = style(true, false, Fg::Ansi(13));
const PANE_STYLE: Style = style(false, false, Fg::Ansi(3));
const AGENT_STYLE: Style = style(false, false, Fg::Ext(135));
const MODEL_STYLE: Style = style(false, false, Fg::Ansi(3));
const BRANCH_STYLE: Style = style(true, false, Fg::Ansi(14));
const TOOL_STYLE: Style = style(true, false, Fg::Ansi(5));
const SUBAGENT_COUNT_STYLE: Style = style(true, false, Fg::Ansi(5));
const BG_JOB_COUNT_STYLE: Style = style(true, false, Fg::Ansi(4));
const CWD_STYLE: Style = style(false, true, Fg::Ansi(4));
const PROMPT_STYLE: Style = style(false, true, Fg::Ansi(7));

/// Port of `statusLabelStyle` (`internal/picker/colors.go:75-91`).
fn status_label_style(status: &str) -> Style {
    match status {
        "working" | "tool_call" | "subagents_running" | "bg_jobs_running" => {
            style(true, false, Fg::Ansi(2))
        }
        "halted" => style(true, false, Fg::Ansi(1)),
        "awaiting_input" => style(true, false, Fg::Ext(220)),
        "unknown" => style(true, false, Fg::Ansi(6)),
        _ => style(false, false, Fg::Ansi(8)),
    }
}

/// Port of `lipgloss.Style.Render` for these styles: SGR prefix (bold, faint, fg) +
/// text + `ESC[m`; the Ascii profile emits the text unchanged.
fn render(style: Style, text: &str, profile: Profile) -> String {
    if profile == Profile::Ascii {
        return text.to_string();
    }
    let mut codes: Vec<String> = Vec::new();
    if style.bold {
        codes.push("1".to_string());
    }
    if style.faint {
        codes.push("2".to_string());
    }
    if let Some(fg) = style.fg
        && let Some(code) = fg_code(fg, profile)
    {
        codes.push(code);
    }
    if codes.is_empty() {
        return text.to_string();
    }
    format!("\x1b[{}m{}\x1b[m", codes.join(";"), text)
}

fn fg_code(fg: Fg, profile: Profile) -> Option<String> {
    match profile {
        Profile::Ascii => None,
        Profile::Ansi256 => Some(match fg {
            Fg::Ansi(n) => format!("38;5;{n}"),
            Fg::Ext(n) => format!("38;5;{n}"),
        }),
        Profile::Ansi => Some(match fg {
            Fg::Ansi(n) if n < 8 => format!("{}", 30 + n),
            Fg::Ansi(n) => format!("{}", 90 + n - 8),
            Fg::Ext(n) => format!("{}", ansi_from_ext(n)),
        }),
        Profile::TrueColor => {
            let (r, g, b) = match fg {
                Fg::Ansi(n) => ansi_rgb(n),
                Fg::Ext(n) => ext_rgb(n),
            };
            Some(format!("38;2;{r};{g};{b}"))
        }
    }
}

/// Down-convert a 256-color index to the nearest ANSI color code (termenv-style).
fn ansi_from_ext(n: u8) -> u8 {
    if n < 16 {
        return if n < 8 { 30 + n } else { 90 + n - 8 };
    }
    if (232..=255).contains(&n) {
        return if n < 244 { 30 } else { 90 };
    }
    let i = n - 16;
    let r = i / 36;
    let g = (i / 6) % 6;
    let b = i % 6;
    let bright = r.max(g).max(b) >= 4;
    let hue = (if b > 0 { 1 } else { 0 }) | (if g > 0 { 2 } else { 0 }) | (if r > 0 { 4 } else { 0 });
    if bright { 90 + hue } else { 30 + hue }
}

fn ansi_rgb(n: u8) -> (u8, u8, u8) {
    const TABLE: [(u8, u8, u8); 16] = [
        (0, 0, 0),
        (205, 0, 0),
        (0, 205, 0),
        (205, 205, 0),
        (0, 0, 238),
        (205, 0, 205),
        (0, 205, 205),
        (229, 229, 229),
        (127, 127, 127),
        (255, 0, 0),
        (0, 255, 0),
        (255, 255, 0),
        (92, 92, 255),
        (255, 0, 255),
        (0, 255, 255),
        (255, 255, 255),
    ];
    TABLE[n.min(15) as usize]
}

fn ext_rgb(n: u8) -> (u8, u8, u8) {
    if n < 16 {
        return ansi_rgb(n);
    }
    if (232..=255).contains(&n) {
        let v = 8 + (n - 232) * 10;
        return (v, v, v);
    }
    let i = n - 16;
    let scale = |v: u8| -> u8 {
        if v == 0 {
            0
        } else {
            55 + v * 40
        }
    };
    (
        scale(i / 36),
        scale((i / 6) % 6),
        scale(i % 6),
    )
}

/// Port of `ensureResetSuffix` (`internal/picker/ansi_clip.go:37`).
fn ensure_reset_suffix(line: &str) -> String {
    if line.contains('\x1b') && !line.ends_with("\x1b[0m") {
        format!("{line}\x1b[0m")
    } else {
        line.to_string()
    }
}

/// Port of `truncateANSI` (`internal/picker/ansi_clip.go:12`).
fn truncate_ansi(line: &str, width: i64) -> String {
    if width < 1 {
        return String::new();
    }
    if (line.len() as i64) <= width {
        return line.to_string();
    }
    ansi_truncate(line, width as usize)
}

/// Port of `ansi.Truncate(line, width, "")`: keep SGR sequences, cut visible cells at
/// width, close any style that was opened.
fn ansi_truncate(line: &str, width: usize) -> String {
    let mut out = String::new();
    let mut used = 0usize;
    let mut in_sgr = false;
    let mut rest = line;
    while !rest.is_empty() {
        if rest.starts_with('\x1b') {
            if let Some(end) = rest.find(['m', 'K', 'J', 'H', 'f', 'A', 'B', 'C', 'D']) {
                let seq = &rest[..=end];
                if seq.ends_with('m') {
                    in_sgr = !seq.ends_with("\x1b[0m") && !seq.ends_with("\x1b[m");
                    out.push_str(seq);
                }
                rest = &rest[end + 1..];
                continue;
            }
        }
        let c = rest.chars().next().unwrap();
        let w = char_width(c);
        if used + w > width {
            break;
        }
        used += w;
        out.push(c);
        rest = &rest[c.len_utf8()..];
    }
    if in_sgr {
        out.push_str("\x1b[0m");
    }
    out
}

/// Port of `lipgloss.Width`: display cells of the text, ignoring SGR sequences.
fn display_width(text: &str) -> i64 {
    let mut width = 0i64;
    let mut rest = text;
    while !rest.is_empty() {
        if rest.starts_with('\x1b') {
            if let Some(end) = rest.find(['m', 'K', 'J', 'H', 'f', 'A', 'B', 'C', 'D']) {
                rest = &rest[end + 1..];
                continue;
            }
        }
        let c = rest.chars().next().unwrap();
        width += char_width(c) as i64;
        rest = &rest[c.len_utf8()..];
    }
    width
}

/// Cell width of one rune (ASCII 1, controls/combining 0, East Asian wide 2, else 1).
fn char_width(c: char) -> usize {
    let u = c as u32;
    if u == 0 {
        return 0;
    }
    if c.is_control() {
        return 0;
    }
    // Common combining marks.
    if (0x0300..=0x036F).contains(&u)
        || (0x1AB0..=0x1AFF).contains(&u)
        || (0x20D0..=0x20FF).contains(&u)
        || (0xFE00..=0xFE0F).contains(&u)
    {
        return 0;
    }
    // East Asian Wide / Fullwidth blocks (the ranges that matter for picker text).
    if (0x1100..=0x115F).contains(&u)
        || (0x2E80..=0x303E).contains(&u)
        || (0x3041..=0x33FF).contains(&u)
        || (0x3400..=0x4DBF).contains(&u)
        || (0x4E00..=0x9FFF).contains(&u)
        || (0xA000..=0xA4CF).contains(&u)
        || (0xAC00..=0xD7A3).contains(&u)
        || (0xF900..=0xFAFF).contains(&u)
        || (0xFE10..=0xFE19).contains(&u)
        || (0xFE30..=0xFE6F).contains(&u)
        || (0xFF01..=0xFF60).contains(&u)
        || (0xFFE0..=0xFFE6).contains(&u)
        || (0x1F300..=0x1F64F).contains(&u)
        || (0x1F900..=0x1F9FF).contains(&u)
        || (0x20000..=0x2FFFD).contains(&u)
        || (0x30000..=0x3FFFD).contains(&u)
    {
        return 2;
    }
    1
}

/// Port of `isPlainASCII` (`internal/picker/entries.go:21`).
fn is_plain_ascii(text: &str) -> bool {
    text.bytes().all(|b| (0x20..0x7F).contains(&b))
}

/// Port of `ellipsis` (`internal/picker/entries.go:33`).
fn ellipsis(text: &str, width: i64) -> String {
    if width < 1 {
        return "…".to_string();
    }
    if is_plain_ascii(text) {
        if (text.len() as i64) <= width {
            return text.to_string();
        }
        if width == 1 {
            return "…".to_string();
        }
        return format!("{}…", &text[..(width - 1) as usize]);
    }
    if display_width(text) <= width {
        return text.to_string();
    }
    let chars: Vec<char> = text.chars().collect();
    let limit = width - 1;
    let mut lo = 0usize;
    let mut hi = chars.len();
    while lo < hi {
        let mid = (lo + hi + 1) / 2;
        let w: i64 = chars[..mid].iter().map(|c| char_width(*c) as i64).sum();
        if w <= limit {
            lo = mid;
        } else {
            hi = mid - 1;
        }
    }
    if lo == 0 {
        return "…".to_string();
    }
    format!("{}…", chars[..lo].iter().collect::<String>())
}

/// Port of `renderStatusChunk` (`internal/picker/entries.go:80`).
fn render_status_chunk(status: &str, profile: Profile) -> String {
    ensure_reset_suffix(&render(
        status_label_style(status),
        &format!("{} ", status_icon(status)),
        profile,
    ))
}

/// Port of `renderIconTextChunk` (`internal/picker/entries.go:84`).
fn render_icon_text_chunk(
    icon: &str,
    text: &str,
    style: Style,
    max_width: i64,
    profile: Profile,
) -> String {
    let mut text = text.trim().to_string();
    if text.is_empty() {
        return String::new();
    }
    if max_width < 1 {
        return ensure_reset_suffix(&render(style, &format!("{icon} {text}"), profile));
    }
    let icon_prefix = render(style, &format!("{icon} "), profile);
    let icon_width = display_width(&icon_prefix);
    if icon_width >= max_width {
        return ensure_reset_suffix(&truncate_ansi(&icon_prefix, max_width));
    }
    let text_budget = max_width - icon_width;
    text = ellipsis(&text, text_budget);
    let mut rendered = render(style, &format!("{icon} {text}"), profile);
    if display_width(&rendered) > max_width {
        rendered = format!("{}…", truncate_ansi(&rendered, max_width - 1));
    }
    ensure_reset_suffix(&rendered)
}

struct MetaChunk {
    icon: &'static str,
    text: String,
    style: Style,
}

/// Port of `buildMetaChunks` (`internal/picker/entries.go:108`).
fn build_meta_chunks(session: &Value) -> Vec<MetaChunk> {
    let mut agent = str_field(session, "agent").trim().to_string();
    if agent.is_empty() {
        agent = "pi".to_string(); // config.DefaultAgent
    }
    let mut session_name = str_field(session, "tmux_session").trim().to_string();
    if session_name.is_empty() {
        session_name = "?".to_string();
    }
    let mut chunks: Vec<MetaChunk> = Vec::new();
    let pi_name = str_field(session, "pi_session_name").trim().to_string();
    if !pi_name.is_empty() {
        chunks.push(MetaChunk {
            icon: ICON_SESSION_NAME,
            text: pi_name,
            style: SESSION_NAME_STYLE,
        });
    }
    chunks.push(MetaChunk {
        icon: ICON_SESSION,
        text: session_name,
        style: SESSION_STYLE,
    });
    let pane_label = session_pane_label(session);
    if !pane_label.is_empty() {
        chunks.push(MetaChunk {
            icon: ICON_PANE,
            text: pane_label,
            style: PANE_STYLE,
        });
    }
    chunks.push(MetaChunk {
        icon: ICON_AGENT,
        text: agent,
        style: AGENT_STYLE,
    });
    if session.get("subagents_running").and_then(Value::as_i64).unwrap_or(0) > 0 {
        chunks.push(MetaChunk {
            icon: ICON_SUBAGENTS_RUNNING,
            text: session
                .get("subagents_running")
                .and_then(Value::as_i64)
                .unwrap_or(0)
                .to_string(),
            style: SUBAGENT_COUNT_STYLE,
        });
    }
    if session.get("bg_jobs_running").and_then(Value::as_i64).unwrap_or(0) > 0 {
        chunks.push(MetaChunk {
            icon: ICON_BG_JOBS_RUNNING,
            text: session
                .get("bg_jobs_running")
                .and_then(Value::as_i64)
                .unwrap_or(0)
                .to_string(),
            style: BG_JOB_COUNT_STYLE,
        });
    }
    let model = str_field(session, "model").trim().to_string();
    if !model.is_empty() {
        chunks.push(MetaChunk {
            icon: ICON_MODEL,
            text: model,
            style: MODEL_STYLE,
        });
    }
    let branch = str_field(session, "branch").trim().to_string();
    if !branch.is_empty() {
        chunks.push(MetaChunk {
            icon: ICON_BRANCH,
            text: branch,
            style: BRANCH_STYLE,
        });
    }
    let tool = str_field(session, "tool_name").trim().to_string();
    if !tool.is_empty() {
        chunks.push(MetaChunk {
            icon: ICON_TOOL_CALL,
            text: tool,
            style: TOOL_STYLE,
        });
    }
    let cwd = short_cwd(str_field(session, "cwd").trim());
    if !cwd.is_empty() {
        chunks.push(MetaChunk {
            icon: ICON_FOLDER,
            text: cwd,
            style: CWD_STYLE,
        });
    }
    chunks
}

/// Port of `layoutMetaChunks` (`internal/picker/entries.go:148`).
fn layout_meta_chunks(
    status: &str,
    chunks: &[MetaChunk],
    width: i64,
    profile: Profile,
) -> Vec<String> {
    if width < 1 {
        return Vec::new();
    }
    struct Piece {
        content: String,
        width: i64,
        chunk: Option<usize>,
        is_status: bool,
    }
    let mut pieces: Vec<Piece> = Vec::new();
    let status_content = render_status_chunk(status, profile);
    if !status_content.is_empty() {
        pieces.push(Piece {
            width: display_width(&status_content),
            content: status_content,
            chunk: None,
            is_status: true,
        });
    }
    for (i, chunk) in chunks.iter().enumerate() {
        let content = render_icon_text_chunk(chunk.icon, &chunk.text, chunk.style, 0, profile);
        if content.is_empty() {
            continue;
        }
        pieces.push(Piece {
            width: display_width(&content),
            content,
            chunk: Some(i),
            is_status: false,
        });
    }

    let sep = " ";
    let sep_width = display_width(sep);
    let mut lines: Vec<String> = Vec::new();
    let mut current: Vec<Piece> = Vec::new();
    let mut used = 0i64;

    let render_at_width = |p: &Piece, line_width: i64| -> String {
        if p.is_status {
            render_status_chunk(status, profile)
        } else if let Some(i) = p.chunk {
            render_icon_text_chunk(
                chunks[i].icon,
                &chunks[i].text,
                chunks[i].style,
                line_width,
                profile,
            )
        } else {
            truncate_ansi(&p.content, line_width)
        }
    };
    let render_exclusive = |p: &Piece| -> String {
        let mut content = render_at_width(p, width);
        if display_width(&content) > width {
            content = format!("{}…", truncate_ansi(&content, width - 1));
        }
        ensure_reset_suffix(&content)
    };

    for p in pieces {
        let mut need = p.width;
        if !current.is_empty() {
            need = used + sep_width + p.width;
        }
        if !current.is_empty() && need > width {
            // Keep the status marker glued to the leading chunk when the line so
            // far holds only the status; truncate that chunk to the remainder.
            if current.len() == 1 && current[0].is_status {
                let remaining = width - used - sep_width;
                if remaining >= 1 {
                    let mut content = render_at_width(&p, remaining);
                    if display_width(&content) > remaining {
                        content = format!("{}…", truncate_ansi(&content, remaining - 1));
                    }
                    used += sep_width;
                    let w = display_width(&content);
                    used += w;
                    current.push(Piece {
                        content: ensure_reset_suffix(&content),
                        width: w,
                        chunk: p.chunk,
                        is_status: p.is_status,
                    });
                    continue;
                }
            }
            if !current.is_empty() {
                let joined = current
                    .iter()
                    .map(|p| p.content.clone())
                    .collect::<Vec<_>>()
                    .join(sep);
                lines.push(ensure_reset_suffix(&joined));
                current.clear();
                used = 0;
            }
        }
        if p.width > width {
            lines.push(render_exclusive(&p));
            continue;
        }
        if !current.is_empty() {
            used += sep_width;
        }
        used += p.width;
        current.push(p);
    }
    if !current.is_empty() {
        let joined = current
            .iter()
            .map(|p| p.content.clone())
            .collect::<Vec<_>>()
            .join(sep);
        lines.push(ensure_reset_suffix(&joined));
    }
    if lines.is_empty() {
        return vec![String::new()];
    }
    lines
}

/// Go `%q`-style quoting for the session-key error (escape non-printables the same
/// way Go's `strconv.Quote` does; printable non-ASCII stays literal).
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

/// Port of `Run` (`internal/picker/picker.go:161`).
pub fn run() -> Result<(), String> {
    unimplemented!("port `Run` from internal/picker/picker.go")
}
