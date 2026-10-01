# Jobs: durable local triggers

This phase extends the [Jobs workspace](jobs-workspace-phase1.md) and the approved
[operations design](../../web/mockups/jobs-operations-review/DESIGN.md).

Create job keeps its four-step flow. The Trigger step offers Manual/API, Schedule,
File changes, or Authenticated event. A selected automatic trigger is created
atomically with the reviewed release and starts paused. Job details add a Triggers
tab with compact status/next-run rows, inline configuration, explicit input preview,
review before enable, and occurrence history. Existing Run job and release review
remain available. Briefs continue to use the shared right rail.

## Ownership and runtime behavior

Runner owns schedules, observer cursors, scoped event credentials, durable
occurrences, admission and dispatch. Desk owns the UI, installation project-root
authority, and authenticated proxy. Runtime remains the evaluator. Gateway remains
the source-acquisition boundary. No new framework or repository is introduced.

Schedules support interval, daily, weekly and one time, IANA time zones, optional
end dates, DST skipping/first repeated time, skip/latest missed-run handling,
skip/queue overlap and bounded queue expiry. Desk starts the companion on boot,
without needing a browser request. Existing interrupted-run semantics remain:
uncertain execution is not automatically repeated.

Automatic inputs are explicitly configured constants, a fresh project-relative
JSON input envelope, or fresh local files through the release's frozen mapping.
The file watcher requires a stable content change used by that input. Runner
opens files beneath its pinned installation root, refuses symlink escape, limits
sizes and preserves snapshot bytes/digests. Samples and previous inputs are never
fallbacks. Invalid input is a visible occurrence failure without a decision.

Event activation returns a random scoped token once. Desk accepts it only at
`POST /api/job-events/{trigger}`, refuses browser Origin headers, and forwards it
to the exact Runner route alongside the private companion bearer. The scoped token
cannot access owner APIs. Identical event retries return the original occurrence;
changed payloads with the same ID conflict. Token rotation revokes the old key.

The same token can read the result of an occurrence it created at
`GET /api/job-events/{trigger}/occurrences/{occurrence}`. Desk parses both identifiers
(`trg_` and `occ_` with 32 lowercase hex digits), refuses any request that is not
exactly that shape (another method, an Origin or `Sec-Fetch-*` header, any query, even
an empty one, a body, or anything but one 64-hex-digit bearer token), and forwards a
request built from the parsed identifiers alone to the Runner's
`GET /v1/triggers/{trigger}/occurrences/{occurrence}`. The
Runner's 401 `invalid_trigger_token` and 404 `occurrence_not_found` pass through
unchanged, so Desk reveals nothing about whether an occurrence exists.

Receipt verification remains bound to the admitted input and its recorded time.
The queue expires before evaluation; no automatic reacquisition or external
operation retry occurs. All artifacts and automation records stay local.

## Deliberate limits

This is a single-owner local service. The host must stay awake and Desk running.
A schedule more than 30 seconds late uses its configured missed policy. File polling
is once per second, up to 128 configured triggers per store, not a filesystem event
stream or a batch ingestion engine. Pausing stops future occurrences but retains
accepted work. Files in one input are read independently, not as an atomic group.

Interactive Drive picker grants still cannot power unattended jobs. Persistent
Gateway **operation profiles** now support local schedule and Google Cloud triggers;
file-change triggers still require local input mappings. The Google adapter uses
an authenticated cloud relay and a dedicated outbound Pub/Sub pull subscription.
Its deployment and installation configuration are explicit; choosing a cloud trigger
does not provision a scheduler. See the Runner's
[Google Cloud design](../../../judgment-pack-runner/docs/design/google-cloud-triggers.md).
Public ingress, managed host services, retention and archive controls remain future work.

Runner's [OpenAPI](../../../judgment-pack-runner/openapi.json) and
[README](../../../judgment-pack-runner/README.md) describe the request contract,
limits, deduplication, restart behavior and backup boundaries.

## Verification

Regression checks cover DST gaps/repeated times, interval/end boundaries, paused
creation, reviewed enable, missed-run handling, admission reconciliation after
restart, scoped credentials, event replay/conflict/rotation, overlap, expiry,
fresh v1/v2 file inputs, exact numbers, invalid files and root escape. UI checks
cover explicit review, one-time token display, incomplete input preservation,
file configuration without implicit reads and failed preview blocking enable.

The real browser fixture creates and enables a schedule, closes the browser and
checks its recorded run; it then exercises event retries, scoped-token rejection,
file changes and responsive dark/light layouts. Fixtures use separate state and
project directories, leaving user jobs untouched. Full Runner tests and race
checks use the real Runtime binary. All supported locale catalogs are checked.
