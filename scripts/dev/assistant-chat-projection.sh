#!/usr/bin/env bash
# Produce fresh hand-authored Go observations and check both shared browser readers offline.
# shellcheck shell=bash
set -euo pipefail

root=${JPDEV_ROOT:-$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)}
tmp_root=${RUNNER_TEMP:-${TMPDIR:-/tmp}/desk-check}
mkdir -p "$tmp_root"
tmp=$(mktemp -d "$tmp_root/assistant-chat-projection.XXXXXX")
trap 'rm -rf "$tmp"' EXIT

chat_outputs="$tmp/chat"
unicode_outputs="$tmp/unicode"
mkdir -m 700 "$chat_outputs" "$unicode_outputs"
chat_log="$tmp/chat-producers.log"
unicode_log="$tmp/unicode-producers.log"
go_env=()
if [ -n "${DESK_GO_CACHE:-}" ]; then go_env+=("GOCACHE=$DESK_GO_CACHE"); fi
if [ -n "${DESK_GO_MODULES:-}" ]; then go_env+=("GOMODCACHE=$DESK_GO_MODULES"); fi

(
  cd "$root"
  env "${go_env[@]}" JPS_TEST_CHAT_PROJECTION_OUTPUTS="$chat_outputs" \
    go test ./internal/desk -run '^(TestAssistantChatProjection(PrepareWriteReadbackAck|WorkFailureAndCreate|PausesStayPinned))$' -count=1 -v
) | tee "$chat_log"
(
  cd "$root"
  env "${go_env[@]}" JPS_TEST_PROJECTION_UNICODE_OUTPUTS="$unicode_outputs" \
    go test ./internal/desk -run '^TestAssistantChatProjection(UnicodeRefusal|ValidUnicode)$' -count=1 -v
) | tee "$unicode_log"

awk -v want='--- PASS: ' '{ sub(/^[ \t]+/, ""); if (index($0, want) == 1 && $0 ~ /TestAssistantChatProjection(PrepareWriteReadbackAck|WorkFailureAndCreate|PausesStayPinned) \(/) found[$3]=1 } END { for (n in found) count++; exit count != 3 }' "$chat_log" || {
  echo 'chat projection producer tests did not all run and pass' >&2
  exit 1
}
awk -v want='--- PASS: ' '{ sub(/^[ \t]+/, ""); if (index($0, want) == 1 && $0 ~ /TestAssistantChatProjection(UnicodeRefusal|ValidUnicode) \(/) found[$3]=1 } END { for (n in found) count++; exit count != 2 }' "$unicode_log" || {
  echo 'Unicode projection producer tests did not all run and pass' >&2
  exit 1
}

node_bin=$(command -v node)
esbuild="$root/web/node_modules/.bin/esbuild"
test -x "$node_bin"
test -x "$esbuild"
"$esbuild" "$root/scripts/assistant-chat-projection-reference.mjs" \
  --bundle --platform=node --format=esm \
  --inject:"$root/scripts/assistant-chat-projection-reference-locale.mjs" \
  --define:import.meta.glob=offlineLocaleCatalogues \
  --outfile="$tmp/chat-reference.mjs"
"$node_bin" "$tmp/chat-reference.mjs" "$chat_outputs" | tee "$tmp/chat-reader.log"
grep -Fxq 'Shared chat readers: 6 completed, work/failure, create and pinned-pause fixtures passed.' "$tmp/chat-reader.log"
"$esbuild" "$root/scripts/assistant-chat-projection-unicode-reference.mjs" \
  --bundle --platform=node --format=esm \
  --inject:"$root/scripts/assistant-chat-projection-reference-locale.mjs" \
  --define:import.meta.glob=offlineLocaleCatalogues \
  --outfile="$tmp/unicode-reference.mjs"
"$node_bin" "$tmp/unicode-reference.mjs" "$unicode_outputs" | tee "$tmp/unicode-reader.log"
grep -Fxq 'Unicode positive: 6 actual reader observations passed.' "$tmp/unicode-reader.log"

# Case-sensitive history has its own fresh observations; do not mix the two
# original export sets with workspace issue-case and backup/recovery coverage.
history_outputs="$tmp/case-sensitive-history"
mkdir -m 700 "$history_outputs"
history_log="$tmp/case-sensitive-history-producers.log"
(
  cd "$root"
  env "${go_env[@]}" JPS_TEST_CASE_SENSITIVE_HISTORY_OUTPUTS="$history_outputs" \
    go test ./internal/desk -run '^(TestAssistantChatProjectionCaseSensitiveDataRecoveryImport|TestCaseSensitiveHistoryExpectationIssues)$' -count=1 -v
) | tee "$history_log"
for producer in TestAssistantChatProjectionCaseSensitiveDataRecoveryImport TestCaseSensitiveHistoryExpectationIssues; do
  awk -v want="--- PASS: $producer (" '{ sub(/^[ \t]+/, ""); if (index($0, want) == 1) found=1 } END { exit !found }' "$history_log" || {
    echo "$producer did not run and pass" >&2
    exit 1
  }
done
"$esbuild" "$root/scripts/case-sensitive-history-reference.mjs" \
  --bundle --platform=node --format=esm \
  --inject:"$root/scripts/assistant-chat-projection-reference-locale.mjs" \
  --define:import.meta.glob=offlineLocaleCatalogues \
  --outfile="$tmp/case-sensitive-history-reference.mjs"
"$node_bin" "$tmp/case-sensitive-history-reference.mjs" "$history_outputs" | tee "$tmp/case-sensitive-history-reader.log"
grep -Fxq 'Case-sensitive history: 4 actual reader observations passed.' "$tmp/case-sensitive-history-reader.log"
