#!/usr/bin/env bash
# Connect actual guarded brief producers to the browser's canonical consumers.
set -euo pipefail
root=${JPDEV_ROOT:-$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)}
tmp_root=${RUNNER_TEMP:-${TMPDIR:-/tmp}/desk-check}
mkdir -p "$tmp_root"
tmp=$(mktemp -d "$tmp_root/assistant-brief.XXXXXX")
trap 'rm -rf "$tmp"' EXIT
mkdir -m 700 "$tmp/outputs"
(
 cd "$root"
 export JPS_BRIEF_OUTPUTS="$tmp/outputs"
 command=(go test ./internal/desk -run '^TestAssistantBrief(OutputContract|SubjectBounds|VerifiedSourceProjection|PublishedAdapter|ActualAPI|ActualInvalidOutput|ActualNative|ActualRunnerSubjects|ReservationRefusals|CommitRechecksAndCancellation|StopAndInterruptedNoReplay)$' -count=1 -v)
 if [ -n "${JPDEV_WRAP:-}" ]; then command=("$JPDEV_WRAP" go -- "${command[@]}"); fi
 "${command[@]}"
) | tee "$tmp/producers.log"
node_command=(bash -c '
 set -euo pipefail
 "$1/web/node_modules/.bin/esbuild" "$1/scripts/assistant-brief-reference.mjs" --bundle --platform=node --format=esm --outfile="$2/reference.mjs"
 node "$2/reference.mjs" "$2/outputs"
' assistant-brief "$root" "$tmp")
if [ -n "${JPDEV_WRAP:-}" ]; then node_command=(flock "$JPDEV_LOCK" "$JPDEV_WRAP" node -- "${node_command[@]}"); fi
"${node_command[@]}"
