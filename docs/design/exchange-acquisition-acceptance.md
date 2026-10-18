# Signed Exchange acquisition acceptance

Desk #446 extends the separate answer-only signed-release acceptance with real
search and page acquisition bodies. The three exact signed Exchange v0.2.0 preset
files are imported on two disposable installations. Their complete definitions,
`gemini-2.5-flash` model, capabilities, constraints, field declarations and status
forms remain unchanged. All installation choices live in reviewed bindings.

`TestExchangeSignedReleaseAcquisitionsOnTwoInstallations` uses actual attested
Runtime v0.34.0 and Runner v0.21.0 executables and the released Gateway v0.20.0
signer/control programs. Only the two adapter role slots use Gateway's separately
pinned, explicitly tagged fixture executable from
`4b9fa82ddeae772654d8f8bcf7b33d47ac65a512`. Its real executable digest and
`fixture.4b9fa82ddeae` version appear in reviewed profiles and genuine receipts.
The fixture source pin remains independent of the official component release lock.

Each case reviews/registers local model, search and scoped page profiles through
the existing producers. Different installations have different profile digests
and signing keys. Plan refuses confirmation without acquisition. After an
explicit Work transition, the shared durable worker performs:

1. A genuine model acquisition asking for search.
2. A genuine managed-search acquisition with three fixture discovery links.
3. A genuine model acquisition choosing a complete URL from the retained search.
4. A genuine page acquisition of that discovered URL.
5. A genuine model acquisition returning typed fields and actual citation indices.

The six main cases therefore consume **18 model requests, 6 searches and 6 page
reads**, with **zero decisions**. Each confirmed review grants exactly three model
requests, one search and one page; durable usage and retained order are checked.
The model fixture reads the actual shared retained context, not a fabricated
response list. Company discovery includes the literal URL
`https://example.com/fixtures/company?from=search%2Ffixture`; the worker preserves
that entire submitted value in arguments and attachment provenance. Query removal
applies only to scope matching/final endpoint identity.

All values describe invented fixtures. Company search says `Fixture District`,
while the acquired page says `Fixture City`: `location` stays `conflicting` with
distinct actual search index 1 and page index 3 citations. No observation is chosen
or invented. The page supplies the integer range `{min:10,max:20}` and its stated
2026-01-01 date, professional name/role and published notice status. The absent
notice deadline stays `not-found` with no value and no citations. Found values
cite the acquired page at index 3. Prepared facts must preserve all types,
statuses, values and citation arrays identically on both installations.

Runner prepares against all five genuine receipts. Its offline reader verifies
the retained preparation twice without acquisition. Raw preparation file pins and
canonical preparation identity are distinct checks. `generatedInfluence:true`
is retained; offline replay reports `binding:not-checked`, so no decision/run
binding is claimed. Cross-installation response transplantation fails as
`profile-mismatch`. Completed scans and a real Desk shutdown/restart retain all
proofs without reacquiring; the private search budget still records three reads.

Gateway receipts carry statement/snapshot commitments. Desk asserts their shape,
preserves the request and attachment's complete URL, and delegates receipt
verification to Runner. Gateway's own fixture tests cover the pre-signing raw
statement and exact response snapshot. This acceptance does not reproduce a
commitment verifier or change retained requests, receipts or preparation files.

`TestExchangeAcquisitionScopeAndRedirectRefusals` adds two separate generic guard
cases through the normal reviewed worker. An out-of-scope submitted URL is refused
before page acquisition or page allowance consumption. A scoped submitted URL
that redirects to `/outside/company` produces a genuine page receipt whose signed
final endpoint lies outside the scope. The worker's receipt check refuses it at
once: the page attempt is kept `refused` (`acquisition-invalid`) with its genuine
receipt for audit and its page allowance spent, and the page never reaches the
model. The test reads the second model request itself: it carries the
`acquisition-invalid` finding and no text or address of the redirected page.
Both cases then complete from the model's own generated answer, whose retained
responses are the two model responses alone, and a scanner pass spends nothing
further. Runner's check 3 still holds every retained receipt at preparation; it
is not exercised on the refused page, which is never retained as a source. These
extra guard cases consume **4 model requests and 1 page read**, zero searches and
zero decisions. They are not part of the six main case totals.

The existing required Jobs companion job runs these shared guard tests, which
retain their own generic fixtures and acquisition counts:

| Boundary | Existing coverage |
| --- | --- |
| Changed model binding before dispatch | `TestPortableResearchStaleConnectionBindingRefusedBeforeAcquisition` in [portable_research_acceptance_test.go](../../internal/desk/portable_research_acceptance_test.go) |
| Stale installed page adapter identity | `TestPortablePageBindingActualCompanionRejectsStaleAdapterBeforeAcquisition` in [portable_page_binding_test.go](../../internal/desk/portable_page_binding_test.go) |
| A page refused at its receipt check is not requested again | `TestPortableScopedPageRefusedAtItsReceiptIsNotRequestedAgain` in [portable_page_binding_test.go](../../internal/desk/portable_page_binding_test.go) |
| Plan before web dispatch | `TestPortableResearchSwitchToPlanBeforeWebDispatch` in [portable_research_test.go](../../internal/desk/portable_research_test.go) |
| Explicit model/page allowance pause and reviewed resume | `TestPortableResearchExplicitBudgetPauseAndResume` in [portable_research_test.go](../../internal/desk/portable_research_test.go) |
| Interrupted/stopped provider call, retained completion and uncertain request recovery | `TestPortableResearchInterruptedCallKeepsCompletedAndNeverReplays` in [portable_research_acceptance_test.go](../../internal/desk/portable_research_acceptance_test.go) |
| Completed operation canonical identity/repetition refusal | `TestPortableResearchCanonicalRequestReusesCompletedOperation` and `TestPortableResearchRepeatedCompletedSourceStopsCorrection` in [portable_research_test.go](../../internal/desk/portable_research_test.go) |
| Fitted context and retained-source reads | `TestPortableResearchContextIsFittedAndReadFromRetainedSource` in [portable_research_acceptance_test.go](../../internal/desk/portable_research_acceptance_test.go) |

All provider/source content is synthetic and stays on loopback. The Gateway
fixture harness delegates to its production acquisition bodies with injected
network dependencies. It does not cover shipping CLI parsers, official adapter
binaries, live provider accuracy, real public website access, source truth,
workflow fan-out or qualification decisions. No product provider loop, permission
override, receipt fabrication or private-network shipping option is added.

Run with operator signature trust configured for the exact signed catalog:

```sh
JPACK_EXCHANGE_ACQUISITION_ACCEPTANCE=1 \
JPACK_EXCHANGE_ACCEPTANCE_CATALOG=/path/to/exchange \
JPACK_EXCHANGE_ACCEPTANCE_TAG=v0.2.0 \
JPACK_EXCHANGE_ACCEPTANCE_COMMIT=565ddf477c7ed986906409f19bcf8fccfaafa8a5 \
JPACK_EXCHANGE_TEST_BIN=/path/to/published/jpack \
JPACK_EXCHANGE_TEST_RUNNER=/path/to/published/jpack-runner \
JPACK_EXCHANGE_COMPONENT_PROVENANCE=/path/to/published/component-artifacts.json \
JPS_TEST_GATEWAY_BUNDLE=/path/to/clean/pinned-gateway-fixture-bundle \
go test -race ./internal/desk \
  -run '^TestExchange(SignedReleaseAcquisitionsOnTwoInstallations|AcquisitionScopeAndRedirectRefusals)$' \
  -count=1 -v
```

Opt-in requires every explicit input and verifies published component bytes plus
the clean fixture's separate pinned identity before approving only its exact
manifest for this test. Default runs visibly skip; CI explicitly opts in and
requires both tests and all six signed subcases to report PASS. Missing inputs,
signature trust, fixture identity or released artifact agreement fail; no
substitute build/provider is selected.
