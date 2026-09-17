# ADR 0004: Document connections boundary

Status: proposed Desk/gateway integration contract; no connection API shipped.
Reviewed against gateway PR #133 at `09977ca44db9559ba7559ff579827598b91724a4`.

## Product ownership

Admin → Connections will manage available personal document sources. The chat
composer will offer Upload files plus supported connected sources, with Manage
connections returning to the same settings. Assistant remains the term for model
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

The reviewed gateway currently accepts 1 MiB request bodies and 30-second source
runs. Inline originals fit only roughly 760 KiB once encoded and enveloped,
despite the proposed adapter's larger raw-byte flag. Increased core bounds need
their own reviewed release before Desk advertises larger uploads.

Cancel means Desk stops accepting the result into the pending message. It does
not claim to cancel the gateway's acquisition: the current gateway may continue
until completion/deadline and mint a receipt after the caller gives up. Stage
cleanup must respect that fact and ignore late results after chat/context changes.

## Delivery sequence

1. Personal local storage, migration, manual backup/restore and explicit project
   history relinking: implemented in ADR 0003.
2. Accepted gateway record + extraction adapter + effective bounds: pending #133
   and implementation. Then add retained original/page objects, a backup format
   extension, source references and shared PDF previews with fixture tests.
3. Auth/capability/Drive retrieval contract and adapter: pending. Then add personal
   Connections and the reusable composer picker. Do not duplicate Claude's parser
   or provider integration in Desk.
4. Corporate identity, policy enforcement and coordinated persistence precede
   shared connections/storage. Network filesystem paths are not collaboration.
5. Retention, scheduled backup and heartbeat improvements require a durable service
   with explicit background authorization; browser timers are insufficient.

No provider placeholder should advertise capabilities the installed backend lacks.
