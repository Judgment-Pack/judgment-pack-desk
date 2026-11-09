#!/usr/bin/env bash
# Required memory producer/consumer acceptance; phases allow separate resource caps.
# shellcheck shell=bash
set -euo pipefail

root=${JPDEV_ROOT:-$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)}
if [ "$#" -ne 2 ] || [[ "$1" != produce && "$1" != consume ]]; then
  echo 'usage: assistant-memory-projection.sh produce|consume OUTPUT_DIRECTORY' >&2
  exit 2
fi
phase=$1
outputs=$2
if [ "$phase" = produce ]; then
  # A new directory prevents old observations from satisfying a skipped producer.
  mkdir -m 700 "$outputs"
  outputs=$(cd "$outputs" && pwd)
  go_env=()
  if [ -n "${DESK_GO_CACHE:-}" ]; then go_env+=("GOCACHE=$DESK_GO_CACHE"); fi
  if [ -n "${DESK_GO_MODULES:-}" ]; then go_env+=("GOMODCACHE=$DESK_GO_MODULES"); fi
  (
    cd "$root"
    env "${go_env[@]}" JPS_MEMORY_OUTPUTS="$outputs" \
      go test ./internal/desk -run '^TestAssistantMemoryProposalActual$/(work|plan)-(false|true)-true$' -count=1 -v
  ) | tee "$outputs/producer.log"
  for native in false true; do
    for mode in work plan; do
      name="TestAssistantMemoryProposalActual/$mode-$native-true"
      awk -v want="--- PASS: $name (" '{ sub(/^[ \t]+/, ""); if (index($0, want) == 1) count++ } END { exit count != 1 }' "$outputs/producer.log" || {
        echo "Memory producer $name did not run and pass exactly once" >&2
        exit 1
      }
      test -s "$outputs/$mode-$native-true.json"
    done
  done
else
  outputs=$(cd "$outputs" && pwd)
  (
    cd "$root/web"
    JPS_REQUIRE_MEMORY_OUTPUTS=1 JPS_MEMORY_OUTPUTS="$outputs" \
      ./node_modules/.bin/vitest run src/assistant/memory/proposals.test.tsx \
      --reporter=json --outputFile="$outputs/consumer.json"
  )
  node --input-type=module - "$outputs/consumer.json" <<'NODE'
import assert from 'node:assert/strict'
import {readFileSync} from 'node:fs'
const report=JSON.parse(readFileSync(process.argv[2],'utf8'))
const cases=report.testResults.flatMap(result=>result.assertionResults)
const actual=cases.filter(test=>test.title.startsWith('consumes actual '))
const expected=['API','native'].flatMap(provider=>['work','plan'].map(phase=>`consumes actual ${provider} ${phase} proposals through the default review UI`))
assert.equal(report.success,true)
assert.deepEqual(actual.map(test=>test.title).sort(),expected.sort())
assert.ok(actual.every(test=>test.status==='passed'),'All four actual memory UI cases must execute and pass; skips are refused')
console.log('Memory shared review UI: 4 actual API/native Work/Plan cases passed; 0 skipped.')
NODE
fi
