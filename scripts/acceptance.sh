#!/usr/bin/env bash
#
# The desk's acceptance proof: two real evaluations through the relay, then the
# project's own declared rows.
#
# It builds the chassis, points it at a throwaway copy of a Judgment Pack
# project, and drives the desk's own MCP client twice against the same pack —
# once with the project's full facts, once with one load-bearing fact removed.
# The pair is the point: the first run should resolve to an outcome, the second
# should escalate, and both dispositions come from the runtime over the wire the
# browser uses.
#
# It then runs what the project itself declares: every pack matrix, and every
# configured graph matrix, through the two tools the matrix and graph views
# call. Those runs answer a different question from the evaluations. An
# evaluation asks what this pack does with these facts; a matrix asks whether
# what the project wrote about its own packs still holds, and its coverage
# report asks how much of each pack those rows are about at all.
#
# Nothing is written to the project you name: it is copied first, because a
# completed evaluation appends an audit record in a project that declares one.
# The matrix runs need no such care — a row is a rehearsal and writes nothing —
# but they run against the same copy anyway, so one run means one project.
#
#   JPACK_PROJECT=/path/to/judgment-pack-quickstart scripts/acceptance.sh
#
# Environment:
#   JPACK_PROJECT  project directory to copy and evaluate (required)
#   JPACK_BIN      runtime binary                     (default: ./bin/jpack)
#   PORT           loopback port for the chassis      (default: 8799)
#   FACTS          facts document, relative to the project (default: full-facts.json)
#   EVIDENCE       evidence document, relative to the project (default: evidence.json)
#   PACK           decision id to evaluate; defaults to the project's first, which
#                  is only the right one where FACTS suits it
#   MUTATE         jq expression removing a load-bearing fact from FACTS
#                  (default: the quickstart pack's /request/completeness)
#   EXPECT_KIND    disposition kind the full-facts run must produce (default: outcome)
#   EXPECT_DEGRADED_KIND  and the mutated run's                     (default: unresolved)
#   EXPECT_MATRIX_STATUS  status the pack matrices must report      (default: passed)
#   EXPECT_GRAPH_STATUS   status the graph matrices must report; a project that
#                         configures none reports skipped, so this defaults to
#                         empty and is only checked when you set it
#   GRAPH_DOCUMENT        a configured graph id to fetch through
#                         experimental_get_graph (ADR-0029), checking the served
#                         text against its own metadata; unset by default,
#                         because a runtime without that tool would refuse it
#   GRAPH_FILE            that graph's document, relative to the project, to
#                         compare the served text against byte for byte
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
expect_matrix_status="${EXPECT_MATRIX_STATUS:-passed}"
expect_graph_status="${EXPECT_GRAPH_STATUS:-}"
graph_document="${GRAPH_DOCUMENT:-}"
graph_file="${GRAPH_FILE:-}"

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

echo "== building the web assets =="
npm --prefix "$root/web" --silent run build

echo "== building the chassis (embedding the built assets) =="
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
pack_args=()
[ -n "${PACK:-}" ] && pack_args=(--pack "$PACK")

echo
echo "== 1/3  full facts =="
npm --prefix "$root/web" --silent run smoke -- "$url" \
  --facts "$work/project/$facts" "${evidence_args[@]}" "${pack_args[@]}" \
  --expect-kind "$expect_kind"

echo
echo "== 2/3  the same pack with one load-bearing fact removed: $mutate =="
npm --prefix "$root/web" --silent run smoke -- "$url" \
  --facts "$work/project/degraded-facts.json" "${evidence_args[@]}" "${pack_args[@]}" \
  --expect-kind "$expect_degraded_kind" --expect-handoff requested

echo
echo "== 3/3  the rows the project declares about itself =="
graph_args=(--graphs)
[ -n "$expect_graph_status" ] && graph_args=(--expect-graph-status "$expect_graph_status")
# Opt-in, because a runtime that predates ADR-0029 advertises neither tool and
# the smoke script refuses the step rather than passing it quietly.
[ -n "$graph_document" ] && graph_args+=(--graph-document "$graph_document")
[ -n "$graph_file" ] && graph_args+=(--graph-file "$work/project/$graph_file")
npm --prefix "$root/web" --silent run smoke -- "$url" \
  --expect-matrix-status "$expect_matrix_status" "${graph_args[@]}"

echo
echo "acceptance: both evaluations produced the disposition they were expected to,"
echo "            and the project's declared rows ran with their coverage reported."
