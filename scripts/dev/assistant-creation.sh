#!/usr/bin/env bash
set -euo pipefail
root=${JPDEV_ROOT:-$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)}
source "$root/scripts/dev/dialect-mapping-fixture.sh"
prepare_dialect_mapping_fixture
tmp_root=${RUNNER_TEMP:-${TMPDIR:-/tmp}/desk-check}
mkdir -p "$tmp_root"
tmp=$(mktemp -d "$tmp_root/assistant-creation.XXXXXX")
cleanup(){ status=$?;if [ "$status" -eq 0 ];then rm -rf "$tmp";else echo "Creation observations retained at $tmp" >&2;fi; }
trap cleanup EXIT
mkdir -m 700 "$tmp/outputs"
(
 cd "$root"
 export JPS_CREATION_OUTPUTS="$tmp/outputs"
 command=(go test ./internal/desk -run '^TestAssistantCreation(ActualAPI|ActualNative|InterruptedValidationAndRetest)$' -count=1 -v)
 if [ -n "${JPDEV_WRAP:-}" ];then command=("$JPDEV_WRAP" go -- "${command[@]}");fi
 "${command[@]}"
) | tee "$tmp/producers.log"
for producer in ActualAPI ActualNative InterruptedValidationAndRetest;do
 grep -Eq -- "^--- PASS: TestAssistantCreation$producer \\(" "$tmp/producers.log" || { echo "Creation $producer producer did not pass" >&2;exit 1; }
done
node_command=(bash -c '
 set -euo pipefail
 "$1/web/node_modules/.bin/esbuild" "$1/scripts/assistant-creation-reference.mjs" --bundle --platform=node --format=esm --outfile="$2/reference.mjs"
 node "$2/reference.mjs" "$2/outputs"
 cd "$1/web"
 JPS_CREATION_OUTPUTS="$2/outputs" ./node_modules/.bin/vitest run src/routes/CreationRoutes.test.tsx
' assistant-creation "$root" "$tmp")
if [ -n "${JPDEV_WRAP:-}" ];then node_command=(flock "$JPDEV_LOCK" "$JPDEV_WRAP" node -- "${node_command[@]}");fi
"${node_command[@]}"
