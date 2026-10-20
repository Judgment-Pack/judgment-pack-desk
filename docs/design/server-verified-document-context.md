# Private verified document context

The private server reader loads a saved attachment in the selected Desk's rooted
conversation namespace. Saved document references, browser text and storage
checksums never establish page verification. This capability performs no upload,
extraction, acquisition, permission grant or network request.

`readAssistantDocument(request, source, reference)` accepts the existing
host-resolved `assistantRunSource` and saved document-reference shape. It returns
selected page numbers, exact text and the existing
`attachment:<id>/<extraction digest>/page/<number>` citation identities.
`contextDocument()` projects those pages into the canonical Agent context document
shape used by `web/src/chat/attachmentContext.ts`; the original and unselected page
text do not enter that value. A caller retains this document as attachment data,
not instructions or evidence of action authorization.

The reader obtains its own short private-storage lock. It reauthorizes the current
session and Policy owner with the existing run resolver, matches the exact saved
owner, request, project and reference, and reads the packet under the same lock.
It obtains the namespace directly from the rooted store: calling the locking run
store constructor from inside this transaction would acquire a nested lock.
Cryptographic verification runs outside the storage lock. The reader rechecks its
snapshot before returning. Callers must also call
`recheckAssistantDocument(request, snapshot)` immediately before each model
request, together with the owning run's admission check. The run host assigns execution key/unit after preparation; the reader mirrors the
existing fresh-source fence for those two fields, while run admission retains
their authorization. The recheck compares the
current owner/request binding, exact reference, complete current Gateway selection
and attachment byte digest. Changed bytes, including a changed extraction packet,
require a new verification; a matching canonical value does not preserve the old
storage revision. The snapshot retains compact owner/request digests rather than copies of the saved
chat for each document. The snapshot and recheck are private host facilities and cannot
be supplied as browser authorization callbacks.

Verification uses the integrity-checked installed Gateway bundle and its published
offline `canon` and `verify <store> <registry> <authority>` commands, with the public
key on stdin. Temporary verification files contain one acquisition receipt and
its canonical result. The registry verdict is explicitly scoped to that held
session. The Gateway verifies the receipt signature, key identity, authority,
sequence/chain, canonical result identity and first loadable signed session seal
with the held count. Desk additionally checks the exact argument commitment,
original checksum and bytes, acquisition shape, source provenance, attachment-v1
page contract, selected readable pages and explicit consent for partial extraction.
Subprocess input/output, storage bytes, registry lines, page counts and text bounds
are checked; cancellation produces no pages. Original and retained response bytes
are never rewritten. Missing local verification components fail closed.

The consumer page contract mirrors `web/src/documents/record.ts`; argument and
provenance bindings mirror `verifyDocument` in `web/src/documents/client.ts`.
Supported fixtures cover inline command extraction, Drive/Gmail HTTP, Notion MCP,
Obsidian command, connection-resource command/HTTP/MCP, web text snapshots and web
PDFs, including OCR. Unmapped glyphs and non-readable page states are withheld.
An intact receipt authenticates acquired bytes and lineage, not the truth or
currency of page assertions.

The hand-authored `internal/desk/testdata/assistant-document-v1.json` is the common
record input. Test signatures use an independent, ASCII-key/integer fixture
serializer and the published receipt/3 and seal/2 contexts, not the consumer's
canonicalizer or verifier. `TestAssistantDocumentSupportedSources` can export
these independently constructed inputs with `JPS_DOCUMENT_REFERENCE_FIXTURES`.
Bundle `scripts/assistant-document-reference-check.mjs` with the repository's
esbuild and run it against that directory to check the existing browser verifier's
exact selected page/citation and tampered-result refusal for all ten source/shape
inputs. No expected page text or verdict is recorded from code under test.

Offline cryptographic tests require `JPS_TEST_GATEWAY` plus
`JPS_TEST_DOCUMENT_BUNDLE`, or the existing `JPS_TEST_GATEWAY_BUNDLE` environment
used by companion acceptance tests. These tests skip when that prerequisite is
absent; a skipped test is not evidence of parity. The `assistant documents:`
mutation rows require the same prerequisite and must be run with it. Ordinary Go
unit checks alone therefore do not satisfy this integration gate. The
`assistant-saved-context` companion check requires primary cryptographic and
integrated API/Codex tests to PASS and runs the independent browser probe. Changed
Go mutation rows and Go nightly shards prepare the same repository-pinned offline
Gateway fixture; neither borrows an installed executable.

The private saved-chat factory now nominates only references on submitted user
turns. Its source parser checks shape without crypto or recursive storage locks.
The factory verifies nominated references outside the source transaction and
retains snapshots in pointer-owned execution custody shared by reconstructed
capabilities. Every physical API/Codex dispatch rechecks that custody, after waits
and before child/parent continuation, and cancels the root on stale authority,
bytes or trust. Exact repeats deduplicate; different page or consent tuples retain
distinct canonical Document IDs, without combining pages or upgrading consent.
Original page citation strings remain unchanged. Composer-only references and
saved inline text cannot supply verified content.

Independent security review of this integrated private path remains required.
Production registration, domain actions, response journal CAS, browser migration,
complete binding certification and the complete Agent cutover remain pending.
