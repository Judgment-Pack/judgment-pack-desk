# ADR 0003: Private chat data and recovery

Status: implemented for personal local Desk; 2026-09-17.

## Decision

Admin → Storage & data contains Project files, Chat data, and Backups & exports.
Project pack settings remain in the project configuration. Personal chat paths
are never read from a project file. API keys stay in the protected configuration
store; relocating chats does not move model or gateway credentials.

New installations use `XDG_DATA_HOME/jpack-desk` when an absolute override is
provided, otherwise `~/Library/Application Support/jpack-desk` on macOS or
`~/.local/share/jpack-desk` on Linux. Existing installations with conversations
in the configuration directory keep that location until an explicit move.
The test-only `DeskConfigDir` override uses a private `data` child.

The protected configuration root contains a version-1 `data-location.json`:
`path` is authoritative, `previous` identifies the most recent recovery copy.
Each dedicated root has a private `.jpack-data.json` marker bound to its
configuration root. No empty store is substituted for a corrupt pointer or an
unreadable linked history. Metadata failures do not prevent restoring a valid
backup when the active root and pointer remain usable.

This remains an owner-only local filesystem store. File ownership, non-symlink
ancestors, owner-only files, pinned directory handles and atomic replacement
are retained. A destination must be separate, empty, private, and outside the
current project. Desk creates new directories with mode 0700, files with 0600;
it does not silently tighten permissions on an existing destination. Platforms
without the ownership checks or file-lock implementation refuse these features.
Windows private custody is not introduced by this change. Network filesystem
sharing and multi-user organization storage are not supported deployment modes.

## Concurrency and relocation

Every conversation operation resolves the current data pointer while holding
`.data.lock`. Separate Desk processes sharing the configuration root use an OS
file lock around reads, If-Match checks, writes, and location cutover. A busy
lock returns a conflict instead of waiting in an unbounded queue. An old client
with stale history still receives an If-Match conflict after a move.

`.data-work.lock` is shared during outbound model/gateway requests and exclusive
during a move, restore, or project relink. The initiating UI also blocks changes
while it has active work or unsaved chat changes. This is not a durable agent-run
lease: a different window can move storage between requests, and subsequent
checkpoints follow the new pointer. Model/gateway work in older Desk versions
does not honor these locks; close those versions before moving. Uncooperative
same-user filesystem writers are outside the transaction guarantee.

Relocation copies one bounded file at a time to an empty marked destination,
validates records and project links, reads written bytes back for equality,
checks that the destination still names the pinned directory, and atomically
switches the pointer. Directory creation, data, and pointer writes are synced.
Failures before cutover remove only this operation's copies. A pointer write
that may have landed preserves the destination. A process killed during a copy
may leave an incomplete destination; the old pointer and original remain valid.
Do not use an incomplete destination as a merge target; choose a new empty one.

All current conversation files are copied together. The original folder is kept
and no longer updated. Earlier recovery copies also remain if locations change
again. Desk does not automatically delete them. Limits are 4096 directory
entries, 16 MiB per project history, and 1 GiB copied per move.

## Project relocation

Default filenames remain `conversations-<SHA256(resolved-project-root)>.json`.
`project-bindings.json` can explicitly associate a moved project path with an
existing history filename. Recover project history first previews the previous
absolute folder's history; confirmation includes its digest and the bindings
revision. An existing destination history is never merged or overwritten.

The old and new paths intentionally refer to the same record after relinking.
A separate copy of a project should keep separate history. A missing referenced
record is an error, not permission to create an empty replacement. Bindings are
versioned, bounded to 128 KiB, and included in moves and backups.

## Backup and restore

An authenticated `GET /api/storage/backup` creates a complete ZIP before serving
it. The version-1 manifest lists exact allowed filenames, byte counts, SHA-256
checksums, and creation time. It contains saved chat envelopes and project
bindings only. Current retained source text and candidate checkpoints travel
inside those envelopes. Credential-store files, configuration preferences, project files,
transient files, and unsent browser drafts are excluded. These checksums detect
corruption; they do not authenticate the author or confer trust on chat content.
Restored candidates and research still pass the existing runtime/receipt checks.

Backups are unencrypted downloads with a 128 MiB payload limit. A stable
`.data-transfer.lock` bounds one backup/upload per private configuration root.
The central-directory size and entry count are checked before `archive/zip`
allocates its table. Duplicate entries, unknown names, traversal paths, special
files, unsupported manifests, missing links, inflated entries, and checksum
mismatches are refused. ZIP64 and multi-disk archives are not needed or accepted.

Restore uploads into a private temporary file, validates the archive, then uses
the same empty-destination copy/verify/cutover protocol as relocation. It replaces
the active store, never silently merges. Current files remain as a recovery copy.
The initiating browser reloads after success; other windows still have normal
If-Match conflict protection. Backups are manual; no browser timer pretends to be
a scheduled backup service.

## HTTP surface

All routes require the existing session and origin checks and return no-store.

| Route | Purpose |
| --- | --- |
| `GET /api/storage` | Effective location, usage, revision, limits and recoverable problems |
| `POST /api/storage/move` | `{path, revision}`; explicit relocation |
| `GET /api/storage/backup` | Download saved chat data as ZIP |
| `POST /api/storage/restore` | Multipart `settings` (`{path, revision}`), then `backup` |
| `POST /api/storage/project-history/preview` | Preview history for `previousProject` |
| `POST /api/storage/project-history/relink` | Same path plus `sourceRevision` and `bindingsRevision` |

Model engines receive none of these storage capabilities.

## Next integration boundary

PDF originals and extracted-page objects require a separately versioned retained
object store and backup extension after the gateway attachment contract is
accepted. Drive authorization, organization identity/policy, automatic retention,
audit configuration UI, and scheduled jobs are separate work. Runtime audit.dir
remains its existing opt-in, project-contained configuration; this ADR introduces
no evaluator or JPS format changes. See [document connections](0004-document-connections-boundary.md).
