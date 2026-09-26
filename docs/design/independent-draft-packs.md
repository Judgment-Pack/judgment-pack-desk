# Draft packs belong to Packs

Packs → Browse combines durable Desk drafts with runtime-registered packs. Status
(All statuses, Draft, Finalized) precedes Sort and combines with folder scope and
search. Opening a pack retains the collection and its filter/scroll state.
Draft titles, folders and identity do not depend on chat titles or visibility.

`/packs/drafts/:draftId` loads the artifact without its originating conversation.
Chat history always opens `/chats/:chatId`. The pack Assistant can resume linked
conversations or start another conversation from the current artifact. Opening a
page does not make a model call. Create pack opens an unsaved workspace; its first
candidate creates the durable draft. To author another pack, use Create pack or a
new unlinked conversation. Candidate revisions in a linked chat update that pack.
A stale conversation cannot overwrite a newer artifact generation.

The right workspace tools and canvas remain shared with finalized packs. Draft
rename/delete are in the title-row actions. Deleting or archiving a chat retains
its draft; deleting a draft leaves an unavailable link in the conversation.

## Persistence and trust

Authenticated, origin-checked GET/PUT `/api/draft-packs` uses a separate private
`draft-packs-<project-record-hash>.json`, the same owner-only data root, file-safety
checks, process lock, atomic staging, 16 MiB ceiling and If-Match compare as chat
storage. The record holds at most 256 artifacts and 4096 deleted identities.
Project-history relocation resolves both repositories through the same binding.
Workspace move, backup and restore include the draft record and retained document
objects. Project folder metadata remains with other project files.

A draft retains candidate bytes/revisions, its authoring brief, source packets,
attached-source references/text, test expectations, and the finalized pack link.
It does not copy conversation turns into the artifact. The brief is artifact
input; it may contain source text supplied in the initial authoring request.
Validation and receipt verdicts are re-established after restore. Recheck is an
explicit, model-free action. Existing runtime/research gates still govern Review
and finalize; saving a draft neither registers it nor makes it executable.

The first load migrates candidates from all chats, including archived chats.
Existing finalized pack links are excluded. A stable case-sensitive migration
identity and deletion tombstones make retries idempotent. Drafts are committed
before conversation links or deletions. Original chat checkpoints are preserved
until the user deletes the chat. A deployment backup is retained before the first
local migration. No lifecycle fields are added to JPS documents.

Finalization creates/registers the runtime pack through the existing reviewed
write path, then persists its link on the artifact. Its prior draft URL redirects
to the finalized pack, and Browse shows one row. A failed link save remains dirty
and requires Retry saving before leaving; these separate repositories and project
files do not form a cross-file transaction.

## Verification

- Storage tests cover authenticated access, stale writes, independent chat
  deletion, backup/restore, private permissions and storage byte accounting.
- Client tests cover archived migration, retry after partial save, all revisions,
  source preservation without transcripts, deletion tombstones, finalization,
  empty-home behavior and collection filter/back-navigation behavior.
- Browser checks use intercepted writes against a copy of local history: opening
  from Browse, retained Status filter, refresh after chat deletion, stable canvas
  pan, right tools, full-height details, narrow layout and saved-pack regression.
