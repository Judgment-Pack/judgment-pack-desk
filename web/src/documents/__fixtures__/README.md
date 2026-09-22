Document record examples from Judgment-Pack/judgment-pack-gateway,
commit f9f05cc72f85ddda32f994b3b3eb2b75dcfc85ab (through PR #137),
`testdata/attachments/examples`. These are producer fixtures, not actual uploads.
The consumer tolerates additional object members, validates the v1 contract,
and independently verifies signed acquisitions before using their text.

`web-snapshot.json` is actual `adapter-web` output from the synthetic TLS fixture
`TestWebSnapshotAndRedirect` at gateway 741620fa11cda18e935bc3df851433aac0d2649b.
It contains only fixture HTML and reported test-process identity; no user uploads,
provider credentials, or personal sources. Signed tests separately bind the
selected URL, snapshot bytes, receipt, seal, and current signer pin.
