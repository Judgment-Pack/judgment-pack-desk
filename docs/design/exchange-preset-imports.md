# Reviewed Exchange preset imports

Status: implemented import/review slice of Desk #400, 2026-10-16. This does not
complete portable research execution or remove the existing recipe templates.

Desk reads an explicit release tag from an HTTPS Git catalog or local repository.
It verifies the tag, commit, tree and blob identities while reading bounded bytes,
never checks out files, executes entry code, or accepts branches as releases.
Local Git helpers, prompts, lazy fetching and non-HTTPS transports are disabled.
Tree traversal counts expanded directories as well as bytes and files. Catalog
metadata uses exact closed keys, duplicate/null refusal, valid Unicode, declared
bounds and presence-sensitive optional members.

The catalog manifest and exact preset file set are checked. Stored-file digests
and Runtime canonical document digests are separate. Presets are unbound integer
version 3 documents, not named formatVersion 3.0.0 publications. Runtime is the
authority for their document form. No successful form check establishes source
truth, fixture replay, local capability binding or run approval.

The page at `/dialects/templates` reviews publisher metadata, source declarations,
README and exact file changes against an immutable previous import. The reviewed
snapshot and history base are retained before import is offered. The exact encoded
snapshot must fit the shared retained-artifact byte limit. Import is a separate
explicit action with CAS; repeated completed requests return the original receipt
before consulting mutable state. Imported files remain content-addressed through
the shared artifact repository. History survives server restart and retains the
previous commit, reviewer and timestamp. A moved tag or downgrade is refused.
Withdrawal does not delete retained definitions or change existing jobs.

Import history is project data under `.desk-private/exchange`; it moves with the
project and is distinct from the chat-only backup archive. Artifact blobs use the
existing retained-artifact repository. This implementation does not claim a cloud
history adapter or multi-user approval service.

## Current supported scope

- `preset` entries with unbound version-3 Dialects, no fixture replay.
- Source declarations and generic field types validated by Runtime, without a
  lossy conversion into Desk's private research recipe.
- Review/import/history and exact updates, with shared loading, diff, notice,
  compact button and private-query components; all supported locales translated.
- Other entry kinds are explicitly unavailable. Upgrade notes whose prior-release
  keys cannot yet be verified against catalog history are also unavailable.

## Work remaining for #400

1. Consume the complete Exchange release history for upgrade-note keys and replay
   fixtures for executable `dialect` entries. An author assertion is not replay.
2. Add definition-driven editing and local connection binding, preserving declared
   parameters, fields, reconciliation, source provenance and byte identities.
3. Connect portable research acquisition to Runner/Gateway with authentic retained
   responses. Existing decoded Desk research actions are not those responses.
4. Move domain-specific content into published Exchange entries. The current
   catalog release contains no entries; do not replace working presets with an
   empty picker or relabel private templateVersion1 files as portable entries.
5. Implement generic downstream selection/relationships after Spec #160's workflow
   contract is accepted. The graph evaluator composes decisions; it does not
   perform research fan-out. Keep migration provenance and all saved evidence.
6. Exercise a non-GTM definition on two installations with different local
   connections and identical retained fixture outcomes. Current two-directory
   Runtime form checks are not this end-to-end acceptance test.

## Verification

Tests cover malformed metadata, credentials/local paths, file and canonical digest
separation, tag-only reads, annotated tags, dirty worktrees, configured Git helper
refusal, corrupt Git objects, exact serialized bounds, moved/lower tags, README-only
updates, withdrawal, explicit reviewed import, completed replay and server restart.
Browser tests cover explicit import, stale selection, reload and uncertain-response
retry without run/binding authority. CI runs the core and HTTP integration checks
with the pinned research-capable Runtime. The unchanged fixture's upstream commit,
licence and file hashes are retained under `internal/exchange/testdata`.

Same-vendor clean-room Codex review was requested by the user. Its findings on
metadata, Git execution/integrity and size limits were corrected. This is not a
cross-vendor review and does not claim the remaining workflow work is implemented.
