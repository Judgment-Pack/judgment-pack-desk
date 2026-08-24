#!/usr/bin/env bash
#
# The desk's acceptance proof: two real evaluations through the relay.
#
# It builds the chassis, points it at a throwaway copy of a Judgment Pack
# project, and drives the desk's own MCP client twice against the same pack —
# once with the project's full facts, once with one load-bearing fact removed.
# The pair is the point: the first run should resolve to an outcome, the second
# should escalate, and both dispositions come from the runtime over the wire the
# browser uses.
#
# Nothing is written to the project you name: it is copied first, because a
# completed evaluation appends an audit record in a project that declares one.
#
#   JPACK_PROJECT=/path/to/judgment-pack-quickstart scripts/acceptance.sh
#
# Environment:
#   JPACK_PROJECT  project directory to copy and evaluate (required)
#   JPACK_BIN      runtime binary                     (default: ./bin/jpack)
#   PORT           loopback port for the chassis      (default: 8799)
#   FACTS          facts document, relative to the project (default: full-facts.json)
#   EVIDENCE       evidence document, relative to the project (default: evidence.json)
#   MUTATE         jq expression removing a load-bearing fact from FACTS
#                  (default: the quickstart pack's /request/completeness)
#   EXPECT_KIND    disposition kind the full-facts run must produce (default: outcome)
#   EXPECT_DEGRADED_KIND  and the mutated run's                     (default: unresolved)
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
project="${JPACK_PROJECT:-}"
jpack="${JPACK_BIN:-$root/bin/jpack}"
port="${PORT:-8799}"
facts="${FACTS:-full-facts.json}"
evidence="${EVIDENCE:-evidence.json}"
mutate="${MUTATE:-del(.request.completeness)}"
expect_kind="${EXPECT_KIND:-outcome}"
expect_degraded_kind="${EXPECT_DEGRADED_KIND:-unresolved}"

die() { echo "acceptance: $*" >&2; exit 1; }

[ -n "$project" ] || die "set JPACK_PROJECT to a Judgment Pack project directory"
[ -f "$project/jpack.json" ] || die "JPACK_PROJECT=$project has no jpack.json"
[ -x "$jpack" ] || die "no runtime binary at $jpack — build one with: go build -C ../judgment-pack-runtime -o $jpack ./cmd/jpack"
command -v jq >/dev/null || die "jq is required to remove the load-bearing fact"
command -v node >/dev/null || die "node 22+ is required to run the smoke client"

work="$(mktemp -d)"
chassis=""
cleanup() {
  [ -n "$chassis" ] && kill "$chassis" 2>/dev/null || true
  rm -rf "$work"
}
trap cleanup EXIT

# The project is copied because a completed evaluation may append to an audit
# directory the project declares. An acceptance run must not write to the tree
# it was pointed at.
cp -R "$project" "$work/project"
[ -f "$work/project/$facts" ] || die "the project has no $facts"
jq "$mutate" "$work/project/$facts" > "$work/project/degraded-facts.json"

echo "== building the chassis =="
go build -C "$root" -o "$work/jpack-desk" .

echo "== starting the chassis on 127.0.0.1:$port =="
"$work/jpack-desk" --port "$port" --jpack "$jpack" "$work/project" >"$work/chassis.log" 2>&1 &
chassis=$!

# The tokened URL is the chassis' own first line of output; waiting for it is
# how this script learns the session token without fixing one.
url=""
for _ in $(seq 1 100); do
  if ! kill -0 "$chassis" 2>/dev/null; then
    cat "$work/chassis.log" >&2
    die "the chassis exited before it printed a URL"
  fi
  url="$(sed -n 's/^ *open: *//p' "$work/chassis.log" | head -1)"
  [ -n "$url" ] && break
  sleep 0.1
done
[ -n "$url" ] || { cat "$work/chassis.log" >&2; die "the chassis never printed a URL"; }
echo "   $url"

evidence_args=()
[ -f "$work/project/$evidence" ] && evidence_args=(--evidence "$work/project/$evidence")

echo
echo "== 1/2  full facts =="
npm --prefix "$root/web" --silent run smoke -- "$url" \
  --facts "$work/project/$facts" "${evidence_args[@]}" \
  --expect-kind "$expect_kind"

echo
echo "== 2/2  the same pack with one load-bearing fact removed: $mutate =="
npm --prefix "$root/web" --silent run smoke -- "$url" \
  --facts "$work/project/degraded-facts.json" "${evidence_args[@]}" \
  --expect-kind "$expect_degraded_kind" --expect-handoff requested

echo
echo "acceptance: both runs produced the disposition they were expected to."
