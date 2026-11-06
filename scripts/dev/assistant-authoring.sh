#!/usr/bin/env bash
# Actual host captures/API/native runs -> canonical browser protocol readers.
set -euo pipefail
root=${JPDEV_ROOT:-$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)}
tmp_root=${RUNNER_TEMP:-${TMPDIR:-/tmp}/desk-check}
mkdir -p "$tmp_root"
tmp=$(mktemp -d "$tmp_root/assistant-authoring.XXXXXX")
cleanup() {
 status=$?
 if [ "$status" -eq 0 ]; then rm -rf "$tmp"; else echo "Authoring failed observations retained at $tmp" >&2; fi
}
trap cleanup EXIT
mkdir -m 700 "$tmp/outputs"
(
 cd "$root"
 export JPS_AUTHORING_OUTPUTS="$tmp/outputs"
 command=(go test ./internal/desk -run '^TestAssistantAuthoring(ActualAPI|ActualNative)$' -count=1 -v)
 if [ -n "${JPDEV_WRAP:-}" ]; then command=("$JPDEV_WRAP" go -- "${command[@]}"); fi
 "${command[@]}"
) | tee "$tmp/producers.log"
for producer in ActualAPI ActualNative; do
 grep -Eq -- "^--- PASS: TestAssistantAuthoring$producer \\(" "$tmp/producers.log" || { echo "Authoring $producer producer did not pass" >&2; exit 1; }
done
node_command=(bash -c '
 set -euo pipefail
 "$1/web/node_modules/.bin/esbuild" "$1/scripts/assistant-authoring-reference.mjs" --bundle --platform=node --format=esm --outfile="$2/reference.mjs"
 node "$2/reference.mjs" "$2/outputs"
' assistant-authoring "$root" "$tmp")
if [ -n "${JPDEV_WRAP:-}" ]; then node_command=(flock "$JPDEV_LOCK" "$JPDEV_WRAP" node -- "${node_command[@]}"); fi
"${node_command[@]}"
