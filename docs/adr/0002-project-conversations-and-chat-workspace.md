# ADR 0002: Project conversations and the chat workspace

Status: accepted in #91; deferred home conversation creation refined in #99.

## Problem

The Create wizard splits the prompt across metadata fields, drops its authoring
run on navigation, and uses the right pane for both assistance and inspection.
That makes it difficult to read a draft while discussing it or resume a task.

## Decision

A project owns a conversation store above its routes. Each chat has its own
composer, model, authoring mode, transcript, candidate history and optional pack
link. Opening a draft changes the layout, not the conversation. Explicit Create
uses the existing checked writer and links the same conversation to the pack.
On saved packs, a new proposal is bound to the requesting chat, candidate revision,
exact editor bytes and buffer identity. Apply remains separate from Save.

The right pane holds Assistant on these routes. Selected details move to a bottom
slot under main. Both resizers use PaneDivider; height and width use the existing
layout-preference store. The main chat view has no redundant Assistant toggle.

One operation may run per project in each window. Separate windows can
produce conflicting history updates; If-Match refuses an overwrite. Navigation and chat switching do not cancel
it. Closing/reloading the application stops browser-owned work; this is not a
background job service or a heartbeat scheduler. Reload requires an explicit
request to continue. Active work and unsaved persistence warn before unload.

### Persistence

Authenticated, origin-checked GET/PUT `/api/conversations` stores version-1 JSON
in personal private data. [ADR 0003](0003-private-chat-data-and-recovery.md)
supersedes the original configuration-root placement: existing installations stay
in place until explicitly moved, while new installations separate data from keys.
Project paths choose default history filenames; explicit bindings support project
relocation. None of these records grants runtime authority.

Limits are 16 MiB per project and 256 chats, including archived chats. Server
validation bounds the opaque envelope; the UI validates individual records
before rendering them. Unsafe file types/ownership/permissions are refused.
Writes use If-Match, an exclusive temporary file, fsync, compare-before-rename,
atomic replacement and read-back. Cooperating Desk processes serialize private-data operations with an OS file
lock. Uncooperative filesystem writers remain outside that guarantee; this is
not a distributed transaction. A conflict
keeps the local conversation dirty. Export it from history before reloading;
retry never silently replaces another window's version. Archive does not free
storage; export and delete do. No transcript is logged or stored in localStorage.

An unsubmitted composer is separate from the conversation store. Home visits and
New chat do not write a history record; the first accepted Send promotes the draft
with the same worker identity. Only the unsent home composer and its model/mode
preferences are cached in per-tab, project-scoped sessionStorage for reload
recovery. Sending or explicitly starting fresh clears that cache. It contains no
transcript, candidate checkpoint or API credential.

A checkpoint excludes receipt verdicts, registries and pending approval tokens.
Candidate documents are reconstructed from their exact text; source documents
are reconstructed from their original gateway responses. Complete draft checks
retain a versioned caller-owned record: candidate digest, exact test inputs and
expectations, cited statement evidence, runtime descriptor, source-context digest,
validation result, and the complete native snapshot-matrix report. The record's
integrity digest detects inconsistent storage; it is not a runtime signature or
production decision authorization.

On reload Desk verifies the record, exact draft/inputs and runtime identity, then
reverifies current source proofs and pins without acquiring or resealing sources.
Only an unchanged, verified record can restore current results and readiness.
Failed comparisons remain failed. Changed, unavailable, interrupted or incomplete
checks cannot unlock finalization. Development runtimes without stable version
identity and older summary-only records require an explicit **Run checks**.
Reload performs no test evaluation, model call or repair. Pending write approvals
are never recovered; finalizing still requires a fresh review and approved write.

Checks run from a draft with no saved conversation persist to that independent
draft without creating chat history. Only the statements cited by its tests are
retained as provenance; subsequent messages use distinct identities and cannot
silently replace those statements. This uses existing Runtime snapshot checks and
Gateway proof verification; neither service becomes a draft-history store.

### Engine contract

ADR 0001's engine slot remains. `AssistantSession.allowConversation` is an
optional boolean, absent by default. It permits a prose-only clarification and
changes the authoring instructions accordingly. It adds no capability. Without
it, the existing strict proposal contract and conformance recordings are
unchanged. With it, a proposed document still comes only from one explicit
fenced proposal envelope; ambiguous or malformed proposals still fail.

Draft mode uses that option and validates a candidate's structure. It does not
claim source verification or behavioral tests. Research mode retains source
receipts, independent expected cases, admission, explicit correction approval,
rehearsal and bounded repair. It requires the existing gateway configuration.

## Reuse and limits

Admin and the setup modal share EndpointForm. Draft and saved-pack previews
share PackOverview, PackQuestion, PackLogic and the detail renderers. Create
keeps the existing writer, companion records and project registration. Existing
edit buffers and stale-answer guards remain responsible for applying changes.

This first implementation supports text attachments (.txt, .md, .json, .csv),
four per drop, 200 KB each, with visible prompt contents. It does not add PDFs,
images, a server-side job scheduler, transcript import, or concurrent runs.
Model calls use the configured provider; browser validation uses fixtures and
makes no paid calls. Independent review and CI remain required before release.
