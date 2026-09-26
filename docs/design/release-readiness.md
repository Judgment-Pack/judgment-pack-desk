# Release readiness

Implemented 2026-09-25. This extends the local Jobs pilot; schedules, connected inputs,
review inbox and Jobs backup/retention remain subsequent work.

## User flow

1. Jobs → Create job (also available from a pack's More menu).
2. Choose a saved pack and sample facts/evidence; select **Check release**.
3. Review the sample decision, test counts and expandable expected/actual results.
   Coverage gaps appear separately and remain advisory.
4. Explicitly review the test status and create the job. Failed or incomplete checks
   must be corrected in Packs/Tests and checked again. With no saved expectations,
   the review explicitly says the release is untested; it never claims a pass.

Release readiness remains visible on the job beside its immutable release details.
The full report is saved with the release rather than added to the Tests workspace's
run history. Older releases remain labeled Not run. Later edits cannot rewrite a
job's original test report. Job/run briefs read the retained evidence on demand.

## Responsibility and contract

Desk reads its saved Tests workspace; it also imports the configured matrix in memory
if that matrix has not yet been imported by Tests. Saved edits and deletion markers
win. It serializes only cases with saved expectations using the same matrix adapter
as Tests. Unsaved proposals, exploratory inputs and prior reports cannot produce a pass.
Failed reads block the check. Pack and test inputs are reread before job creation;
changes require another check and explicit review. This freshness check is advisory
against concurrent authoring: the admission guarantee is always the frozen release,
not a claim that no file can change after the last read.

Runner receives exact `pack`, optional serialized `matrix`, optional descriptive
`testSource`, and the existing sample `input` at POST /v1/previews. It performs Runtime
validation, lock generation and sample evaluation, then invokes the same pinned Runtime
with `packs test --id target --config jpack.json --format json` in a private directory.
No other project files or environment credentials are consulted. Each subprocess has
a 30-second deadline; Desk allows 130 seconds for the four-step release check; Runner allows 135 seconds
for its HTTP response rather than cutting off a valid sequence after 40 seconds.

A release retains `tests` (passed/failed/error/not-run), plus `testEvidence` when a
matrix was supplied. Evidence contains exact matrix bytes and digest, pack/Runtime
digests, timestamp, optional suite revision/names and the complete Runtime report.
No database migration is needed: release records are versioned JSON in SQLite.

Runner checks the supported report contract, pack identity/version, every unique case
ID, summary counts, row completion and process exit status. Runtime exclusively owns
comparison and coverage semantics. Exit 1 plus a complete mismatch report means failed;
an empty/invalid matrix, partial report or abnormal exit means error. Coverage is never
an admission input. POST /v1/jobs rejects failed/error evidence with 409 release_not_ready.
Direct API callers can omit the matrix and review a not-run release; Runner does not
claim to discover their project's complete suite. The installation owner is the trust
boundary in this pilot.

## Validation

Real Runtime integration tests cover passing and failing matrices, advisory gaps,
malformed/empty suites, server admission, tampered bindings, abnormal exit, report
persistence through restart, unchanged operational execution and Brief evidence.
Frontend checks cover saved-source selection, matrix import/deletions, current
expectations, read errors, omitted evidence, explicit review, stale pack/test/project
rejection and compact report rendering. Browser checks use a separate synthetic
project and real Runner/Runtime, never user business records.

Validation record: Runner real-Runtime tests passed under the race detector, and
Runner vet passed. Desk's complete Go suite and vet passed, including companion
recovery. The frontend full sweep passed 4,282 tests with one intentional skip;
the final Jobs checks passed all 18 tests after the copy adjustment. TypeScript,
production build, and all 12 locale catalogs passed. Browser checks confirmed
UI/API failure gating, stale-review rejection, advisory coverage, frozen reports,
a completed operational run, and explicit untested review. Desktop, light theme
and 390px mobile had no page errors or horizontal overflow. Local Desk and Runner
were installed and restarted; a read-only live check found the Check release control
and the runner's release-tests capability, without creating any user jobs.

A deliberately delayed four-stage release check also completed successfully in
44.1 seconds, confirming that the former 40-second HTTP limit no longer interrupts it.

Browser verification artifacts: `/tmp/release-browser/`. Changes remain uncommitted.
