# Storage files implementation checks

Date: 2026-09-28. This is implementation verification, **not** the independent
cross-vendor adversarial review required before merging Gateway's material changes.
Gateway dependency: `759898a587a1d1de602ca5f6301365d9aa10539f`,
[Gateway PR #166](https://github.com/Judgment-Pack/judgment-pack-gateway/pull/166).

## Checks completed

- Desk Go tests and vet; connection/catalog relay race tests.
- Bundle-builder tests and mutation needle guard (927 rows, zero invalid).
- 165 frontend tests across StorageFilesView, connections and locale coverage.
- TypeScript and production web build; all 12 locale catalogs checked, no missing
  messages or placeholder errors. Existing chunk-size advisory remains.
- Gateway adapters tests/vet and connection/CLI race suites; core tests/vet;
  conformance: 30 canon + 41 store vectors, zero disagreements.
- Actual Gateway companion JSON-line protocol: temporary vault, 4 MiB file,
  create/read/update/trash. No user's files or live cloud account were mutated.
- Exact pinned Gateway companion bundle built into a staging directory. Live
  Desk and its runtime binary were not replaced.

## Browser check

Playwright against the local Desk frontend, with **synthetic connection API
responses**. No real provider connection was configured. One list request produced
metadata; opening a file produced one read request. Preparing deletion issued no
commit. Typing `Yes` left Delete disabled; the exact filename is required.
Desktop layout, keyboard resize, narrow-width navigation and light/dark surfaces
were checked. No browser errors or horizontal overflow at 390px.

- [Desktop, dark](desktop-dark.png)
- [Desktop, light](desktop-light.png)
- [Narrow layout](narrow-light.png)
- [Typed delete confirmation](delete-confirmation.png)

The backend provider tests use TLS fakes. They verify S3 SigV4 payload signatures,
conditional requests, Drive multipart uploads and conditional trash; they are not
live AWS/Google qualification. Drive changes refuse a missing version lock.

Additional failure checks cover old selections/plans after reconnect, identical
file bytes at different connections, stale updates, claim replay, uncertain write
recovery, loss of a status response, long local filenames, traversal/symlink
refusal, and bounded request/response lines. New-file destinations remain frozen
when the user browses elsewhere. Unsaved file edits use Desk's existing guard and
browser-tab `*`.
