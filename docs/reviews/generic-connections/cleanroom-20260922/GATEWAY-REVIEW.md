# Independent gateway foundation code review

Recommendation: **APPROVE gateway PR #149 within the reviewed scope.** No reproducible introduced defect requiring a change was found. This is a code-review recommendation, not merge approval for the entire parent stack or live-provider certification.

Signed attribution: **OpenAI Codex, independent clean-room reviewer `/root/foundation_gateway_review`, 2026-09-22.** Vendor: **OpenAI**. The reviewer is described by its developer context as Codex based on GPT-6; the exact served model identifier is not exposed, so no more specific model name is asserted. The drafter is also OpenAI Codex. This is the user's explicitly authorized **same-vendor exception**, not a cross-vendor approval and not compliance with the repository's ordinary different-vendor review rule.

## Exact reviewed revisions and independence

| Repository | Head reviewed | Base for introduced-change review |
| --- | --- | --- |
| gateway PR #149 | `a36190272b012dd5f9d4e5fcaab55a5421fc2eaf` | `f62de27b82c8f31d90bffc2f11a2be37ee4d9962` |
| Desk PR #125, boundary inspection | `c67f6096c4285a62726d7c39b4615ee2a99803c6` | `88009f99324e19a7b8b14f6a38d4891e5b99e531` |

The review started from the detached trees and task scope without inherited implementation conclusions. I read repository contribution/review instructions, changes, implementation dependencies, design contracts and executable tests. I did **not** read `docs/reviews` or `docs/design/cloud-review-foundation-response.md`, and did not rely on drafter validation or previous reviewer responses. Existing test helpers were inspected as code and used where stated below.

No tracked reviewed source, original implementation checkout, credential, live service or PR was modified. Probe source, configuration, generated fixtures and logs are under `gateway-repros/` beside this report.

## Scope actually reviewed

- Gateway's changed attachment source kind, exact member validation, provider/resource/display-URL validation, retained-byte identity checks and published JSON Schema.
- `ResourceDocument`, its shared document processor, inherited processing/deadline/output limits, text normalization, PDF processing states and OCR disablement. I exercised retained originals through complete, partial and failed extraction records.
- Resource page count/size/text/cursor bounds, unavailable-item metadata, companion response-line cap, no-partial-result error envelope and stateless catalog/local-plan command modes.
- Catalog v3 presentation, setup, authorization-endpoint and source metadata, shipped-provider coverage, localization presence, legacy catalog preservation and local launch plan contents.
- Changed disconnect revocation reporting and relevant inherited connection/grant/custody behavior. Actual provider-specific authorization/retrieval implementation was inspected where needed to understand the retained-file helper's caller obligations, not re-reviewed in its entirety.
- Desk's consumers of catalog/presentation/local-plan data, verified bundle and explicit manifest-digest trust input, launch argument/environment construction, generic connection routing, resource page/status checks and document ingestion/reverification.
- Current signer/authority pin, sealed receipt, acquisition source/shape, argument commitment, selected provider/resource, retained original bytes and document metadata checks. The generic proof path is additive; legacy proof checks remain separately selected.

I did not review every Desk UI change; another reviewer owns the detailed Desk review. I inspected inherited gateway source-environment construction and ran the frozen corpus, but did not perform a fresh full audit of the unchanged signing core, PDF parser or parent PR implementations.

## Findings

**No actionable introduced finding was reproduced.** There is consequently no severity/path/trigger/fix finding entry to disposition.

Evidence supporting the recommendation:

1. The actual `--catalog-v3` output is accepted by Desk's parser with all four shipped providers supported and web availability retained. Current local-plan entries reproduce the prior explicit source names, executable arguments, shapes, timeouts and custody environment selection. Discovery does not open account storage; its negative mixed-mode tests pass.
2. The shared resource producer validates retained-byte size and source identity, disables OCR, applies the existing processor deadline/bounds, retains originals and checks the final canonical record. Independent probes produced valid records for all four supported text media types and seven distinct PDF fixture outcomes. Every generated record passed the published schema and Desk's document checker.
3. Eleven freshly gateway-produced records were signed using the repository's published test-key helper and passed Desk's retained-proof verification. For each, a different current signer pin, different current authority, altered resource selection and missing seal were refused. These tests exercise cryptographic consumer acceptance offline; they do not assert provider authorization from a receipt.
4. Independent probes refused empty input, 4 MiB + 1 input, credential/query/fragment/non-HTTPS display URLs, duplicate resource IDs and inconsistent continuation flags. Exactly 4 MiB plain text succeeded. An empty page with a nonempty continuation token was accepted by both sides. Existing tests cover oversized control-response refusal, unavailable reasons, resource page budgets, PDF provenance, discovery modes and truthful disconnect reporting.
5. Credential custody and retrieval remain gateway responsibilities. The new producer does not fetch, create grants or authenticate. No shipping resource adapter calls it yet; its documented precondition is that the caller already admitted/consumed the selection grant and bounded retrieval. I found no basis to attribute an unimplemented cloud adapter's possible mistakes to this PR.

## Validation actually run

| Check | Result |
| --- | --- |
| Gateway targeted `go test ./attachment ./connections ./cmd/gateway-connections` | Passed with local test listeners enabled |
| Gateway adapters `go test ./...` | Passed, all packages |
| Gateway adapters `go vet ./...` | Passed |
| `gofmt -l` on changed Go files | No output |
| Gateway `go run . conform` | 30 canonicalization vectors + 41 store vectors; 0 disagreements |
| Independent `producer_probe.go` | Passed: four text media types; normal, mixed, scanned, encrypted-user, malformed-truncated, inflate-bomb and many-pages PDFs; byte/URL/page bounds |
| Schema validation of the 11 newly generated records | Passed with jsonschema 4.23.0 |
| Independent `consumer_probe.mts` | All 11 records verified; current-pin/authority/resource/seal refusals passed |
| Independent `catalog_probe.test.ts` | 2 tests passed with Vitest 4.1.11 / Node 22.23.1 |
| Desk selected catalog, local-plan and manifest-approval Go tests | Passed with local test listeners enabled |

The initial gateway and Desk selected test runs hit the sandbox's loopback-listener prohibition; these were environment failures, followed by passing authorized reruns. A direct catalog import in Node encountered Vite's `import.meta.glob`; the final catalog probe runs under Vitest. Reproduction source, exact commands and these environment details are in `gateway-repros/REPRODUCE.md`.

## Limits and parent-stack disposition

Both comparison bases are heads of open draft parent work, according to the provided parent metadata. This review assesses the foundation delta against those exact bases and the relevant inherited dependencies; it does not approve those parents or establish that either PR is ready to merge to the default branch. Any material change after the reviewed heads needs its own review under the applicable process.

No AWS, Azure, Spaces or Dropbox adapter, real account, live OAuth flow, paid API, deployment or installed bundle update was tested. The recorded result demonstrates the generic record/catalog boundary on local fixtures. It does not establish cursor custody, grant consumption, remote revocation, read authorization or credential handling for a future adapter. Those must be reviewed with the adapter implementation.

The installed bundle and operator-selected manifest digest are trust inputs. This review does not claim publisher-signature verification or protection from a malicious operator-approved executable. Platform execution was Linux only; no Windows/macOS run or complete browser interaction suite was performed by this reviewer.
