#!/usr/bin/env bash
# Real native bridge producer, rooted Runtime and canonical browser readers.
set -euo pipefail
root=${JPDEV_ROOT:-$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)}
source "$root/scripts/dev/dialect-mapping-fixture.sh"
prepare_dialect_mapping_fixture
tmp_root=${RUNNER_TEMP:-${TMPDIR:-/tmp}/desk-check}
mkdir -p "$tmp_root"
tmp=$(mktemp -d "$tmp_root/assistant-diagnostic.XXXXXX")
cleanup(){ status=$?;if [ "$status" -eq 0 ];then rm -rf "$tmp";else echo "Diagnostic observations retained at $tmp" >&2;fi; }
trap cleanup EXIT
mkdir -m 700 "$tmp/outputs"
(
 cd "$root"
 export DESK_RUN_CLIENT_OUTPUTS="$tmp/outputs"
 command=(go test ./internal/desk ./internal/codexbridge -run '^(TestAssistantDiagnostic|TestAccountBinding)' -count=1 -v)
 if [ -n "${JPDEV_WRAP:-}" ]; then command=("$JPDEV_WRAP" go -- "${command[@]}"); fi
 "${command[@]}"
) | tee "$tmp/producers.log"
grep -Eq -- '^--- PASS: TestAssistantDiagnosticActualNativeCheckpoint \(' "$tmp/producers.log" || { echo "Native diagnostic producer did not pass" >&2;exit 1; }
node_command=(bash -c '
 set -euo pipefail
 "$1/web/node_modules/.bin/esbuild" "$1/scripts/assistant-diagnostic-reference.mjs" --bundle --platform=node --format=esm --outfile="$2/reference.mjs"
 node "$2/reference.mjs" "$2/outputs"
' assistant-diagnostic "$root" "$tmp")
if [ -n "${JPDEV_WRAP:-}" ]; then node_command=(flock "$JPDEV_LOCK" "$JPDEV_WRAP" node -- "${node_command[@]}"); fi
"${node_command[@]}"
