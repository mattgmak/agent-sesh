#!/bin/sh
# tests/e2e-interactive.sh — drive the two interactive pickers on the toy server.
#
# The Bubble Tea picker and the fzf picker need a tty, so each one runs in its
# own window of the toy tmux server. The script captures the window, checks the
# rendered rows, then quits the app with a keystroke and checks the window
# closed. tests/e2e.sh calls it with its run dir; it also runs standalone.
#
# Usage: sh tests/e2e-interactive.sh <run-dir>
# Exit code: 0 when every check passed, 1 otherwise.
set -u

RUN=${1:?usage: e2e-interactive.sh "<run-dir>"}
BIN=$RUN/agent-sesh
SOCKET=$RUN/tmux.sock
STATE=$RUN/state
CONFIG=$STATE/agent-sesh/config.json
TMOUX_BIN=${TMUX_BIN:-tmux}
OUT=$RUN/interactive
FAILED=0
CUR=$OUT/capture.txt

say() { printf '%s\n' "$*"; }
section() { printf '\n== %s\n' "$1"; }
fail() {
  FAILED=$((FAILED + 1))
  printf '  FAIL  %s\n' "$1"
  if [ -s "$CUR" ]; then
    sed 's/^/        /' "$CUR" | head -12
  fi
}
pass() { printf '  ok    %s\n' "$1"; }
skip() { printf '  skip  %s\n' "$1"; }

# check "<window-id> <window-name>" <desc> <needle> — recapture that window and
# pass when it holds needle. The app needs its whole load cascade before the
# preview row appears: registryRefreshInterval (3 s) plus registryReloadDebounce
# (200 ms) plus previewMinRefreshInterval (1.2 s). The old 5 s window timed out
# about a second before that finished. 75 x 0.2 s = 15 s covers it. Each window
# keeps its own file: one shared capture.txt made section 2 overwrite the frame
# section 1 failed on. The name form is the capture target because capture-pane
# on a window id returns a partial buffer for a window no client has viewed.
check() {
  target=e2e:${1#* }
  CUR=$OUT/capture-${1#* }.txt
  n=0
  while [ "$n" -lt 75 ]; do
    $TMOUX_BIN -S "$SOCKET" capture-pane -p -t "$target" >"$CUR" 2>/dev/null
    if grep -Fq -- "$3" "$CUR" 2>/dev/null; then
      pass "$2"
      return 0
    fi
    sleep 0.2
    n=$((n + 1))
  done
  fail "$2 (missing: $3)"
}

# ui_window <name> <command...> — run a command in a new window and print
# "<window-id> <window-name>" on stdout.
# new-window has no -x/-y: window size follows the session size. It also has no
# -d: tmux gives an inactive window the 80x24 default until a client views it,
# so a detached app starts at 80 columns and its pty only later reports the real
# size. Without -d the window is current, the pty reports the session size from
# the first frame, and bubbletea renders the full split layout.
ui_window() {
  name=$1
  shift
  if ! id=$($TMOUX_BIN -S "$SOCKET" new-window -P -F '#{window_id}' -t e2e -n "$name" \
    -e AGENT_SESH_CONFIG=$CONFIG -e XDG_STATE_HOME=$STATE "$@" 2>"$OUT/run.txt"); then
    cat "$OUT/run.txt"
    return 1
  fi
  printf '%s %s\n' "$id" "$name"
}

# close_window <window-id> — send Escape then q, wait up to 5 s for the window.
# fzf starts in insert mode, where q only types into the query, so Escape runs
# first to reach normal mode, where q is bound to abort.
close_window() {
  id=$1
  $TMOUX_BIN -S "$SOCKET" send-keys -t "$id" Escape 2>/dev/null
  sleep 0.5
  $TMOUX_BIN -S "$SOCKET" send-keys -t "$id" q 2>/dev/null
  n=0
  while [ "$n" -lt 25 ]; do
    $TMOUX_BIN -S "$SOCKET" list-windows -a -F '#{window_id}' 2>/dev/null | grep -qx "$id" || return 0
    sleep 0.2
    n=$((n + 1))
  done
  return 1
}

if [ ! -x "$BIN" ]; then
  say "interactive: no binary at $BIN, run tests/e2e.sh first"
  exit 1
fi
if ! $TMOUX_BIN -S "$SOCKET" has-session -t e2e 2>/dev/null; then
  say "interactive: no toy session on $SOCKET, run tests/e2e.sh first"
  exit 1
fi

mkdir -p "$OUT"
# The TUI cursor starts on the first row of the list, so the preview column must
# hold that row's pane text. The list row shows title, window, agent, branch and
# cwd; it never shows the pane id, and preview <target> prints only the captured
# pane text, so the column holds the pane's MARK instead of its %N id. The row
# order follows the seeded timestamps, so a hardcoded window drifts: ask the app
# for its first row, then read that pane's own capture for the expected text.
FIRST=$(AGENT_SESH_CONFIG=$CONFIG XDG_STATE_HOME=$STATE "$BIN" list --json 2>/dev/null |
  sed -n 's/.*"tmux_target": *"\([^"]*\)".*/\1/p' | head -1)
MARK=""
if [ -n "$FIRST" ]; then
  MARK=$($TMOUX_BIN -S "$SOCKET" capture-pane -p -t "$FIRST" 2>/dev/null |
    grep -m1 -o 'PANE_[A-Z]*')
fi

section "1. Bubble Tea picker"
if win=$(ui_window tui "$BIN"); then
  check "$win" "renders the agent column" pi
  check "$win" "renders the session name" e2e
  if [ -n "$MARK" ]; then
    check "$win" "renders the selected pane preview" "$MARK"
  fi
  if close_window "${win%% *}"; then
    pass "q quits the picker"
  else
    fail "q quits the picker"
  fi
else
  fail "start the picker in a window"
fi

section "2. fzf picker"
if ! command -v fzf >/dev/null 2>&1; then
  skip "fzf not installed"
elif win=$(ui_window fzf "env AGENT_SESH_FZF=1 AGENT_SESH_FZF_WINDOW=inline $BIN"); then
  check "$win" "fzf draws its prompt" ">"
  check "$win" "fzf lists a session" e2e
  if close_window "${win%% *}"; then
    pass "q aborts fzf"
  else
    fail "q aborts fzf"
  fi
else
  fail "start fzf in a window"
fi

say ""
if [ "$FAILED" -eq 0 ]; then
  say "interactive: all checks passed"
  exit 0
fi
say "interactive: $FAILED check(s) failed"
exit 1
