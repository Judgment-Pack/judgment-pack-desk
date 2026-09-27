# Storage selection and incremental discovery — design proposal

This extends the isolated Jobs mock. It implements no storage acquisition, scheduler, provider authorization or durable checkpoint. Connection and file examples are fictional. Existing application behavior is unchanged.

## Separate the purposes

1. **Discover cases from files.** Find new matching files or explicitly supported new versions. Parse one case per object, JSONL record, array item or declared record location. Bind a stable case identifier and record version before retrieving additional evidence and evaluating each case.
2. **Find evidence for an existing case.** Locate matching artifacts by case identity, freshness and a declared selection rule. Previously processed or previously used files remain eligible evidence. Discovery deduplication must not turn retained evidence into absence. Latest-file selection is per case and per run cutoff, never a global newest-file shortcut.

## Selection UI

The right source pane has **Files**, **Output mapping**, and **Incremental** (or **Selection** for evidence lookup).

- Select an installed connection and a stable permitted root folder or bucket prefix. Browse uses that scope only. A selected example file helps build a rule; it must not accidentally become the only future file eligible for acquisition.
- Offer exact file, fixed filename in date folders, timestamped filename, wildcard pattern, and advanced regular expression.
- Date tokens are this proposed Desk UI's notation, not Databricks syntax. They match or enumerate partitions in a declared scope; they are not substituted solely with the current wall clock.
- Folder example: root `incoming/life-events/`, folder `{yyyy}/{MM}/{dd}`, name `evidence.json`.
- Filename example: root `incoming/life-events/exports/`, name `evidence_{yyyyMMdd}_{HHmmss}.jsonl`.
- Glob semantics must be explicit: `*` within one component, `**` across folders. Include/exclude patterns are separate. Push a literal prefix to the provider before bounded filtering.
- Regex is an advanced RE2 matcher over an explicitly selected filename or relative-path scope. Match the whole string; offer named date/time/case captures. Bound pattern size and scan work. A regex match is not calendar validation: reject invalid dates after extraction.
- Prefer zero-padded UTC timestamps where producers can choose names. Otherwise configure a timezone, precision and an explicit policy for ambiguous local timestamps. A folder date is a partition label, not proof of the evidence's observation time or freshness.
- Preview matching and excluded files before saving. Show relative path, captured time, modified time, new/already handled/waiting/excluded status, and the reason. Sample-only previews must say that the listing is incomplete; never call a sample a complete inventory.

## Incremental contract

A naming pattern filters candidates; it is not an ingestion checkpoint. Do not use the maximum filename date, scheduler time or last-seen lexicographic path as the sole high-water mark.

Runner retains a durable per-job/source progress ledger outside the watched input tree. Identity includes connection scope, full object key/path and a stable version identifier. For local/unversioned sources, snapshot exact bytes and compute a digest with change detection around the read. Size and modification time can accelerate checks but are not content identity. ETags are provider concurrency tokens, not universal content hashes.

Prefer immutable completed files. Offer an explicit changed-file policy: flag unexpected overwrite, or process each new observed version. Polling mutable unversioned paths cannot guarantee observing every intermediate update. Appended files are full-file versions unless a separate append protocol exists; record-level case/version deduplication prevents repeated evaluations of old rows.

Expose first-run behavior: include an existing reviewed range, or establish a baseline and start with new arrivals. Establish that boundary with listing/event reconciliation so files arriving during initialization are not silently lost. No new arrivals is a successful empty scan; an inaccessible source is a failed acquisition.

Use provider events/change feeds where available, with persisted delivery identities and reconciliation scans; otherwise use bounded paginated listing. Events can repeat and arrive out of order. A recent-folder scan (seven days in the example) is an optimization, not a completeness guarantee. Periodically reconcile all eligible partitions in the configured historical scope, and support explicit older backfill. Show the scope/cost tradeoff; late files outside an intentionally bounded retention scope need a visible policy.

Record durable work admission before advancing a discovery cursor. Persist processing completion separately. For files containing many cases, retain per-record progress against the exact retained file version; one successful case must not mark the whole file complete. Failures remain visible and retryable. Do not make source deletions or replacements erase previous run evidence. Changing selection rules or source identity requires an explicit decision about checkpoint reuse/backfill, never a silent reset.

For local producers, prefer atomic publication into the watched root; otherwise use a completion marker or a configured settling interval plus before/after read checks. A stable-size interval is a heuristic, not proof of completion. Cloud reads should use the selected immutable version or conditional read and verify returned identity. A publisher manifest can group related files into one complete case batch.

## Ownership and boundaries

- Desk: folder picker, pattern builder, output mapping, matching preview and progress UI.
- Gateway: scoped listing/read operations, provider credentials, version-bound reads, background-access capabilities and receipts.
- Runner: discovery progress, durable admission, deduplication, bounded fanout, retries, retained evidence and evaluation.
- Runtime: existing fact/evidence evaluation. No filename pattern or polling logic belongs in the pack.

Interactive file-picker permissions do not grant unattended access. The current selected S3/document adapter is not an incremental background loader. These designs require explicit background-capable adapters and Runner discovery work before activation can be offered.

## References reviewed

- [Databricks Auto Loader production: durable checkpoints, backfills and reconciliation](https://docs.databricks.com/aws/en/ingestion/cloud-object-storage/auto-loader/production)
- [Databricks file options: glob filters, recursive lookup, modified-time filters](https://docs.databricks.com/aws/en/spark/api-options)
- [Databricks Auto Loader FAQ: immutable files, overwritten files and checkpoint identity](https://docs.databricks.com/gcp/en/ingestion/cloud-object-storage/auto-loader/faq)
- [S3 event delivery: at least once](https://docs.aws.amazon.com/AmazonS3/latest/userguide/EventNotifications.html)
- [S3 event structure: ordering applies per key](https://docs.aws.amazon.com/AmazonS3/latest/userguide/notification-content-structure.html)
- [Cloud Storage metadata: generation, version preconditions and ETag semantics](https://docs.cloud.google.com/storage/docs/metadata)
