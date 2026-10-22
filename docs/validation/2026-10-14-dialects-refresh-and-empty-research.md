# Dialect visibility and empty research outcomes

## Findings

The Dialects list inherited the application's `staleTime: Infinity` and disabled focus refresh. That default assumes project file-change notifications. Dialect documents are private state under `.desk-private/input-preparations`, which the watcher deliberately excludes. An empty collection cached before a save in another window could therefore remain empty on return; saved record counts could remain stale too. A regression reproduces this with the application's exact cache defaults.

The saved Ontario healthcare and ministry RFPs dialect exists. Its first run (`research_8bb49aff100fe4bb5884e05f1779df78`) exhausted 20 model requests, made eight searches and ten page reads, and retained 17 usable sources. It never issued a records action. Retained pages included closed notices, historical project pipelines, procurement guidance and a recruitment posting. The model's final assessment says no supported currently open notice was found. Declining the request increase preserved those sources but overwrote the explanatory status with a generic “saved results” message. There were no committed records to recover.

The second run also ended with zero records. Its final assessment describes closed/cancelled notices, open notices outside Ontario and general portals rather than eligible open solicitations. Those outcomes do not establish that no eligible RFPs exist; they establish that these runs did not produce supported records.

## Changes

- Shared private-data query options refresh the collection on entry/focus and every five seconds while observed. Collection consumers in Dialects, research selection, tests and job subscriptions use the same policy. Detail readers refresh on entry/focus. Requests carry cancellation signals; failed refreshes preserve successful cached data.
- An empty cached collection shows a loading state during refresh, and a failed empty refresh shows unavailable rather than claiming no dialects exist.
- Empty record views distinguish running, paused, finished-with-zero and filtered results. Clearing filters cannot imply records were lost.
- Summary and activity projections recover the last accepted coordinator finish explanation directly from the existing journal. Individual finish steps expose their own explanation. Child results, rejected actions and unfinished actions cannot become the overall outcome. No historical journal is rewritten.
- Budget decline states the saved-record and retained-source counts. The zero-record action reads “Finish without records”; nonempty runs read “Keep N records.” Existing declined runs receive the accurate count wording through a read-only projection.
- Sources remain evidence material, not records. No records, findings, review decisions or extra request permissions are fabricated.

## Validation

- Regression failed before the refresh change and passes afterward.
- Dialect/query/UI tests cover empty cached lists, polling, failed refresh retention and recovery, current document reads, zero-result explanations and budget-decline labels.
- Server tests cover private preparation reads/imports, budget transitions, legacy outcome projection and invalid/child explanations.
- Disposable browser fixture covers an external save appearing without reload, updated record counts, filtering/reset, retained rows through an HTTP failure, recovery and absence of model requests.
- TypeScript, frontend production build, Go build and locale extraction/checks are run before installation.

The live recipe, records, research journals and chat are preserved. This change does not launch another paid research run or relax the recipe's eligibility criteria.

## Completed checks and installation

- 28 frontend test files: 164 passed (`<scratch>/dialects-recovery-tests-final.log`).
- Targeted server tests passed (`<scratch>/dialects-recovery-server.log`).
- Browser fixture passed all six checks (`<scratch>/dialects-recovery-browser2.log`).
- TypeScript, production frontend build, Go build and locale extraction passed.
- Installed manifest: `fcba4cb734290f3aa541e35e93917741fb20fd61fe388db65b930ec20e731b50`.
- Restarted only after active chats/research/jobs were idle. HTTP 200 serves `/assets/index-DfICQOQ7.js`.
- Compared 1,715 managed project JSON/JSONL files across installation: zero changed or added.
