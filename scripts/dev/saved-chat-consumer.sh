#!/usr/bin/env bash
# Actual Go observations are temporary inputs; expected behavior stays handwritten.
set -euo pipefail
root=${JPDEV_ROOT:-$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)}
tmp_root=${RUNNER_TEMP:-${TMPDIR:-/tmp}/desk-check}
mkdir -p "$tmp_root"
scratch=$(mktemp -d "$tmp_root/saved-chat-consumer.XXXXXX")
trap 'rm -rf "$scratch"' EXIT
mkdir -m 700 "$scratch/outputs"
export DESK_RUN_CLIENT_OUTPUTS="$scratch/outputs"
export DESK_SAVED_PACK_READER_OUTPUT="$scratch/outputs/saved-pack-chat.json"
export DESK_SAVED_PACK_SUITE_OUTPUT="$scratch/outputs/saved-pack-suite.json"
export DESK_SAVED_PACK_RECHECK_OUTPUT="$scratch/outputs/saved-pack-recheck.json"
cd "$root"
producer='^Test(ConversationExactRows|ConversationUnrelatedCASRetainsStartedRun|SavedChatConsumerProducer|SavedChatConsumerApprovalVector|SavedChatConsumerCheckVector|AssistantRunClientSharedVectors|AssistantRunClientActualProducer|AssistantRunContinuationActualEino|AssistantRunContinuationActualSubscription|AssistantRunProtocolApprovalRestoredAndDeliveredOnce|AssistantSavedPackActualAPIAndReload|AssistantSavedPackExplicitRecheckHTTP)$'
command=(go test ./internal/desk -run "$producer" -count=1 -v)
# The Go wrapper owns the machine lock; never wrap this branch in a second flock.
if [ -n "${JPDEV_WRAP:-}" ]; then command=("$JPDEV_WRAP" go -- "${command[@]}"); fi
"${command[@]}"
for file in pending.json events.sse consumer-source.json consumer-terminal.json consumer-conversations.json consumer-approval.json actual-continuation-answer.json actual-continuation-extend-budget.json actual-continuation-subscription.json saved-pack-chat.json saved-pack-snapshot.json saved-pack-suite.json saved-pack-recheck.json; do
 test -s "$DESK_RUN_CLIENT_OUTPUTS/$file" || { echo "Missing actual saved-chat producer: $file" >&2; exit 1; }
done
node_command=(bash -c '
 set -euo pipefail
 cd "$1/web"
 npm test -- src/assistant/serverRuns/controller.test.ts src/assistant/serverRuns/useSavedChatRun.test.tsx src/assistant/serverRuns/presentation.test.ts src/assistant/serverRuns/decode.test.ts src/assistant/serverRuns/client.test.ts src/assistant/serverRuns/stream.test.ts src/chat/store.server.test.ts src/chat/ChatProvider.search.test.tsx src/chat/usePackProposalSave.server.test.tsx src/chat/store.test.ts src/chat/questions.test.ts src/chat/savePackProposal.test.ts src/chat/PackAssistant.test.tsx
 cd "$1"
 node scripts/saved-chat-consumer-mutations.mjs
' saved-chat-consumer "$root")
if [ -n "${JPDEV_WRAP:-}" ]; then node_command=(flock "$JPDEV_LOCK" "$JPDEV_WRAP" node -- "${node_command[@]}"); fi
"${node_command[@]}"
