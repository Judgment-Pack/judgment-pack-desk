# Local gateway PDF/HTTP integration handoff

This is an integration-author report, not independent approval. All commits remain local. No remote push, PR merge, live state, credentials, external provider, or paid API was used by this task. Parent is coordinating final independent review and any subsequent fixes.

## Exact inputs and outputs

Base: gateway origin/main as fetched by parent, `625a24a8ae82f49ea171feb9c8382153e8dbd8a2`.

| Input | Head | Local merge commit |
| --- | --- | --- |
| #145 PDF read correctness | `14fda49e344c10d756ba8fe43927c0eda462835b` | `89513551b05c9ac2732878d021d52295f6feecc1` |
| #146 PDF memory/work/deadline bounds | `e91190e1a498aacdaf1f7a83a7e2a15b7ef00d29` | `acfdeb939033cf283608509e10b534b1991ce09b` |
| #147 strict HTTP envelope | `b22ac23e01b7e3d3218b0811be6d0e06e2de6b0b` | `98fad04795fa55c87c26e115db4e725531ccf769` |
| Reviewed connection/source foundation, parent follow-up | `a36190272b012dd5f9d4e5fcaab55a5421fc2eaf` | `d64e9d2641ab9e4bae7a170699c11c1af3983461` |

PDF/HTTP checkout: `/tmp/jp-merge-all-20260922/pdf-integration`, branch `integration/pdf-fixes`, HEAD `98fad04795fa55c87c26e115db4e725531ccf769`.

All-gateway checkout: `/tmp/jp-merge-all-20260922/all-gateway-integration`, branch `integration/all-gateway`, original handoff HEAD `d64e9d2641ab9e4bae7a170699c11c1af3983461`. Parent owns further edits here, including the independent review's codespace finding; do not assume its current working tree remains this original handoff.

All four merge commits preserve input histories and carry `Signed-off-by: kikashy <35789537+kikashy@users.noreply.github.com>`. No published history was rebased. #145 and #147 merged without conflicts. The foundation merge also has no conflicts or manual resolutions; its remerge diff is empty. Its PDF/document and core trees are byte-identical to `98fad047`; documentation auto-merges append separate source-extension sections.

## Conflict resolutions and semantic intersections

#146 conflicts span 27 regions in eight production files: `adapters/document/pdf/{cmap,content,crypt,document,extract,filters,font,lexer}.go`.

Exact conflict-resolution evidence: `/tmp/jp-merge-all-20260922/pdf-resolution.diff`, generated with `git show --remerge-diff acfdeb9`. This includes cleanly-applied semantic corrections and tests as well as textual conflicts. Comparisons against the two input branches are reproducible with `git diff 14fda49 acfdeb9 -- adapters/document/pdf` and `git diff e91190e acfdeb9 -- adapters/document/pdf`.

- Keep the #145 generation scope around multi-field operations, the section's generation captured before `/Length`, stale-section queue termination, raw encryption-dictionary reads, rebuilt-handler cache invalidation, and file-scoped scan failures.
- Retain the #146 shared document allowance, lexer byte charges and reservations, parser allowances, decoded-copy ownership (`heldByDocument` versus `heldByPage`), font/CMap deadlines and page work budgets.
- Clear object/font/CMap/header caches through #145 helpers without refunding cumulative charges. The single opening reset where no walk/page can retain objects also clears head classification and releases parsed storage, as #146 specifies. Later resets retain the charge.
- Object-stream headers retain #145 ordered number/offset positions and duplicate handling. Charge each declared pair for two map-entry allowances and two ordered-slot allowances; charge unread placeholder positions two slots each before appending. Retain lexer exhaustion checks before publication and generation-gated cache installation.
- CMaps retain length-sensitive keys, codespace matching/inference and unusable-encoding behavior. Apply four-entry range charges and decoded-destination charges while still charging unusable scalar-string destinations. A parsing allowance or deadline abandons the entire map.
- Strict inline-image dictionaries use #146 construction and token allowances, restoring the lexer's previous allowance on exit. Strict hexadecimal/lexical behavior survives with odd-nibble growth reservations.
- The inline-image framing entry returns `deadlineMet(it.ctx)`, not `it.ctx.Err()`, because a clock deadline can precede context timer cancellation.
- Clean-merge inspection found `objectRead`'s new exhausted-budget branch used unconditional `noteBound`/cache assignment. It now uses `noteBoundAt(generation, err)` and `publish(generation, ...)`, matching every other read publication. The cumulative charge is retained.

Four committed intersection regressions live in `integration_followup_test.go`: stale exhausted reads cannot publish; unread object-stream slots consume allowance; inline framing reports clock deadlines; strict dictionaries restore the lexer allowance.

Test adaptations preserve their original discriminators: API signatures and walker path/generation; exact header charge includes the additional #145 retained structures; the unusable-CMap destination test uses a fresh construction allowance and asserts exact charged mappings so construction bounds cannot mask its claim; count-bound fixtures include enough stream bytes to admit the independent memory budget.

## Existing review records and final deltas

Read `CONTRIBUTING.md`, `docs/adr/README.md`, PR bodies and supplied final review dispositions. No applicable AGENTS.md was found in checkout or ancestors.

- #145 latest supplied record `gateway-145-comment-5777412628.json`: eight-round findings accepted/applied in `14fda49`; a ninth review is described as pending. The question of including `conformance` remains explicitly held. The final commit introduces the inferred-codespace run cap as a maintainer rule. Parent has assigned independent final-delta/integration review and is addressing that review's codespace finding.
- #146 latest record `gateway-146-comment-5776705964.json`: ten minors and a note accepted/applied in `e91190e`; sixth review described as pending. Final clock arbitration and regression deltas require that separate review; my checks are author verification only.
- #147 latest record `gateway-147-comment-5769469126.json`: one minor/five notes dispositioned and implemented by `b22ac23`; record explicitly invokes its disposition exception for these fixes.

Focused final production deltas exported for review: `pdf-final-reads-delta.diff` (CMap/content/document/filter changes) and `pdf-final-bounds-delta.diff` (request arbitration). Reviewer should additionally read `git diff 14fda49^ 14fda49 -- adapters/document/pdf/extract.go adapters/README.md` and the new tests. Parent authorized a clean-room same-vendor exception; this report does not substitute for that reviewer.

## Verification

Environment: Go 1.26.5 linux/amd64. Every Go invocation used `GOCACHE=/tmp/jp-authoring-go-cache GOFLAGS=-buildvcs=false`. Paths below are under `/tmp/jp-merge-all-20260922`.

| Check | Result and evidence |
| --- | --- |
| `cd pdf-integration/go; go test ./... -count=1` | PASS, 72.325s, `pdf-core-tests-final.log`; required approved escalation for local httptest ports. Initial sandbox-only attempt failed to bind loopback, `pdf-core-tests.log`. |
| `cd pdf-integration/go; go vet ./...` | PASS |
| `cd pdf-integration/go; go build -trimpath -o /tmp/jp-merge-all-20260922/pdf-gateway .` and `/tmp/jp-merge-all-20260922/pdf-gateway conform` | PASS: 30 canon and 41 store vectors, zero disagreements; `pdf-conformance.log`. |
| `cd pdf-integration; GATEWAY_BIN=/tmp/jp-merge-all-20260922/pdf-gateway SMOKE_REQUIRE_ENGINE=1 python3 -m unittest discover -s plugins/smoke/testing -p test_stand_in_engine.py -v` | PASS, four tests, 9.113s, `pdf-smoke-parity.log`; approved local-listener escalation. |
| `cd pdf-integration/adapters; go vet ./...` and `gofmt -l .` | PASS, no formatting output. Core edited files also remain gofmt-clean. |
| Focused integration/adjusted fixtures: `go test ./document/pdf -run '^TestOpeningBoundsArePDFMalformed$\|^TestBoundsObjectStreamHeaderEntriesAreCharged$\|^TestReadsAnUnusableCMapIsChargedForAllTheSame$\|^TestIntegration' -count=1` (regex alternatives without backslashes in actual command) | PASS, 3.320s, `pdf-reconciled-tests.log`; also `pdf-integration-tests.log`. |
| `cd pdf-integration/adapters; go test ./document/... ./cmd/adapter-document -count=1` final run | Document PASS 8.730s, command PASS 3.838s. PDF ran 119.487s with exactly one failure: old test assertion omits `d.fileBound`; see below. `pdf-document-tests-final.log`. |
| `cd all-gateway-integration/adapters; go test ./... -run '^$'` | PASS all packages, `all-gateway-compile.log`; this is compile validation only. |
| Extra all-gateway source behavioral suite | NOT STARTED: escalation auto-review was rejected with usage-limit failure. Requested `go test . ./connections ./attachment ./websource ./cmd/adapter-sources ./cmd/gateway-connections ./mcphttp -count=1`; no workaround attempted. Parent handles any authorized continuation. |

### Remaining PDF assertion correction

`TestBoundsTrailerRescanIsBoundedByWhatItExamines/objects_whose_parse_is_given_up_on` tests only `d.bound`. The combined scanner correctly stores this exhausted-budget bound in `d.fileBound`. Reproduced three times at original committed candidate: `pdf-trailer-rerun.log`.

A temporary Go overlay changing the assertion to accept either `d.bound` or `d.fileBound`, and additionally asserting the affected case actually has `isBound(d.fileBound)`, makes the entire five-case test pass three times (0.734s): `pdf-trailer-scope-probe.log`. Exact probe/overlay: `pdf-trailer-scope-probe.go`, `pdf-trailer-overlay.json`.

Command: `cd pdf-integration/adapters; GOCACHE=/tmp/jp-authoring-go-cache GOFLAGS=-buildvcs=false go test -overlay /tmp/jp-merge-all-20260922/pdf-trailer-overlay.json ./document/pdf -run '^TestBoundsTrailerRescanIsBoundedByWhatItExamines$' -count=3 -v`.

Parent was notified and owns the assertion correction together with the independent codespace correction. No source mutation was made after the parent requested a freeze. A full PDF rerun on the corrected final candidate remains required; do not describe the original final PDF suite as passing.

## Limits and pending work

No test sessions from this agent remain running. No race suite, cross-platform builds, Docker agreement, live providers, plugin npm build, or fresh complete all-gateway behavioral suite was run here. The final independent codespace fix and file-bound assertion correction are outside this original handoff and need parent validation/reviewer confirmation. Integration authorship is not independent approval.
