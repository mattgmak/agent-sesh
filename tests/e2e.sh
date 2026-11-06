#!/bin/sh
# tests/e2e.sh — end-to-end check for agent-sesh on a throwaway tmux server.
#
# It builds the binary, starts a toy tmux server on its own socket, fills it
# with three panes that each run a fake `pi` process, seeds the registry with
# their real pane ids, then drives every non-interactive command and checks the
# output. The interactive Bubble Tea picker and the fzf picker need a real tty,
# so the harness runs them last and only when tests/e2e-interactive.sh exists.
#
# Usage:
#   sh tests/e2e.sh              build, run every phase, keep the run dir
#   SKIP_BUILD=1 sh tests/e2e.sh reuse the binary from the last run
#   KEEP=1 sh tests/e2e.sh       leave the toy server up for poking by hand
#
# Exit code: 0 when every check passed, 1 otherwise.
set -u

REPO=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
TMP=${TMPDIR:-/tmp}/agent-sesh
RUN=$TMP/run
INTERACTIVE=${INTERACTIVE:-1}
SKIP_BUILD=${SKIP_BUILD:-0}
KEEP=${KEEP:-1}
TMOUX_BIN=${TMUX_BIN:-tmux}
TMOUX_SOCKET=$RUN/tmux.sock

say()  { printf '%s\n' "$*"; }
section() { printf '\n== %s\n' "$1"; }
fail() {
  FAILED=$((FAILED + 1))
  printf '  FAIL  %s\n' "$1"
  if [ -s "$WORK/out" ]; then
    sed 's/^/        /' "$WORK/out" | head -12
  fi
}
pass() { printf '  ok    %s\n' "$1"; }
skip() { printf '  skip  %s\n' "$1"; }

# tmux_try <desc> <command...> — one tmux command, retried three times.
# A socket path reused right after kill-server sometimes answers "server exited
# unexpectedly" while the old server finishes exiting. The retry covers it.
tmux_try() {
  desc=$1
  shift
  n=1
  while :; do
    if "$@" >"$WORK/out" 2>&1; then
      pass "$desc"
      return 0
    fi
    [ "$n" -ge 3 ] && { fail "$desc (after $n attempts)"; return 1; }
    n=$((n + 1))
    sleep 1
  done
}

# run <desc> as <subcommand...> — run one agent-sesh command, keep its output.
# Without `as`, the remaining args run verbatim (sh -c, curl, and similar).
run() {
  desc=$1
  shift
  if [ "$1" = as ]; then
    shift
    set -- "$BIN" "$@"
  fi
  if "$@" >"$WORK/out" 2>&1; then
    pass "$desc"
    return 0
  fi
  fail "$desc"
  return 1
}

# runnot <desc> as <subcommand...> — pass only when the command fails.
runnot() {
  desc=$1
  shift
  if [ "$1" = as ]; then
    shift
    set -- "$BIN" "$@"
  fi
  if "$@" >"$WORK/out" 2>&1; then
    fail "$desc (expected a non-zero exit)"
    return 1
  fi
  pass "$desc"
  return 0
}

# has <desc> <needle> [file] — pass when file (default stdout) holds needle.
has() {
  desc=$1
  needle=$2
  file=${3:-$WORK/out}
  if grep -Fq -- "$needle" "$file" 2>/dev/null; then
    pass "$desc"
    return 0
  fi
  fail "$desc (missing: $needle)"
  return 1
}

# hasnt <desc> <needle> — pass when stdout does not hold needle.
hasnt() {
  desc=$1
  needle=$2
  if grep -Fq -- "$needle" "$WORK/out" 2>/dev/null; then
    fail "$desc (unexpected: $needle)"
    return 1
  fi
  pass "$desc"
  return 0
}

# seed_registry <target> <status> [target status]... — write the registry file.
# Rows must carry the real tmux pane id: registry.Sanitize drops any row whose
# pane id is missing from the tmux snapshot.
seed_registry() {
  tmp=$WORK/seed.json
  body=$(
    while [ "$#" -gt 0 ]; do
      target=$1
      status=$2
      shift 2
      printf '{"id":"%s","agent":"pi","agent_session_name":"e2e","tmux_target":"%s","branch":"main","status":"%s","updated_at":"%s"}\n' \
        "$target" "$target" "$status" "$NOW"
    done | paste -sd, -
  )
  printf '{"version":1,"sessions":[%s]}\n' "$body" >"$tmp"
  cp "$tmp" "$REG"
}

# pane_id <window-name> — the pane id tmux gave a window.
pane_id() {
  $TMOUX_BIN -S "$SOCKET" list-panes -a -F '#{window_name} #{pane_id}' |
    awk -v w="$1" '$1 == w { print $2; exit }'
}

FAILED=0
WORK=$RUN
mkdir -p "$RUN" || exit 1
BIN=$RUN/agent-sesh
SOCKET=$TMOUX_SOCKET

section "0. build"
if [ "$SKIP_BUILD" = 1 ] && [ -x "$BIN" ]; then
  pass "using $BIN"
else
  (cd "$REPO" && CGO_ENABLED=0 go build -o "$BIN" ./cmd/agent-sesh) >"$WORK/out" 2>&1 &&
    pass "go build ./cmd/agent-sesh" || fail "go build ./cmd/agent-sesh"
fi
if [ ! -x "$BIN" ] && [ "$SKIP_BUILD" != 1 ]; then
  say "build failed, stopping"
  exit 1
fi

section "1. toy tmux server on $SOCKET"
rm -rf "$RUN/state" "$RUN/logs"
mkdir -p "$RUN/state/agent-sesh" "$RUN/logs"
STATE=$RUN/state
REG=$STATE/agent-sesh/sessions.json
CONFIG=$STATE/agent-sesh/config.json
PORT=${PORT:-28876}
export AGENT_SESH_CONFIG=$CONFIG
export XDG_STATE_HOME=$STATE
NOW=$(date -u +%Y-%m-%dT%H:%M:%SZ)

printf '{"version":1,"agents":["pi"],"daemon":{"port":%s},"notify":{"enabled":true,"backend":"log"}}\n' "$PORT" >"$CONFIG"

# Each pane runs a script named pi so the snapshot proves the agent, prints a
# unique marker for the preview test, then reads stdin so send-keys has a
# reader. A plain `exec sh` dies at once under tmux, so the loop reads instead.
mkdir -p "$RUN/bin"
cat >"$RUN/bin/pi" <<'PI'
#!/bin/sh
echo "${MARK:-none}"
while IFS= read -r -t 300 line; do
  printf 'got %s\n' "$line"
done
PI
chmod +x "$RUN/bin/pi"

# Stop the server from the previous run and wait for its socket to go away,
# then wait one more second: tmux racing a reused socket path is the one flaky
# part of this harness.
if [ -S "$SOCKET" ]; then
  $TMOUX_BIN -S "$SOCKET" kill-server >"$WORK/out" 2>&1 || true
fi
n=0
while [ -S "$SOCKET" ] && [ "$n" -lt 20 ]; do
  sleep 0.2
  n=$((n + 1))
done
sleep 1

tmux_try "new-session e2e:one" \
  $TMOUX_BIN -S "$SOCKET" new-session -d -s e2e -n one -x 200 -y 50 -e MARK=PANE_ONE "$RUN/bin/pi"
tmux_try "new-window e2e:two" \
  $TMOUX_BIN -S "$SOCKET" new-window -d -t e2e -n two -e MARK=PANE_TWO "$RUN/bin/pi"
tmux_try "new-window e2e:three" \
  $TMOUX_BIN -S "$SOCKET" new-window -d -t e2e -n three -e MARK=PANE_THREE "$RUN/bin/pi"
sleep 1

T1=$(pane_id one)
T2=$(pane_id two)
T3=$(pane_id three)
if [ -z "$T1" ] || [ -z "$T2" ] || [ -z "$T3" ]; then
  say "toy server has no panes, stopping"
  exit 1
fi
say "  pane ids: $T1 $T2 $T3"
export TMUX="$SOCKET,1,0"

section "2. discovery and registry"
run "debug panes lists unregistered pi panes" as debug panes
has "discovered $T1" "$T1"
has "discovered $T2" "$T2"
has "discovered $T3" "$T3"
run "debug validate" as debug validate

seed_registry "$T1" idle "$T2" working "$T3" tool_call
run "list" as list
has "row for $T1" "$T1"
has "row for $T2" "$T2"
has "row for $T3" "$T3"
run "debug registry" as debug registry
has "debug registry lists rows" "$T1"

section "3. preview"
for target in "$T1" "$T2" "$T3"; do
  case $target in
    "$T1") marker=PANE_ONE ;;
    "$T2") marker=PANE_TWO ;;
    *) marker=PANE_THREE ;;
  esac
  run "preview $target" as preview "$target"
  has "preview $target content" "$marker"
done
runnot "preview unknown target fails" as preview nope:9.9

section "4. list --json"
run "list --json" as list --json
cp "$WORK/out" "$WORK/list.json"
has "json emits session objects" '"id": "' "$WORK/list.json"
has "json holds $T1" "\"id\": \"$T1\"" "$WORK/list.json"
has "json holds $T2" "\"id\": \"$T2\"" "$WORK/list.json"
has "json keeps the seeded status" '"status": "working"' "$WORK/list.json"
has "json keeps the seeded agent" '"agent": "pi"' "$WORK/list.json"
has "json keeps the seeded branch" '"branch": "main"' "$WORK/list.json"

section "5. notify"
run "notify log backend" as notify --backend log --event tool_call --title pi --body "used Bash"
has "log line" "notify: pi | used Bash"
run "notify from a session row" as notify --backend log --session "$T2"
has "row lookup fills the event" "notify: pi $T2 | working"
run "notify derives the title" as notify --backend log --event idle --body "a body"
has "derived title" "notify: agent-sesh | a body idle"
run "notify derives the body" as notify --backend log --event tool_call --title pi
has "derived body" "notify: pi | tool_call"
runnot "notify unknown session fails" as notify --backend log --session nope:9.9
runnot "notify unknown backend fails" as notify --backend bogus --event idle --title t --body b

run "notify command backend" as notify --backend command \
  --command "echo \$AGENT_SESH_EVENT > $WORK/notify.env" --event idle --title t --body b
has "command gets the event env" "idle" "$WORK/notify.env"

if [ -e /dev/tty ]; then
  run "notify osc on a real tty" as notify --backend osc --tty /dev/tty --event idle --title t --body b
else
  skip "notify on /dev/tty (not available)"
fi
: >"$WORK/osc.bin"
run "notify osc to a file" as notify --backend osc --tty "$WORK/osc.bin" --event idle --title t --body b
hasnt "osc did not fall back to log" "notify:"
run "osc file was written" sh -c "test -s $WORK/osc.bin"

section "6. daemon"
PIDFILE=$STATE/agent-sesh/daemon-127.0.0.1_$PORT.pid
run "daemon start" as daemon start
DAEMON_PID=$(cat "$PIDFILE" 2>/dev/null)
if [ -n "$DAEMON_PID" ]; then
  pass "pid file written ($DAEMON_PID)"
else
  fail "pid file written"
fi
run "daemon answers /v1/health" sh -c \
  "curl -fsS --max-time 5 http://127.0.0.1:$PORT/v1/health > $WORK/health.json"
has "health reports the three rows" '"sessions": 3' "$WORK/health.json"
run "daemon answers /v1/sessions" sh -c \
  "curl -fsS --max-time 5 http://127.0.0.1:$PORT/v1/sessions > $WORK/daemon.json"
has "daemon returns sessions" "$T1" "$WORK/daemon.json"
run "daemon answers /v1/counts" sh -c \
  "curl -fsS --max-time 5 http://127.0.0.1:$PORT/v1/counts > $WORK/counts.json"
has "counts matches the seeded rows" '"active":2' "$WORK/counts.json"
run "daemon status" as daemon status
run "daemon notify" as notify --backend log --session "$T3"
run "daemon stop" as daemon stop
if [ -n "$DAEMON_PID" ]; then
  if kill -0 "$DAEMON_PID" 2>/dev/null; then
    fail "daemon process still alive after stop"
  else
    pass "daemon process exited"
  fi
fi
runnot "daemon stop twice fails" as daemon stop
if [ -f "$PIDFILE" ]; then
  fail "pid file left behind"
else
  pass "pid file removed"
fi

section "7. pane gone prunes the row"
tmux_try "kill e2e:three" $TMOUX_BIN -S "$SOCKET" kill-window -t e2e:three
sleep 1
run "list after kill" as list
if grep -q "$T3" "$WORK/out"; then
  fail "pruned row still listed"
else
  pass "row for $T3 pruned"
fi
grep -q "$T1" "$WORK/out" && pass "$T1 still listed" || fail "$T1 still listed"

section "8. interactive picker"
if [ "$INTERACTIVE" = 0 ]; then
  skip "INTERACTIVE=0"
elif [ -x "$REPO/tests/e2e-interactive.sh" ]; then
  sh "$REPO/tests/e2e-interactive.sh" "$RUN" && pass "interactive suite" || fail "interactive suite"
else
  skip "tests/e2e-interactive.sh not built yet"
fi

if [ "$KEEP" = 1 ]; then
  say ""
  say "toy server left up:  tmux -S $SOCKET attach -t e2e"
  say "state dir:           $RUN/state"
  say "binary:              $BIN"
else
  $TMOUX_BIN -S "$SOCKET" kill-server 2>/dev/null
  say ""
  say "toy server killed. run dir kept: $RUN"
fi

say ""
if [ "$FAILED" -eq 0 ]; then
  say "e2e: all checks passed"
  exit 0
fi
say "e2e: $FAILED check(s) failed"
exit 1
