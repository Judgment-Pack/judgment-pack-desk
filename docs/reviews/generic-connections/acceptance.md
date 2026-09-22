# Gateway-only provider acceptance

The synthetic replacement test keeps the Desk executable byte-identical, replaces
only its installed gateway companion bundle, and approves that exact manifest.
The provider `fixture-files` appears nowhere in production Desk handlers. Its
catalog, setup schema and source launch plan are supplied by the replacement.

The browser configures Folder/Access key, displays the resource scope and file
limits, disables archived/oversized entries, pages to Policy.txt, and receives a
real gateway-signed acquisition of bytes produced by the generic resource
helper. Desk persists the original and resource proof and displays Receipt
verified after verifying the current pin, sealed receipt, source/selection,
grant commitment and retained bytes. The draft and composer focus survive;
zero chats are saved before a first message. Disconnect confirms local removal
and warns that provider access may remain. See findings.json and screenshots.
The fixture key is synthetic and grants access only to the test adapter.

The existing Obsidian flow also passes: local setup, search, selection,
attachment, retained verification after disconnect/source edit, route dismissal,
and the narrow layout in all twelve locales. See existing-findings.json and
narrow-ja.png. The script waits for initialization after each language reload
before opening the responsive drawer.

Reproduce with a built candidate bundle and synthetic runtime project:

```sh
DESK_BUNDLE=/path/to/candidate JPACK_BINARY=/path/to/jpack \
JPACK_FIXTURE_PROJECT=/path/to/runtime/internal/graph/testdata/project \
CHROMIUM_PATH=/path/to/chromium SMOKE_OUTPUT=/tmp/generic-review \
node scripts/check-generic-connections.mjs
```

Use the same environment with scripts/check-connections.mjs for existing-source
and twelve-locale acceptance. The generic driver builds its own fixture
companion from scripts/fixtures/generic-connection.go; that file is excluded from
normal Go builds and never bundled in a release. Neither script contacts real
provider accounts or a model API.

These are implementation acceptance records, not an independent adversarial
review. New cloud provider implementations and their live authentication are
not covered by synthetic acceptance.

## September 22 review follow-up

- Full frontend: 178 files, 3,883 passed and one existing skip. An additional
  unsupported-query-mode case and its 13-test catalog file pass separately.
- Production build/typecheck, full Go backend and vet pass. The pin/catalog
  checks pass after pinning gateway a36190272b012dd5f9d4e5fcaab55a5421fc2eaf.
- All 2,222 messages are translated in all twelve locales with no placeholder
  errors. The 926 historical needle guards remain valid; these are not claimed
  as new generic-contract mutation coverage.
- Eleven focused behavioral mutations span gateway and Desk. The first pass
  exposed a non-isolated item-count test; after correcting it all eleven mutants
  were killed. Reproduction script, initial/final results and limitations live
  in gateway docs/reviews/generic-connections/.
- The resource-v1 signed consumer fixture now includes actual PDF output from
  the gateway producer, in addition to text. No per-cloud PDF branch is needed.

Review bundle manifest SHA256:
`a274dedfc0de76f65f6fdfd0dd41ea1bed9164d49b0ae52b3dee38ab9535f84f`.
This bundle was built without a publisher Google registration and was not
installed over the running Desk. Browser servers used temporary synthetic
projects on separate ports.
