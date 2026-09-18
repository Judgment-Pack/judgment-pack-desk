# ADR 0004: Document connections boundary

Status: personal gateway/PDF settings and extraction delivered; the OAuth,
capability and organization-policy contract below remains proposed.
Updated after gateway #134–#137 and Desk #105–#107.

## Product ownership

Admin → Connections manages the personal shared gateway and PDF processing.
The chat composer offers Upload files and marks Google Drive unavailable.
Configuration is kept in Admin; the composer has no duplicate settings action. Assistant remains the term for model
configuration. Reading a Drive document will not enable chat sync or cloud backup.

The initial personal experience needs only provider, account, status and action.
Use the shared neutral settings sections, bounded Radix menus/dialogs, accessible
labels, and green/gold semantic tokens. Keep account access details expandable.
Do not show organization controls without actual backend permissions. Enabling a
provider for an organization does not authorize any member's account.

## Responsibility

- Desk owns interaction, private originals, staging/reference lifetimes, chat and
  candidate links, previews, page citations, retention and recovery.
- Gateway adapters own extraction/OCR and Drive retrieval/export. Their operators
  configure permitted sources, credentials, byte/time limits and parser execution.
- The authorization service owns account consent, token refresh/revocation and
  authenticated connection ownership. Its concrete OAuth handoff is still an
  integration dependency; a UI toggle or a provider ID cannot stand in for it.
- Runtime and JPS retain their current evaluator/pack semantics.

## Required capability and connection contract

Before implementing the picker, agree on versioned examples for:

1. Provider ID, display label, supported read operations and content types.
2. Availability: unavailable, available, or blocked by an enforced policy.
3. Effective raw-byte, request-body, output, page, text and deadline limits,
   including the gateway envelope/base64 overhead. Missing limits never mean
   unlimited access.
4. Server-derived connection ID, owner/scope, account display name, authorization
   state, allowed operations, and permitted Connect/Reconnect/Disconnect actions.
5. Authorization start/completion/cancellation and bounded picker/read requests.
6. Actionable errors: unavailable, revoked, reconnect required, wrong account,
   inaccessible file, changed source version, oversize, timeout, partial extraction.

The server resolves ownership and policy for every request. A client-provided
connection ID grants no authority. Model prompts and source text cannot choose
arbitrary executables, credential destinations, or provider scopes. Personal
OAuth connections are distinct from explicitly shared organization connections.
Begin with selected-file access and read-only adapter operations. Drive scopes
that permit writes do not imply exposing write tools.

## Attachment contract alignment

Use gateway `attachmentVersion: "1"`, not a competing Desk extraction schema.
The document adapter is a command source invoked through `/acquire`, not a tool
added to the engine MCP listing. Receipt verification remains the existing flow.

For `original.retention: "caller"`, Desk retains original bytes before acquisition
and verifies the original digest. For `inline`, decode and verify retained bytes
into the private object store. Never put original base64 in model context or logs.
Deduplicate within the authorized scope only. Document identity is the original
bytes; citations bind the extraction record and page as well, since repeat
extraction can differ.

Show page-level `ok`, `no-text`, `needs-ocr`, and `failed`, together with processing
completeness, truncation and unmapped-character information. A valid receipt
establishes provenance, not correct OCR or complete extraction. Unknown contract
versions/states/error codes and inconsistent records cannot become usable
context. Partial context requires a deliberate user choice with missing pages
visible. Tests and examples come from the accepted gateway fixtures.

The gateway now supports configurable request limits and source timeouts. The
operator must configure all layers for the intended document size; see
[document attachments](../document-attachments.md) for tested bounds. Gateway
defaults remain too small for Desk’s default 16 MiB file allowance.

Cancel means Desk stops accepting the result into the pending message. It does
not claim to cancel the gateway's acquisition: the current gateway may continue
until completion/deadline and mint a receipt after the caller gives up. Stage
cleanup must respect that fact and ignore late results after chat/context changes.

## Delivery sequence

1. Personal local storage, migration, manual backup/restore and explicit project
   history relinking: implemented in ADR 0003.
2. Accepted gateway record, extraction adapter and configurable bounds: delivered.
   Desk retains originals and verified page records, includes them in backups,
   and provides previews and source references with fixture tests.
3. Auth/capability/Drive retrieval contract and adapter: pending. Personal
   Connections is available for gateway/PDF settings; a real provider picker
   requires this additional backend contract. Extraction stays in the gateway.
4. Corporate identity, policy enforcement and coordinated persistence precede
   shared connections/storage. Network filesystem paths are not collaboration.
5. Retention, scheduled backup and heartbeat improvements require a durable service
   with explicit background authorization; browser timers are insufficient.

No provider placeholder should advertise capabilities the installed backend lacks.
