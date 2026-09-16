# ADR 0002: Project conversations and the chat workspace

Status: proposed; implemented on a review branch. The private-storage endpoint
and recovery boundary need independent review before merge.

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

One operation may run per project. Navigation and chat switching do not cancel
it. Closing/reloading the application stops browser-owned work; this is not a
background job service or a heartbeat scheduler. Reload requires an explicit
request to continue. Active work and unsaved persistence warn before unload.

### Persistence

Authenticated, origin-checked GET/PUT `/api/conversations` stores version-1 JSON
under the existing pinned, owner-only local configuration root. The filename is
chosen from SHA-256 of the resolved project root. The file is not in the project,
not a pack, and not a source of runtime authority. There is no caller-supplied
filesystem path. API credentials stay in the existing key store.

Limits are 16 MiB per project and 256 chats, including archived chats. Server
validation bounds the opaque envelope; the UI validates individual records
before rendering them. Unsafe file types/ownership/permissions are refused.
Writes use If-Match, an exclusive temporary file, fsync, compare-before-rename,
atomic replacement and read-back. As with desk.json, a separate process can race
the final comparison/rename; this is not a distributed transaction. A conflict
keeps the local conversation dirty. Export it from history before reloading;
retry never silently replaces another window's version. Archive does not free
storage; export and delete do. No transcript is logged or stored in localStorage.

A checkpoint excludes runtime checks, receipt verdicts, registries and pending
approval tokens. Candidate documents are reconstructed from their exact text;
source documents are reconstructed from their original gateway responses.
Restored research reads and verifies the current registry without resealing an
already sealed session, readmits its expected cases, and reruns the runtime
checks. A missing registry or unsealed/incomplete saved acquisition cannot be
promoted to ready. A saved historical approval is display/audit data, not a
replacement for current admission. Recovery does not invoke a model.

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
