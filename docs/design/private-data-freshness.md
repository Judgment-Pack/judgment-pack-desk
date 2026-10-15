# Private data freshness

The project watcher excludes `.desk-private`. Its infinite query freshness is
appropriate for watched project documents, not mutable private server state.

Read-only private snapshots use `web/src/data/privateQuery.ts`: refresh on mount,
focus and reconnect, with a visible-window interval (normally 10 seconds). Shared
query keys retain their last successful data on failure. Pages distinguish initial
loading, unavailable, stale data, filtered-empty, and confirmed-empty states.
Diagnostics and Retry stay in the shared bottom status strip.

This policy covers Dialects, saved pack tests, briefs/history, job details/usage,
folders/presentation, desk/storage metadata, memory and web-access settings.
Existing research, evaluation, job-list and trigger polling retain their shorter
intervals. Project files, explicit runtime checks/previews and immutable source
receipts keep their existing policies. Never attach polling to a command or model
call. New features must choose the policy based on ownership, not page name.

An editor holds the revision it began with. Background reads must not replace
unsaved fields or advance the revision used for a write. Server-side If-Match
checks remain authoritative. Reviewed workflow snapshots likewise remain frozen.

Chats have an owned, mutable worker and a separate persistence protocol. Only
failed initial hydration is automatically retried. Established transcripts are
not periodically replaced: doing so could overwrite local messages, approvals,
checkpoints or uncertain save outcomes. This change does not implement merging
concurrent chat edits across windows.

Short storage transactions use `waitPrivateDataLock`, with cancellation and a
five-second upper bound. Waiting precedes execution and does not replay reads,
writes or paid work. Dialect reads and writes, retained-record publication, and
shared workspace storage use it. Admission, process ownership, long-lived work
leases and worker claims remain nonblocking. Lock waits do not bypass revision,
approval, file ownership or permission checks.

Regression coverage includes real file-lock contention, stale writes after
waiting, cancellation, initial read recovery, cached-row retention, settings edit
revisions, chat hydration, and disposable-browser journeys for Dialects and Tests.
