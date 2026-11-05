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

say() { printf '%s\n' "$*"; }
section() { printf '\n== %s\n' "$1"; }
fail() {
  FAILED=$((FAILED + 1))
  printf '  FAIL  %s\n' "$1"
  if [ -s "$OUT/capture.txt" ]; then
    sed 's/^/        /' "$OUT/capture.txt" | head -12
  fi
}
pass() { printf '  ok    %s\n' "$1"; }
skip() { printf '  skip  %s\n' "$1"; }

# check <desc> <needle> <file> — pass when file holds needle.
check() {
  if grep -Fq -- "$2" "$3" 2>/dev/null; then
    pass "$1"
  else
    fail "$1 (missing: $2)"
  fi
}

# ui_window <name> <command...> — run a command in a new window, wait for the
# first frame, and return the window id and captured text.
# new-window has no -x/-y: window size follows the session size.
ui_window() {
  name=$1
  shift
  rm -f "$OUT/capture.txt"
  if ! $TMOUX_BIN -S "$SOCKET" new-window -d -P -F '#{window_id}' -t e2e -n "$name" \
    -e AGENT_SESH_CONFIG=$CONFIG -e XDG_STATE_HOME=$STATE "$@" >"$OUT/window.txt" 2>"$OUT/run.txt"; then
    cat "$OUT/run.txt"
    return 1
  fi
  n=0
  while [ ! -s "$OUT/capture.txt" ] && [ "$n" -lt 25 ]; do
    sleep 0.2
    $TMOUX_BIN -S "$SOCKET" capture-pane -p -t "e2e:$name" >"$OUT/capture.txt" 2>/dev/null
    n=$((n + 1))
  done
  [ -s "$OUT/capture.txt" ]
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
TARGET=$($TMOUX_BIN -S "$SOCKET" list-panes -t e2e:1 -F '#{pane_id}' 2>/dev/null | head -1)

section "1. Bubble Tea picker"
if ui_window tui "$BIN"; then
  win=$(cat "$OUT/window.txt")
  check "renders the agent column" pi "$OUT/capture.txt"
  check "renders the session name" e2e "$OUT/capture.txt"
  if [ -n "$TARGET" ]; then
    check "renders the selected pane" "$TARGET" "$OUT/capture.txt"
  fi
  if close_window "$win"; then
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
elif ui_window fzf "env AGENT_SESH_FZF=1 AGENT_SESH_FZF_WINDOW=inline $BIN"; then
  win=$(cat "$OUT/window.txt")
  check "fzf draws its prompt" ">" "$OUT/capture.txt"
  check "fzf lists a session" e2e "$OUT/capture.txt"
  if close_window "$win"; then
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
