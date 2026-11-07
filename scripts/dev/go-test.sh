#!/usr/bin/env bash
# [GO_TEST_SHARD=others|K/N|desk] scripts/dev/go-test.sh   (the go-test step of dev.json)
# scripts/dev/go-test.sh --shards                          (the CI matrix: [1, 2, ..., N])
#
# internal/desk's suite is far longer than one CI job (thousands of top-level tests, many
# starting a Desk of their own), so it runs in shards: CI runs each shard as one job of the Go
# matrix in .github/workflows/ci.yml, and the Go job checks that together they reported every
# test the package lists (scripts/dev/go-test-shards.py, which also says how a test is assigned).
#
#   GO_TEST_SHARD unset    every package, internal/desk shard after shard (hours, not minutes)
#   GO_TEST_SHARD=others   every package except internal/desk
#   GO_TEST_SHARD=desk     internal/desk, shard after shard
#   GO_TEST_SHARD=K/N      internal/desk shard K of N (N is normally the default below)
#
# GO_TEST_SHARD_MINUTES (default 30) bounds the test binary of each shard, and GO_TEST_TIMEOUT
# (default 40m) the run of the other packages. Tests run with -v so skips stay visible. With
# JPDEV_LOGDIR set, each shard leaves its full log, its test list and a record of which shard it
# is there (go-test-desk.log, go-test-desk.list, go-test-shard.json), written as the run goes,
# and under CI the shard's failing tests are added to the job summary.
set -uo pipefail
. "$(dirname "$0")/lib.sh"
cd "$root" || exit 2

# The number of shards. Measured in November 2026, the package's tests took about 3.6 hours
# on a hosted runner, one at a time (a sample spread over the whole list, scaled by tests timed
# both there and locally; one standard error is about an hour), so 16 shards run about 14
# minutes each, and stay inside their 30 if the estimate is two errors short. Each shard's
# minutes of tests are in the Go job's summary: change this number when they drift.
shards_default=16
package=./internal/desk
module=github.com/Judgment-Pack/judgment-pack-desk
helper=scripts/dev/go-test-shards.py

if [ "${1:-}" = --shards ]; then
  python3 -c 'import json, sys; print(json.dumps(list(range(1, int(sys.argv[1]) + 1))))' "$shards_default"
  exit 0
fi
[ "$#" -eq 0 ] || { echo "go-test.sh: unknown argument $1" >&2; exit 2; }

out=${JPDEV_LOGDIR:-$tmp/go-test}
mkdir -p "$out"

# show LOG: the whole go test log on a terminal; with JPDEV_LOGDIR set, where the log is kept and
# its package lines, so that check.sh's own log and summary stay short.
show() {
  if [ -n "${JPDEV_LOGDIR:-}" ]; then
    echo "go-test: the full log is $(basename "$1") in the step logs"
    grep -E '^(ok|FAIL|panic: )' "$1"
  else
    cat "$1"
  fi
}

# run_others: every package but internal/desk, as go-test ran them before the split. The go
# command's output goes to a file, not a pipe, so a process a test leaves behind cannot hold the
# step open after the test binary has ended.
run_others() {
  local list log="$out/go-test-others.log" code
  list=$(go list -buildvcs=false ./... | grep -vxF "$module/internal/desk") || return 2
  # shellcheck disable=SC2086
  go test -count=1 -timeout="${GO_TEST_TIMEOUT:-40m}" -v $list > "$log" 2>&1 < /dev/null
  code=$?
  show "$log"
  return "$code"
}

# run_shard K N: one shard of internal/desk. The kill bound follows go test's own -timeout panic
# by five minutes; that panic names the tests still running, which the summary repeats.
run_shard() {
  local k=$1 n=$2 list="$out/go-test-desk.list" log="$out/go-test-desk.log" pattern code
  local minutes=${GO_TEST_SHARD_MINUTES:-30}
  [[ "$minutes" =~ ^[1-9][0-9]*$ ]] || { echo "go-test.sh: GO_TEST_SHARD_MINUTES is a whole number" >&2; return 2; }
  if ! go test -list '.*' "$package" > "$list" 2>&1 < /dev/null; then
    cat "$list"
    echo "go-test: $package did not list its tests"
    return 2
  fi
  pattern=$(python3 "$helper" select "$k/$n" "$list") || return 2
  printf '{"shard": "%s/%s", "artifact": "%s"}\n' "$k" "$n" "${GO_TEST_ARTIFACT:-}" > "$out/go-test-shard.json"
  echo "go-test: $package shard $k/$n: $(tr '|' '\n' <<<"$pattern" | wc -l | tr -d ' ') of $(grep -cE '^(Test|Example|Fuzz)' "$list") tests"
  timeout -k 60 $((minutes * 60 + 300)) \
    go test -count=1 -timeout="${minutes}m" -v -run "$pattern" "$package" > "$log" 2>&1 < /dev/null
  code=$?
  show "$log"
  python3 "$helper" summary "$k/$n" "$list" "$log" | tee -a "${GITHUB_STEP_SUMMARY:-/dev/null}"
  return "$code"
}

status=0
case "${GO_TEST_SHARD:-}" in
  others)
    run_others || status=1
    ;;
  [1-9]*/[1-9]*)
    run_shard "${GO_TEST_SHARD%/*}" "${GO_TEST_SHARD#*/}" || status=1
    ;;
  "" | desk)
    [ -n "${GO_TEST_SHARD:-}" ] || run_others || status=1
    # Shard after shard, each with its own log directory, then the same completeness check
    # as CI's.
    base=$out
    dirs=()
    for k in $(seq 1 "$shards_default"); do
      out="$base/shard-$k"
      mkdir -p "$out"
      dirs+=("$out")
      run_shard "$k" "$shards_default" || status=1
    done
    python3 "$helper" complete "$shards_default" "${dirs[@]}" || status=1
    ;;
  *)
    echo "go-test.sh: GO_TEST_SHARD is others, desk or K/N, not $GO_TEST_SHARD" >&2
    exit 2
    ;;
esac
exit "$status"
