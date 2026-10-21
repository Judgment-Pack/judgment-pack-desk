#!/usr/bin/env bash
# Produce fresh hand-authored Go observations and check shared browser readers offline.
# shellcheck shell=bash
set -euo pipefail

root=${JPDEV_ROOT:-$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)}
tmp_root=${RUNNER_TEMP:-${TMPDIR:-/tmp}/desk-check}
mkdir -p "$tmp_root"
tmp=$(mktemp -d "$tmp_root/assistant-chat-projection.XXXXXX")
trap 'rm -rf "$tmp"' EXIT

chat_outputs="$tmp/chat"
unicode_outputs="$tmp/unicode"
graph_outputs="$tmp/graph"
policy_outputs="$tmp/policy"
mkdir -m 700 "$chat_outputs" "$unicode_outputs" "$graph_outputs" "$policy_outputs"
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

(
  cd "$root"
  env "${go_env[@]}" DESK_GRAPH_READER_OUTPUT="$graph_outputs/chat.json" \
    DESK_GRAPH_DETACHED_OUTPUT="$graph_outputs/detached.json" \
    go test ./internal/desk -run '^TestAssistantGraphProducerDurableReadAndProjection$' -count=1 -v
) | tee "$tmp/graph-producer.log"
grep -Eq '^--- PASS: TestAssistantGraphProducerDurableReadAndProjection \(' "$tmp/graph-producer.log" || {
  echo 'Graph projection producer test did not run and pass' >&2
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

"$esbuild" "$root/scripts/assistant-graph-projection-reference.mjs" \
  --bundle --platform=node --format=esm \
  --inject:"$root/scripts/assistant-chat-projection-reference-locale.mjs" \
  --define:import.meta.glob=offlineLocaleCatalogues \
  --outfile="$tmp/graph-reference.mjs"
"$node_bin" "$tmp/graph-reference.mjs" "$graph_outputs/chat.json" "$graph_outputs/detached.json" | tee "$tmp/graph-reader.log"
grep -Fxq 'Graph shared readers: exact producer draft and detached deletion retention passed.' "$tmp/graph-reader.log"

# Policy cards use actual six-tool API production plus a retained stale update.
# Fresh files and named producers prevent a skipped/empty run passing readback.
(
  cd "$root"
  env "${go_env[@]}" DESK_POLICY_READER_OUTPUT="$policy_outputs/passed.json" \
    DESK_POLICY_STALE_READER_OUTPUT="$policy_outputs/stale.json" \
    go test ./internal/desk -run '^TestAssistantPolicy(ActualAPIProducerAndProjection|PlanChildAndStaleBasis)$' -count=1 -v
) | tee "$tmp/policy-producer.log"
for producer in TestAssistantPolicyActualAPIProducerAndProjection TestAssistantPolicyPlanChildAndStaleBasis; do
  grep -Eq "^--- PASS: $producer \\(" "$tmp/policy-producer.log" || {
    echo "$producer did not run and pass" >&2
    exit 1
  }
done
"$esbuild" "$root/scripts/assistant-policy-projection-reference.mjs" \
  --bundle --platform=node --format=esm \
  --inject:"$root/scripts/assistant-chat-projection-reference-locale.mjs" \
  --define:import.meta.glob=offlineLocaleCatalogues \
  --outfile="$tmp/policy-reference.mjs"
"$node_bin" "$tmp/policy-reference.mjs" "$policy_outputs/passed.json" "$policy_outputs/stale.json" | tee "$tmp/policy-reader.log"
grep -Fxq 'Policy shared readers: exact passed/stale update and pack/graph handoff cards passed.' "$tmp/policy-reader.log"

# Test-design output is a review-only suite record plus a verified chat link.
# Scripted Runtime fixtures prove Desk custody; released Runtime support is #582.
test_outputs="$tmp/test-design"
mkdir -m 700 "$test_outputs"
(
  cd "$root"
  env "${go_env[@]}" DESK_TEST_HOST_CHAT_OUTPUT="$test_outputs/chat.json" \
    DESK_TEST_HOST_SUITE_OUTPUT="$test_outputs/tests.json" \
    DESK_TEST_HOST_DRAFT_CHAT_OUTPUT="$test_outputs/draft-chat.json" \
    DESK_TEST_HOST_DRAFT_SUITE_OUTPUT="$test_outputs/draft-tests.json" \
    DESK_TEST_HOST_DRAFTS_OUTPUT="$test_outputs/drafts.json" \
    go test ./internal/desk -run '^TestAssistantTestHost(ActualAPIAndReaders|SavedAndDraftCustody)$' -count=1 -v
) | tee "$tmp/test-design-producers.log"
for producer in TestAssistantTestHostActualAPIAndReaders TestAssistantTestHostSavedAndDraftCustody; do
  grep -Eq "^--- PASS: $producer \\(" "$tmp/test-design-producers.log" || {
    echo "$producer did not run and pass" >&2
    exit 1
  }
done
NODE_PATH="$root/web/node_modules" "$esbuild" "$root/scripts/assistant-test-projection-reference.mjs" \
  --bundle --platform=node --format=esm --jsx=automatic \
  --inject:"$root/scripts/assistant-chat-projection-reference-locale.mjs" \
  --define:import.meta.glob=offlineLocaleCatalogues \
  --banner:js='import {createRequire} from "node:module"; const require=createRequire(import.meta.url);' \
  --outfile="$tmp/test-design-reference.mjs"
"$node_bin" "$tmp/test-design-reference.mjs" "$test_outputs/chat.json" "$test_outputs/tests.json" \
  "$test_outputs/draft-chat.json" "$test_outputs/draft-tests.json" "$test_outputs/drafts.json" | tee "$tmp/test-design-reader.log"
grep -Fxq 'Test design shared readers: exact saved/draft proposals, existing-case review/stale guards and linked review actions passed (scripted Runtime; #582 remains blocked).' "$tmp/test-design-reader.log"
# Recipe proposals retain a real independent draft before the shared chat write.
recipe_outputs="$tmp/dialect-recipe"
mkdir -m 700 "$recipe_outputs"
(
  cd "$root"
  env "${go_env[@]}" DESK_DIALECT_RECIPE_READER_CHAT="$recipe_outputs/chat.json" \
    DESK_DIALECT_RECIPE_READER_DRAFT="$recipe_outputs/draft.json" \
    go test ./internal/desk -run '^TestAssistantDialectRecipeActualAPIProducer$' -count=1 -v
) | tee "$tmp/recipe-producer.log"
grep -Eq '^--- PASS: TestAssistantDialectRecipeActualAPIProducer \(' "$tmp/recipe-producer.log" || {
  echo 'Dialect recipe producer test did not run and pass' >&2
  exit 1
}
"$esbuild" "$root/scripts/assistant-dialect-recipe-reference.mjs" \
  --bundle --platform=node --format=esm \
  --inject:"$root/scripts/assistant-chat-projection-reference-locale.mjs" \
  --define:import.meta.glob=offlineLocaleCatalogues \
  --outfile="$tmp/recipe-reference.mjs"
"$node_bin" "$tmp/recipe-reference.mjs" "$recipe_outputs/chat.json" "$recipe_outputs/draft.json" | tee "$tmp/recipe-reader.log"
grep -Fxq 'Dialect recipe shared readers: exact typed proposal and independent draft readback passed.' "$tmp/recipe-reader.log"

# Mapping/publication output is check evidence only. Actual publication remains
# a separate reviewed write; the producer uses the pinned offline Runtime.
dialect_outputs="$tmp/dialect-mapping-publication"
mkdir -m 700 "$dialect_outputs"
(
  cd "$root"
  producer=(scripts/dev/jobs-companion.sh dialect-mapping-host)
  if [ "${DESK_DIALECT_MAPPING_RUNTIME+x}" = x ] || [ -n "${JPACK_BIN:-}" ]; then
    producer=(go test ./internal/desk -run '^TestAssistantDialectMappingActualAPIProducer$' -count=1 -v)
  fi
  env "${go_env[@]}" DESK_DIALECT_MAPPING_READER_CHAT="$dialect_outputs/chat.json" \
    DESK_DIALECT_MAPPING_READER_SOURCE="$dialect_outputs/source.json" \
    DESK_DIALECT_MAPPING_READER_REVIEW="$dialect_outputs/review.json" \
    "${producer[@]}"
) | tee "$tmp/dialect-mapping-producer.log"
grep -Eq '^--- PASS: TestAssistantDialectMappingActualAPIProducer \(' "$tmp/dialect-mapping-producer.log" || {
  echo 'Dialect mapping/publication producer did not run and pass' >&2
  exit 1
}
"$esbuild" "$root/scripts/assistant-dialect-mapping-reference.mjs" \
  --bundle --platform=node --format=esm \
  --inject:"$root/scripts/assistant-chat-projection-reference-locale.mjs" \
  --define:import.meta.glob=offlineLocaleCatalogues \
  --outfile="$tmp/dialect-mapping-reference.mjs"
"$node_bin" "$tmp/dialect-mapping-reference.mjs" "$dialect_outputs/chat.json" \
  "$dialect_outputs/source.json" "$dialect_outputs/review.json" | tee "$tmp/dialect-mapping-reader.log"
grep -Fxq 'Dialect mapping/publication shared readers: 2 exact checked review actions passed.' "$tmp/dialect-mapping-reader.log"

# Records/activity and six finite action kinds are review-only observations.
record_outputs="$tmp/dialect-record-actions"
mkdir -m 700 "$record_outputs"
(
  cd "$root"
  env "${go_env[@]}" DESK_DIALECT_RECORD_READER_CHAT="$record_outputs/chat.json" \
    DESK_DIALECT_RECORD_READER_SOURCE="$record_outputs/source.json" \
    DESK_DIALECT_RECORD_READER_RECORDS="$record_outputs/records.json" \
    DESK_DIALECT_RECORD_READER_ACTIVITY="$record_outputs/activity.json" \
    go test ./internal/desk -run '^TestAssistantDialectRecordActualAPIProducer$' -count=1 -v
) | tee "$tmp/dialect-record-producer.log"
grep -Eq '^--- PASS: TestAssistantDialectRecordActualAPIProducer \(' "$tmp/dialect-record-producer.log" || {
  echo 'Dialect records/actions producer did not run and pass' >&2
  exit 1
}
"$esbuild" "$root/scripts/assistant-dialect-record-reference.mjs" \
  --bundle --platform=node --format=esm \
  --inject:"$root/scripts/assistant-chat-projection-reference-locale.mjs" \
  --define:import.meta.glob=offlineLocaleCatalogues \
  --outfile="$tmp/dialect-record-reference.mjs"
"$node_bin" "$tmp/dialect-record-reference.mjs" "$record_outputs/chat.json" \
  "$record_outputs/source.json" "$record_outputs/records.json" "$record_outputs/activity.json" | tee "$tmp/dialect-record-reader.log"
grep -Fxq 'Dialect records/actions shared readers: bounded original values, real activity and 6 exact review-only actions passed.' "$tmp/dialect-record-reader.log"
