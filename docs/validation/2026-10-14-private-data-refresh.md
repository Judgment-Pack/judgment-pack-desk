# Private data refresh and storage contention

## Confirmed cause

The fresh-load Dialects notice reported `Input preparations are busy.` Saved
Ontario and ICP dialect files both validated. The preparations handler used an
immediate nonblocking directory lock, so a concurrent save could fail a GET.
The detail query then had no periodic recovery. This is distinct from an empty
research outcome: the Ontario runs retained sources but committed no records.

## Cross-page review

- Dialects: collection/detail refresh, selectors, related actions, retained-run
  readers, truthful empty/unavailable states, current terminology.
- Tests: saved suites refresh outside the file watcher; failed reads cannot say
  no cases/no runs/case deleted. Read Retry only refreshes storage. Existing
  cases stay visible during a later failure; routed editors retain their bases.
- Jobs: collection and job detail refresh, run entry refresh, triggers/references
  and background connection pickers, plus pack usage links.
- Library: folder/presentation snapshots and quick switcher, briefs and history.
- Settings: desk directory, storage status, project memory and web access.
  Memory and web-access edits hold the reviewed revision through a refresh.
- Chats: short private storage contention waits; failed initial hydration
  recovers. Loaded transcripts retain their worker ownership and are not merged
  or periodically replaced across windows.
- Existing active research/evaluation/trigger polling is retained. Watched
  project files, immutable receipts and explicit runtime checks/previews keep
  their appropriate policies. No AI execution is added by refresh.

## Implementation and validation

The shared private query policy refreshes on entry/focus/reconnect and visibly
polls, retaining the last successful snapshot through errors. The shared storage
wait is bounded to five seconds and honors request cancellation where a request
owns the transaction. Ownership, worker claims and admission locks stay
nonblocking; writes still check their original revisions after acquiring a lock.

Passed targeted Go storage/research/evaluation tests, including real directory
contention, concurrent reads of conversations/drafts/tests/briefs, stale writes
after waiting, cancellation and deadlines. Frontend regressions cover initial
read recovery, focus/hidden-window behavior, cached data, empty states, editor
revision preservation and chat hydration. TypeScript and production builds pass.
The locale check passes with existing English-fallback warnings.

Disposable-browser validation exercises the initial Dialects read failure,
external dialect/record saves, filters, failed refresh retention and recovery;
the pack journey exercises externally saved tests and cached-row retention.
No model requests are made by these checks. Installation must be performed only
when saved work is idle and verified against a before/after user-data snapshot.

## Installed result

Both browser journeys passed. Installed manifest:
`887a12d1135735f2c95fa3f3b4c9e82eda669aabc47295c22d501ebbc02f50e8`.
Desk returned HTTP 200 with `/assets/index-9NLjhhy5.js`. Before/after checks
verified 1,715 managed JSON/JSONL files: zero changed and zero added. The
pre-install idle check found no active chat, research, evaluation or job work.
