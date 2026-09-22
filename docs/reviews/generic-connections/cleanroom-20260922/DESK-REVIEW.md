# Independent Desk foundation review

Recommendation: **request changes** for one confirmed P2 functional defect in the newly supported OAuth/form protocol. No other substantive finding is asserted from the scope covered below.

Review date: 2026-09-22. Reviewer vendor: **OpenAI**. Model identity available in this session: **Codex, based on GPT-6**; a more specific deployment/model identifier was not exposed to this reviewer. This was a fresh reviewer role, not the drafter. The drafter was OpenAI Codex, so this is the explicitly authorized **same-vendor clean-room exception**, not cross-vendor review compliance.

Reviewed detached trees:

| Repository | Base | Reviewed head |
| --- | --- | --- |
| Desk PR #125 | `88009f99324e19a7b8b14f6a38d4891e5b99e531` | `c67f6096c4285a62726d7c39b4615ee2a99803c6` |
| Gateway PR #149, paired contract only | `f62de27b82c8f31d90bffc2f11a2be37ee4d9962` | `a36190272b012dd5f9d4e5fcaab55a5421fc2eaf` |

I formed the finding from source and an independently written reproduction. I did not consult the drafter's design documents, review documents, screenshots, acceptance results, or prior cloud review responses. Existing test source and fixture helpers were inspected and executed where listed. No tracked reviewed source or original implementation clone was edited; the reviewed Desk tree remained clean. A proof-of-fix experiment was performed only on a disposable copy and then reverted there.

## Finding D1 — P2: Required setup fields permanently disable OAuth continuation

**Location:** `web/src/connections/ConnectionsPane.tsx:222`, with the same condition on the reconnect action at line 221. Supporting flow: lines 86–92, 192; admission at `web/src/connections/catalog.ts:37`.

**Trigger:** An installed gateway advertises a valid generic `resource-v1` provider with `auth: "oauth"`, `registration: "form"`, at least one required setup field, the required operations, and a declared HTTPS authorization endpoint. The user saves the required registration fields successfully. The gateway then reports `not-connected`, meaning registration is complete and OAuth consent is the next action. Opening an already configured but not connected provider has the same problem.

**Effect:** `connect()` clears the setup state after successful `configure`. Once the status becomes `not-connected`, line 192 hides the setup form for OAuth. However, line 222 still disables “Continue with <provider>” based on `missingSetup` for every `registration === 'form'` provider, even though this action would call OAuth rather than configure. Required fields are empty and invisible, so the user cannot continue, even after reopening the pane. The reconnect branch has the same mismatch.

**Evidence:** The independent component test first passes the descriptor through the actual `parseConnectionCatalog` and asserts that it is supported. It fills and successfully saves required form fields, observes that the password value has cleared, switches the mocked protocol status to `not-connected`, and asserts that the setup field is absent. The assertion that the Continue button is enabled then fails: expected `false`, received `true` for `button.disabled`. No provider account or external request is involved.

- Reproduction: `desk-repros/web/src/connections/cleanroom-oauth-form.test.tsx`
- Original failure: `desk-repros/oauth-form.log`
- A minimal condition change applied only to the disposable copy makes the same test pass: `desk-repros/oauth-form-fix-proof.log`
- Suggested fix direction, saved as an illustrative patch: `desk-repros/oauth-form-fix-direction.patch`

**Fix direction:** Derive one “this action configures a form” condition matching the form rendering/configure branch (`registration === 'form' && (state === 'setup-required' || auth !== 'oauth')`). Require setup fields only for that action; allow consent and reconnect when registration already exists. Cover initial configured-but-not-connected status, successful setup → consent, and reconnect in regression tests.

**Attribution and severity:** Introduced by this PR's new generic form/OAuth support. Existing providers do not currently use this exact combination, so this is not a present AWS/Azure/Spaces/Dropbox adapter defect. It is a defect in a protocol combination the Desk parser already advertises as supported, and it prevents a compatible gateway-only OAuth provider from being used without a Desk fix. P2 is appropriate; no credential exposure or signature bypass was demonstrated.

## Scope covered

- Desk production diff and relevant inherited paths: catalog relay and parsers, supported/unsupported provider routing, generic presentation and forms, OAuth tab URL checks, connection companion dispatch and cleanup, manifest verification and operator digest override, source-plan decoding and worker launch, connection pane request ownership and pagination, chat attachment flow, object retention, record and receipt verification, source reader links, and shared menu/shell integration.
- Paired gateway contract source: v3 catalog/presentation and local launch plan, control result/page structures and bounds, common retained resource producer and source validation, attachment contract additions, companion response bounds, and disconnect response behavior. This does not replace the separate gateway review.
- Failure and boundary review: catalog disappearance, account/resource context changes, cancellation and late replies, unsupported protocols, setup state cleanup, credential recovery, cursor loops, source-size/unavailable rows, truthful disconnection, proof-kind/provider/resource/argument bindings, original bytes, and current pin verification.
- Localization: shared dynamic display lookup, new English/Japanese UI strings, and the repository locale/placeholder check across all 11 non-English locale files. This was not a human linguistic audit of every translation.
- No applicable `AGENTS.md` was found in the reviewed tree or its `/tmp` ancestors.

## Validation actually performed

| Check | Actual result |
| --- | --- |
| `npm test -- --reporter=dot src/connections src/documents` in reviewed Desk `web` | **18 test files / 236 tests passed.** These are repository-authored tests independently executed during this review. |
| `go test ./internal/desk`, with `GOCACHE=/tmp/jp-authoring-go-cache GOFLAGS=-buildvcs=false` | **Passed**, 78.371 seconds. Initial sandboxed attempt could not open `httptest` loopback listeners; rerun with approved sandbox escalation passed. |
| `npm run typecheck` | **Passed.** |
| `npm run i18n:check` | **Passed.** Each of 11 non-English locale files reported 2222/2222 translated strings and zero placeholder errors. |
| `git diff --check` and final `git status --short` | **Clean.** |
| Independent OAuth/form regression reproduction against unchanged copied production source | **Failed as described in D1.** |
| Same reproduction with illustrative two-condition fix in disposable copy only | **Passed.** The copied production file was subsequently restored to the reviewed head. |
| Independent generic resource reopen/current-pin tests | **3 passed**: retained resource verifies without provider/catalog/network access; changing current authority rejects; changing current public key rejects. |

The independent resource tests are in `desk-repros/web/src/documents/cleanroom-resource-pin.test.ts`; output is `desk-repros/resource-pin.log`. They use the repository's synthetic `signedResource`/test-signing helper to construct valid signed objects; the assertions and changed-pin/offline cases were written independently. This is cryptographic consumer testing, not a real cloud-provider test.

## Limits and non-findings

- No real provider accounts, credentials, paid APIs, messages, merges, or live installations were used. Actual AWS/Azure/Spaces/Dropbox adapters are absent by design and were not treated as missing work in this foundation change.
- I did not run a browser end-to-end test or build a replacement production bundle. The practical gateway-only path was reviewed in source and through the repository's protocol/launch tests, rather than claimed from the drafter's acceptance evidence or the supplied candidate binary.
- I found no demonstrated bypass in the reviewed manifest/source-plan checks or retained resource verification. That statement is limited to the inspected code and tests; it is not a general security certification.
- Future adapters still own real account/scope binding, safe upstream retrieval, durable credential custody, cursor/selection lifetime, one-use grants, and cancellation/revocation semantics. A host-level foundation cannot establish those properties for adapters that do not yet exist.
- The retained resource proof deliberately remains usable without a current provider catalog or active connection, while still requiring the current gateway authority/public key and the signed acquisition/result/arguments/original bindings. The independent tests confirmed those consumer properties for synthetic records.

Reproduction commands and artifact locations are recorded in `desk-repros/COMMANDS.md`.
