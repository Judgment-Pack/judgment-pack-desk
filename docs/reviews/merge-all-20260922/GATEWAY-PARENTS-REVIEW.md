# Independent gateway parent review — 2026-09-22

Recommendation: **APPROVE the two reviewed deltas under the user's explicit same-vendor review exception.** No concrete, reproducible introduced blocker was found in the bounded scope below. This does not claim cross-vendor review compliance or approve unrelated changes.

## Reviewer and independence

- Reviewer identity exposed by this session: **Codex, based on GPT-6**; model vendor: **OpenAI**. A more specific serving-model variant/build identifier is not exposed to this reviewer and is not inferred.
- Fresh independent-context review: this reviewer received the assigned commits, review scope, operating constraints, and the user's authorization as relayed by the parent agent. It independently inspected code and formed its conclusion without reading author acceptance records or prior review findings for these changes.
- The parent task records the user's explicit authorization for Codex clean-room independent review and subsequent “go ahead and merge all and restart.” This is a **same-vendor exception**, not satisfaction of the different-vendor requirement in `CONTRIBUTING.md` and `docs/adr/README.md`.
- Read repository guidance in `CONTRIBUTING.md`, `docs/adr/README.md`, and `SECURITY.md`; no tracked `AGENTS.md` or `CLAUDE.md` was found in the reviewed trees.
- No subagents spawned. No posting, merging, installation, paid API, real credentials, or private-account access performed. All test writes are confined to separate `/tmp` review copies. Original source checkouts were not mutated.

## Exact revisions

| Layer | Base | Reviewed head |
| --- | --- | --- |
| Gateway PR #144 — connection catalog | `cee136fd07427d0cd2e7be8c5016356c9f2ba160` (PR #143) | `f0b469aabc175192f9b29e2c8169006cdb336472` |
| Gateway PR #148 — public web | `f0b469aabc175192f9b29e2c8169006cdb336472` (PR #144) | `f62de27b82c8f31d90bffc2f11a2be37ee4d9962` |

Paired consumers inspected: Desk PR #123 `31af425aba4fccc349cdb21dd3d3bc64123fcc76` and Desk PR #124 `88009f99324e19a7b8b14f6a38d4891e5b99e531`. Desk #124 was checked out for direct producer-consumer verification; its #123 catalog parser was extracted verbatim from the exact commit for the v1 compatibility test.

## Scope and conclusions

Reviewed catalog data ownership, supported-operation allowlists, command-mode separation, disabled-policy persistence, stateless discovery, unknown provider/protocol handling, strict version negotiation and paired Desk consumption. Discovery exits before opening stores or loading publisher registration. Returned catalog slices cannot mutate the broker's allowlist. Web input is advertised separately from account providers. Unknown capabilities do not select dynamic handlers or executables in Desk.

Reviewed public HTTPS URL admission, special/private address exclusions, all-answer DNS validation, exact numeric dialing, same-host and cross-host redirect revalidation, proxy/cookie/header behavior, TLS validation, deadlines and byte/header/output limits, MIME/charset/compression refusals, static HTML processing, original text/PDF processing, source identity, retention and acquisition provenance, command input/refusal behavior, and actual receipt-consumer compatibility. The transport retains the URL hostname for TLS verification while dialing the admitted numeric address. Rebinding to a private address on a same-host redirect was rejected before another connection.

The web record distinguishes fetched-response digest from retained static-text digest. Plain text/PDF original snapshots bind those identities together. Actual generated outputs passed both gateway/schema validation and Desk's independent record and cryptographic verification. Modifying the saved requested URL was refused by Desk against the actual signed receipt.

**Findings requiring changes: none.** No severity/file/trigger/effect finding is invented for unimplemented providers. Existing note-account adapters and the later generic cloud foundation are not treated as reviewed by this report.

## Executed evidence

Environment: Go `go1.26.5 linux/amd64`; Node `v22.23.1`; `GOCACHE=/tmp/jp-authoring-go-cache`; `GOFLAGS=-buildvcs=false`.

1. At exact #144 head: `go test ./connections ./cmd/gateway-connections -run Catalog -count=1` — passed. Actual `gateway-connections --catalog` v1 output exported.
2. At exact #148 head: `go test ./websource ./attachment ./connections ./cmd/gateway-connections` — passed. The initial restricted-sandbox invocation failed only because loopback listeners were prohibited; rerun with approved local-listener escalation passed.
3. `go vet ./websource ./attachment ./connections ./cmd/gateway-connections ./cmd/adapter-web` — passed. Built the actual gateway and gateway-connections binaries from #148.
4. Added isolated independent web probes, all passed: same-host redirect DNS rebinding; IPv4-mapped private answer mixed with public DNS; HTTPS-to-HTTP redirect; oversized response headers; chunked body exceeding 4 MiB; cancellation during body streaming. The existing suite also passed TLS hostname/untrusted-root rejection, cookie/authorization exclusion, redirect count and cross-host re-resolution, MIME/charset/compression refusal, malformed request refusal, static HTML suppression, and PDF extraction cases.
5. Exported actual `websource.read` envelopes and records from synthetic local TLS responses for HTML, plain text, normal PDF, and scanned PDF. `check_web_schema.py` accepted all four and exercised its required-member/unknown-variant negative checks for each.
6. Launched the real #148 gateway on a temporary loopback listener using only the repository's published test seed copied into a private-mode temporary file. Replayed each actual adapter envelope as an HTTP-shaped source, acquired and sealed sessions, then exported the real signed responses and registry. Desk #124 `verifyDocument` accepted all four actual records and real receipt chains; mutation of the selected URL was rejected for each.
7. Desk #124 existing `catalog.test.tsx` and `web-source.test.ts` — **31 tests passed**. Added isolated producer-consumer tests — **6 tests passed**: real v1 and v2 catalog outputs, refusal of crossed catalog versions, and the four actual signed web records.
8. Desk Go `TestIndependentActualGatewayCatalog` and `TestConnectionCatalogProcessBoundsAndValidation` — passed. The first executes the actual #148 gateway-connections binary through Desk's catalog subprocess reader.

Reproduction artifacts:

- `/tmp/jp-merge-all-20260922/gateway-parent-review/adapters/websource/independent_review_test.go`
- `/tmp/jp-merge-all-20260922/gateway-parent-review/evidence/` — emitted records/envelopes, catalog outputs, public test-key signed packets, signer log, and `sign-producer-output.py`
- `/tmp/jp-merge-all-20260922/gateway-catalog-parent-review/` — exact #144 checkout
- `/tmp/jp-merge-all-20260922/desk-gateway-parent-consumer/internal/desk/independent_catalog_test.go`
- `/tmp/jp-merge-all-20260922/desk-gateway-parent-consumer/web/src/documents/independent-producer.test.ts`
- `/tmp/jp-merge-all-20260922/desk-gateway-parent-consumer/web/src/connections/independent-catalog-v1.ts` — exact #123 parser

## Limits

This is a bounded source and fixture review, not a live provider acceptance or infrastructure audit. Synthetic TLS tests deliberately map admitted public numeric addresses to a local test listener; they exercise production admission and TLS code without external requests. The signing test replays actual produced envelopes through the real signer; it does not claim that the signer fetched the fixture page itself. No real public-site request or cloud account flow was attempted. No browser visual review, installed-bundle smoke test, release image audit, or exhaustive hostile-PDF fuzzing was performed. The fixed DNS/routing/trust-root assumptions remain operator-controlled as documented. These conclusions apply to the exact reviewed deltas and paired consumer commits, not subsequent material edits.
