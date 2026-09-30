# Independent Drive whole-account adoption review

Reviewed the implementation before author review narratives. Scope: Drive handoff, relay, isolated gateway checker, catalog/client, ConnectionsPane, DriveFilePicker, job input fields/mapping, attachment boundary, storage reconnect, setup guide and gateway pin. Distinguished unrelated component-sync work using the pre-existing diff. No production edits, commits, live Google calls, or user custody/credentials accessed.

## Finding and final disposition

| ID | Severity | Location | Finding | Proposed verification / disposition |
| --- | --- | --- | --- | --- |
| R1 | P2 | `web/src/jobs/MappedInputFields.tsx:77-85`; acquisition at `web/src/jobs/mappedInputs.ts:87` | A Drive `reconnect-required` during **Read sources and preview** only becomes error text. The new picker has already closed after selection, and this later acquisition neither normalizes the adapter diagnostic through `connectionFailure` nor presents a Reconnect action. A token can expire or be revoked between selection and preview. Search/select correctly expose reconnect, but this separate job-read leg does not meet handoff item 4. | Normalize the Drive acquisition error, expose explicit reconnect from the job flow, and preserve mapping, case values and other selected/local sources. Test: select Drive file; reject its later preview acquisition with `reconnect-required`; assert Reconnect appears without automatically opening consent, existing inputs remain, and canceled/unmounted reauth cannot apply late results. **Fixed and independently verified.** Drive reconnect errors are normalized at `mappedInputs.ts:87-93`; `MappedInputFields.tsx:81-91,110-112` now exposes explicit reconnect, preserves mapping/case/local snapshots, discards expired Drive grants only after completed consent, and ignores aborted late replies. |

No unresolved actionable findings remain. Final disposition: R1 fixed. Follow-up reviewed the isolated PR worktree, including the unchanged original Runner pin, exact gateway v0.7.0 pin, required adapter-render packaging, and the storage-route addition to containment coverage.

## Invariants checked

- `internal/desk/connections.go:164-168`: removed pick is rejected before companion dispatch. Lines 214-219 apply the existing managed-local document capability check to both Drive search and select. Catalog operation checks and session guards remain.
- `ConnectionsPane.tsx:125-138,153-164`: search results remain metadata state; select receives only checked row IDs. Returned Drive IDs/count/unique IDs/grant shape are validated before ingestion/callback. Only after selection is `resourceId` mapped to adapter `fileId`.
- `ConnectionsPane.tsx:68-90,157-162`, `SourceInputFields.tsx:30-50`: cleanup cancels pending work; late replies are checked before callbacks; the source-job read receives the picker abort signal.
- `MappedInputFields.tsx:85`, `mappedInputs.ts:29-46,85`: one selected file is assigned to its chosen source; grants still fill only explicit string case parameters with safe pointers; expanded acquisition IDs/grants must match the selected pair.
- `StorageFilesView.tsx:51-53,89,139-142`: Drive reconnect is explicit and leaves editor/plan state intact; commit remains reachable from the UI review dialog, and deletion requires exact typed confirmation. No new assistant/job files-commit exposure found.
- `scripts/local-gateway-check.py`: temporary XDG custody and synthetic registrations retained; checks whole-Drive scope, source-search catalog, search/select refusals and removed pick. Unrelated local-plan checks were already in the baseline.
- Gateway pin is exactly `v0.7.0` / `adf57076ec9e7fb9a6816df751b95d1f67423324`; setup copy enables Drive API, uses whole-Drive scope, and links Drive scope guidance.

## Independently executed checks

- `go test ./internal/desk -run 'Test.*(Connections|Connection|GmailRoutes|CancelDoes)' -count=1` — passed.
- Node 22.23.1: focused Vitest suites `ConnectionsPane.test.tsx`, `client.test.ts`, `SourceInputFields.test.tsx`, `MappedInputFields.test.tsx`, `StorageFilesView.test.tsx` — 5 files, 81 tests passed.
- Initial default-node test invocation failed before running tests because the shell selected unsupported old Node; rerun with installed Node 22 passed.
- Follow-up in the isolated PR worktree, Node 22.23.1: `npm test -- --run src/jobs/mappedInputs.test.ts src/jobs/MappedInputFields.test.tsx` — 2 files, 18 tests passed, including acquisition-error normalization and reconnect complete/cancel/unmount cases.

R1 was initially established by code flow and its fix was subsequently verified through code review and the new focused regression tests. No independent live-account migration, browser OAuth flow, full component CI, or fresh release-archive integration run was performed. Those remain separate evidence requirements; parent-session results are not counted as independently executed here.

## Author validation

The six handoff items ship together because the new catalog and old Picker
contract are incompatible. Gateway v0.7.0 is pinned with its required render
adapter; the existing Runner pin and unrelated component-update work are outside
this PR.

| Handoff item | Change | Verification |
| --- | --- | --- |
| 1. Catalog and relay | Accept Drive source-search; reject removed pick | Catalog tests, authenticated relay tests, real v0.7.0 catalog |
| 2. Chat | Browse bounded metadata; select checked IDs; map resourceId to fileId | Component regressions and browser synthetic selection |
| 3. Jobs | Shared selector with one file; unchanged typed grant slots | Both job field suites, mapped-input tests, desktop/mobile browser |
| 4. Reconnect | Explicit recovery from search, selection, acquisition and storage errors | Old-scope failures, delayed acquisition, cancel/unmount, dirty buffer preservation |
| 5. Setup | Drive API only, whole-Drive scope and verification guidance | Setup tests and complete locale coverage |
| 6. Pin | Gateway v0.7.0 at adf57076ec9e7fb9a6816df751b95d1f67423324 | Published release/tag check, actual isolated companion build |

- Full isolated PR web suite: 249 files; 4,572 passed and one existing skip.
- Full Go suite passed; connection/Jobs race tests and Go vet passed.
- Typecheck, production build and all 12 locale catalogs passed.
- All 925 mutation needles remained valid.
- Real Desk with Gateway v0.7.0 passed isolated catalog, whole-Drive consent URL,
  removed-picker rejection, unconnected search/select refusals, cancellation,
  unknown-grant refusal, signed PDF extraction and configuration-preservation checks.
- Browser checks used synthetic Drive responses: chat selection and reconnect,
  unsent draft preservation, single-file job selection, 1440px dark and 390px light
  containment, and focus restoration. No job file content was read on selection.
- The full containment gate now samples the existing storage-files route too.

No real Google consent, live-account scope migration or Drive content retrieval
was performed. No shared credential custody was changed. Google Doc creation,
folder management and new assistant/job write operations are not part of this PR.
