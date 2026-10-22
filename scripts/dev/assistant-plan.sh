#!/usr/bin/env bash
# Actual private Plan producers and existing canonical/browser readers, offline.
# shellcheck shell=bash
set -euo pipefail
root=${JPDEV_ROOT:-$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)}
tmp_root=${RUNNER_TEMP:-${TMPDIR:-/tmp}/desk-check}
mkdir -p "$tmp_root"
tmp=$(mktemp -d "$tmp_root/assistant-plan.XXXXXX")
trap 'rm -rf "$tmp"' EXIT
outputs="$tmp/outputs"
mkdir -m 700 "$outputs"
(
 cd "$root"
 export DESK_PLAN_READER_OUTPUTS="$outputs"
 command=(go test ./internal/desk -run '^TestAssistantPlan(ActualProducersAndProjection|ActualRefusalEvents|RecoveryCustodyMembers|SharedVectors)$' -count=1 -v)
 if [ -n "${JPDEV_WRAP:-}" ]; then command=("$JPDEV_WRAP" go -- "${command[@]}"); fi
 "${command[@]}"
) | tee "$tmp/producers.log"
for producer in ActualProducersAndProjection ActualRefusalEvents RecoveryCustodyMembers SharedVectors; do
 grep -Eq -- "^--- PASS: TestAssistantPlan$producer \\(" "$tmp/producers.log" || { echo "Plan $producer producer did not pass" >&2; exit 1; }
done
"$root/web/node_modules/.bin/esbuild" "$root/scripts/assistant-plan-reference.mjs" \
 --bundle --platform=node --format=esm --loader:.css=empty \
 --banner:js='import {createRequire} from "node:module";const require=createRequire(import.meta.url);' \
 --inject:"$root/scripts/assistant-chat-projection-reference-locale.mjs" \
 --define:import.meta.glob=offlineLocaleCatalogues --outfile="$tmp/reference.mjs"
node "$tmp/reference.mjs" "$outputs"
if [ -n "${JPS_TEST_AGENT_SCHEMAS:-}" ]; then
 python3 "$root/scripts/dev/assistant-plan-schema.py" "$JPS_TEST_AGENT_SCHEMAS" "$outputs"
fi
