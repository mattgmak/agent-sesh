#!/usr/bin/env bash
# Repeatable picker perf metric: View() and one navigation key at 200x50 with
# 40 sessions. Offline (vendor mode, GOPROXY=off), no tmux needed.
#
# Usage: scripts/bench-picker.sh [extra go test flags]
# Env:   BENCH_COUNT (default 3) — runs per benchmark, mean printed at the end.
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo_root"

count="${BENCH_COUNT:-3}"
# CGO off keeps the run independent of a local C toolchain.
export CGO_ENABLED=0
export GOPROXY=off
export GOFLAGS="${GOFLAGS:--mod=vendor}"

out="$(go test ./internal/picker -run '^$' -bench 'BenchmarkPicker' -benchmem -count "$count" "$@")"
printf '%s\n' "$out"

printf '\nmean over %s runs:\n' "$count"
printf '%s\n' "$out" | awk '
function mean(x, k) { return x[k] / cnt[k] }
/^Benchmark/ {
  name = $1
  sub(/-[0-9]+$/, "", name)
  for (i = 2; i <= NF; i++) {
    if ($i == "ns/op")      ns[name]     += $(i - 1)
    if ($i == "B/op")       bytes[name]  += $(i - 1)
    if ($i == "allocs/op")  allocs[name] += $(i - 1)
  }
  cnt[name]++
}
END {
  for (n in cnt)
    printf "%-28s %12.0f ns/op %10.0f B/op %8.0f allocs/op\n", n, mean(ns, n), mean(bytes, n), mean(allocs, n)
}' | sort
