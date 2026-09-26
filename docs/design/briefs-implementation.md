# Saved one-page briefs

Brief is a contextual reader in the existing right tool rail. Select a saved case in Tests, or open a job or a completed/failed/interrupted operational run, then open Brief. The empty state offers **Generate brief**. The overflow menu offers regeneration, revision history and **Print / Save PDF**. There is no automatic generation on navigation, rerun, reload or polling.

The reader keeps the generated prose separate from deterministic evidence availability and recorded results. Required evidence remains visible even when missing or unknown. A job uses its frozen release and sample rehearsal; an individual run uses its own retained inputs and result. A case only includes a result for the identical saved case and pack text. Attached document text is reverified through the existing document pipeline when generation is requested, then frozen with the brief; opening the reader does not reacquire evidence.

AI prose is explicitly a summary. It does not assert a human review, resolve disagreement, authorize a decision, verify a document merely because availability says `present`, edit a pack, execute tests, or submit a job. The existing Vercel engine and configured model relay are reused with a dedicated brief purpose, no MCP connection, no runtime/connector tools and no adversarial second pass. Generation uses the viewer's configured model and response language.

## Persistence and concurrency

- Desk owns test-case briefs in the project-bound, owner-only private `briefs-<project-binding>.json` record. Existing atomic writes, cross-process locks, data moves, storage accounting and backups include this record.
- Runner owns job/run briefs in its SQLite database. The server constructs operational source snapshots from retained release, job and run records. Those records are never modified by brief generation. Runner data remains separate from Desk chat backups.
- Revisions are append-only through the authenticated API, with server-generated IDs, sequence numbers, timestamps, source snapshot, model and method (`ai-summary-v1`). A saved case is checked against test storage before accepting generation. No mutable history replacement endpoint is exposed.
- Begin reserves a subject for ten minutes before any model call. It requires the current revision count; a second writer is refused. Complete uses the reservation's frozen snapshot; replaying the same completed write is idempotent. Cancel and expiry leave prior revisions intact. An expired writer cannot overwrite or cancel a later reservation.
- The reader compares current inputs to the saved basis and displays a changed-input notice. Source extraction supplements are excluded from that comparison because they are derived from the already-pinned attachment references. External provider changes are not detected automatically.
- Per-record limits: 1 MiB source snapshot, 100 revisions per subject, 1,024 case subjects and 16 MiB private record. Generated prose has four bounded sections, a 4,500-character limit and a 220-word prompt budget. The brief remains readable without silently dropping required evidence.

The current installation has one local authenticated owner. Saved briefs are common server records for authorized viewers of that installation; this does not introduce organizational sharing or roles. Browser generation stops when its route is left; a closed/crashed tab may leave its reservation until expiry. Generation failures leave the last saved revision readable.

## Verification

Backend tests cover reservation exclusion, stale writers, malformed output, cancellation, expiry, immutable snapshots, idempotent completion, authenticated case persistence and backup restore. Runner tests use the real Runtime, persist both job and run briefs through service restart and confirm the operational run is unchanged. The engine-hook regression asserts brief generation opens no runtime socket and receives no tools.

An isolated browser fixture with a scripted model exercises case/job/run generation, tool switching without new model calls, reload, two revisions and history selection, malformed-output recovery, changed-input indication, expanded reading, dark/light palettes and a 390px viewport. Its sample export was verified as one A4 page. Longer source requirements can extend a printout; export does not truncate evidence to force a page count. The print action uses the browser's Print / Save PDF dialog, not a hosted conversion service.
