#!/usr/bin/env bash
# Real API/native adapter observations through the registered SSE and browser reader.
# shellcheck shell=bash
set -euo pipefail
root=${JPDEV_ROOT:-$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)}
tmp_root=${RUNNER_TEMP:-${TMPDIR:-/tmp}/desk-check}
mkdir -p "$tmp_root"
tmp=$(mktemp -d "$tmp_root/assistant-run-progress.XXXXXX")
trap 'rm -rf "$tmp"' EXIT
mkdir -m 700 "$tmp/outputs"
(
 cd "$root"
 export DESK_RUN_CLIENT_OUTPUTS="$tmp/outputs"
 command=(go test ./internal/desk -run '^TestAssistantRunProgressActualAPIAndNativeSSE$' -count=1 -v)
 if [ -n "${JPDEV_WRAP:-}" ]; then command=("$JPDEV_WRAP" go -- "${command[@]}"); fi
 "${command[@]}"
) | tee "$tmp/producers.log"
grep -Eq -- '^--- PASS: TestAssistantRunProgressActualAPIAndNativeSSE \(' "$tmp/producers.log" || { echo 'Run progress producer did not pass' >&2; exit 1; }
node_command=(bash -c '
 set -euo pipefail
 "$1/web/node_modules/.bin/esbuild" "$1/scripts/assistant-run-progress-reference.mjs" \
  --bundle --platform=node --format=esm --outfile="$2/reference.mjs"
 node "$2/reference.mjs" "$2/outputs"
' assistant-run-progress "$root" "$tmp")
if [ -n "${JPDEV_WRAP:-}" ]; then
 node_command=(flock "$JPDEV_LOCK" "$JPDEV_WRAP" node -- "${node_command[@]}")
fi
"${node_command[@]}"
