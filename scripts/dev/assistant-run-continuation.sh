#!/usr/bin/env bash
# Actual continuation producers and production browser readers; no activation.
# shellcheck shell=bash
set -euo pipefail
root=${JPDEV_ROOT:-$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)}
tmp_root=${RUNNER_TEMP:-${TMPDIR:-/tmp}/desk-check}
mkdir -p "$tmp_root"
tmp=$(mktemp -d "$tmp_root/assistant-run-continuation.XXXXXX")
trap 'rm -rf "$tmp"' EXIT
mkdir -m 700 "$tmp/outputs"
(
 cd "$root"
 export DESK_RUN_CLIENT_OUTPUTS="$tmp/outputs"
 command=(go test ./internal/desk -run '^TestAssistantRunContinuation' -count=1 -v)
 if [ -n "${JPDEV_WRAP:-}" ]; then command=("$JPDEV_WRAP" go -- "${command[@]}"); fi
 "${command[@]}"
) | tee "$tmp/producers.log"
for producer in ActualEino ActualSubscription AnswerAndBudgetOnce SharedVectors CheckpointCustody PersistedCrashNeverReplays; do
 grep -Eq -- "^--- PASS: TestAssistantRunContinuation$producer \\(" "$tmp/producers.log" || { echo "Run continuation $producer producer did not pass" >&2; exit 1; }
done
python3 "$root/scripts/assistant-run-continuation-mutations.py"
node_command=(bash -c '
 set -euo pipefail
 "$1/web/node_modules/.bin/esbuild" "$1/scripts/assistant-run-continuation-reference.mjs" --bundle --platform=node --format=esm --outfile="$2/reference.mjs"
 node "$2/reference.mjs" "$2/outputs"
 node "$1/scripts/assistant-run-client-mutations.mjs"
' assistant-run-continuation "$root" "$tmp")
if [ -n "${JPDEV_WRAP:-}" ]; then node_command=(flock "$JPDEV_LOCK" "$JPDEV_WRAP" node -- "${node_command[@]}"); fi
"${node_command[@]}"
