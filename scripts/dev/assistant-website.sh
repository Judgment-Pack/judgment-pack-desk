#!/usr/bin/env bash
# Actual private discovery/page producers and shared browser readers, offline.
set -euo pipefail
root=${JPDEV_ROOT:-$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)}
tmp_root=${RUNNER_TEMP:-${TMPDIR:-/tmp}/desk-check}
mkdir -p "$tmp_root"
tmp=$(mktemp -d "$tmp_root/assistant-website.XXXXXX")
trap 'rm -rf "$tmp"' EXIT
if [ -z "${JPS_TEST_GATEWAY:-}" ] && [ -z "${JPS_TEST_GATEWAY_BUNDLE:-}" ]; then
 . "$root/scripts/dev/gateway-fixture.sh"
 prepare_gateway_fixture
fi
export JPS_TEST_PYTHON="${JPS_TEST_PYTHON:-$(command -v python3)}"
outputs="$tmp/outputs"
mkdir -m 700 "$outputs"
(
 cd "$root"
 export DESK_WEBSITE_READER_OUTPUTS="$outputs"
 command=(go test ./internal/desk -run '^TestAssistantWebsite(ActualProducers|ActualRefusalEvents|SharedVectors)$' -count=1 -v)
 if [ -n "${JPDEV_WRAP:-}" ]; then command=("$JPDEV_WRAP" go -- "${command[@]}"); fi
 "${command[@]}"
) | tee "$tmp/producers.log"
for producer in ActualProducers ActualRefusalEvents SharedVectors; do
 grep -Eq -- "^--- PASS: TestAssistantWebsite$producer \\(" "$tmp/producers.log" || { echo "Website $producer producer did not pass" >&2; exit 1; }
done
"$root/web/node_modules/.bin/esbuild" "$root/scripts/assistant-website-reference.mjs" \
 --bundle --platform=node --format=esm \
 --inject:"$root/scripts/assistant-chat-projection-reference-locale.mjs" \
 --define:import.meta.glob=offlineLocaleCatalogues --outfile="$tmp/reference.mjs"
node "$tmp/reference.mjs" "$outputs"
if [ -n "${JPS_TEST_AGENT_SCHEMAS:-}" ]; then
 python3 "$root/scripts/dev/assistant-website-schema.py" "$JPS_TEST_AGENT_SCHEMAS" "$outputs"
fi
