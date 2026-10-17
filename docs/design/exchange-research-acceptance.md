# Signed Exchange research preparation acceptance

`TestExchangeSignedReleaseResearchPreparationOnTwoInstallations` extends the
separate signed-release import acceptance with fresh portable research and offline
preparation replay. It is opt-in and uses disposable Desk, Runner and Gateway
installations plus loopback deterministic Gemini test providers.

The catalog path, signed tag and expected commit are explicit inputs. The test
verifies the tag and reads the entry files from that exact Git commit. It imports
and retains each preset's exact `entry.json`, `dialect.json` and `README.md` bytes;
no entry is copied into this repository, rewritten or retagged. Stored-file digest
pins and Runtime canonical definition identities are checked separately.

The three Exchange v0.2.0 presets keep their published `gemini-2.5-flash` model,
`answer`/`search`/`page` capabilities, fields, constraints and evidence instructions.
Each installation registers its own reviewed model, search and scoped page
profiles through the existing trusted producer routes. All three binding digests
and the Gateway public key differ between installations. Installation assignments
live in the import binding; the published definition remains unchanged.

For each preset on each installation, the test:

1. Imports the signed preset and verifies its retained files and definition.
2. Saves a portable review with an allowance of one model request, one search and
   one page read. Plan rejects confirmation without acquiring anything.
3. Switches the saved conversation to Work and confirms the exact review. The
   durable shared worker, execution/context boundary and Gateway model adapter
   perform one request to the local Gemini test provider.
4. Retains the genuine Gateway acquisition receipt and returns every declared
   field as `not-found`, with no invented value or citation. The saved usage is
   exactly one model request, zero searches and zero page reads.
5. Prepares inputs with Runner and replays the retained preparation twice through
   its installed offline `verify-preparation` interface. The raw preparation file
   pin and canonical preparation identity are checked as separate values. The
   replay reports `binding: not-checked`; no decision/run binding is claimed.
6. Confirms completed-worker scans and offline replay spend no additional provider
   requests. A first-installation response transplanted into the second
   installation's binding is refused as `profile-mismatch`.

The complete test exercises **six model-answer acquisitions**: three presets on
two installations. Search and page profiles are declared, reviewed and bound, but
their acquisitions are unused. Search connection status remains unchanged. This
does not establish live search, page reads, redirects, found-value extraction,
source truth or qualification decisions. Deterministic live-free search/page
acquisition needs a separate adapter/network fixture seam and acceptance task.

Run from the Desk repository with the operator's Git signature trust configured:

```sh
JPACK_EXCHANGE_RESEARCH_ACCEPTANCE=1 \
JPACK_EXCHANGE_ACCEPTANCE_CATALOG=/path/to/exchange \
JPACK_EXCHANGE_ACCEPTANCE_TAG=v0.2.0 \
JPACK_EXCHANGE_ACCEPTANCE_COMMIT=565ddf477c7ed986906409f19bcf8fccfaafa8a5 \
JPACK_EXCHANGE_TEST_BIN=/path/to/jpack-runtime-034 \
JPACK_EXCHANGE_TEST_RUNNER=/path/to/jpack-runner-021 \
JPS_TEST_GATEWAY_BUNDLE=/path/to/descriptor-enabled-gateway-bundle \
go test ./internal/desk \
  -run '^TestExchangeSignedReleaseResearchPreparationOnTwoInstallations$' \
  -count=1 -v
```

The test approves the exact supplied Gateway manifest only for its own lifetime,
using the existing development-bundle test seam. It does not change installed
application data or global companion trust. Without the explicit research
acceptance opt-in it skips; a partial opt-in fails instead of substituting a
different catalog, model, adapter identity or live provider.

The Jobs companion CI job runs this acceptance and the separate import check
against Exchange commit `565ddf477c7ed986906409f19bcf8fccfaafa8a5` and signed tag
`v0.2.0`. Its public SSH verification key is pinned in
`.github/testdata/exchange-release-allowed-signers` and configured only on that
disposable catalog repository. This fixture trust does not authorize application
imports, profiles or future releases. CI requires both tests and the scoped-page
companion tests to report a pass, so a skipped prerequisite cannot turn green.
