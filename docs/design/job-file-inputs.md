# Job file inputs

Jobs can read one JSON record from a browser-selected local file or Google Drive.
All snapshots, releases, run records, audit artifacts and briefs remain on the local
filesystem. Drive is an optional read source, never artifact storage.

## Ownership

- Desk owns selection, mapping review, current-pin receipt verification, and UI.
- Gateway owns Drive OAuth, single-use selection grants and signed acquisition.
- Runner owns deterministic projection, release-frozen mappings, durable admission,
  retained originals and run provenance. No AI or network is used for mapping.
- Runtime continues to own evaluation and audit semantics, without provider code.

## Flow

Create job → choose pack and input source → choose file → review source paths →
Preview mapping → Check release → review tests and sample decision → Create job.
For each Run job, choose another file and preview the frozen mapping before Submit.
Changing a file or mapping invalidates the preview and release approval. Source
selection/preview is canceled on unmount or connection changes; late results are
ignored. Existing manual/API jobs retain their original workflow.

Facts are mapped by JSON Pointer. Missing source paths remain unknown; false,
zero and null are preserved. Evidence values must be literal availability words;
connection state never proves evidence. Source JSON and provenance stay behind
compact disclosures, while the mapping preview shows exact numeric literals.

## Retention and trust

Input files are UTF-8 JSON objects up to 200 KB, not arbitrary local paths or URLs.
Every run retains the exact original bytes/hash, mapping/digest and source proof
where applicable. JSON numbers are never round-tripped through JavaScript for
submission. Jobs storage remains separate from chat backup, with the existing
pilot backup limitations documented by Runner.

Drive uses the existing picker and document ingestion. Desk verifies actual signed
receipts against the current configured gateway pin before using them. Runner
validates byte/mapping bindings; it does not claim cryptographic verification of
caller-submitted API proofs. Source acquisition is not an assertion of evidence
truth. Brief generation stays on demand and excludes raw bytes and picker grants.

Background reads and recurring schedules need a separate authorization contract;
this change grants no persistent Drive access for jobs. JSON files are the first
adapter for the mapping contract; future providers should reuse the same contract.

## Verification record

- Real Runtime runner tests, including race checks: passed.
- Desk backend tests and vet, frontend typecheck/build, and 12-locale validation: passed.
- Full frontend suite: 4,295 passed, one skipped. An initial parallel run timed out
  loading an existing pack-evaluation fixture; it passed in isolation and in the
  final full run with four workers. Final mapping UI changes passed targeted checks.
- Isolated Chrome workflow: local file selection, mapping invalidation, release
  review, new-file requirement, frozen mapping, execution, reload and Desk restart.
  Desktop/mobile and light/dark checks passed with no page overflow or JS errors.
- Signed Drive fixtures check receipt verification and reject changed pins, source
  identities, selection IDs and original bytes. A live Google Drive selection was
  not exercised; no user file was selected or read during verification.
- Installed local Desk and Runner; live smoke checks create no user jobs.
