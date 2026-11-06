# Saved selected input recipes

A recipe explicitly selects retained-column behavior only by retaining the exact
installed `conversionContract` tuple. Its mapping retains the matching profile,
closed conversion and availability leaves, and exactly matching conversion
sidecars. Absence preserves the ordinary recipe contract. A leaf, an installed
component, a check result or a proposed value does not select or authorize an
operation. Saved selection has no caller-supplied operation roles.

Loading a saved recipe validates its local shape and retained pack identity. It
never starts Runtime. Old recipes retain their historical omission, null and
array representations; ambiguous old cells stay readable on the ordinary path
without being promoted into selected custody. Original imports, raw cell states,
review snapshots and recipe revisions are not reconstructed or migrated.

A changed selected qualification passes a fresh selected Runtime document and
pack-inventory check before the existing recipe compare-and-swap. Runtime receives
the exact logical mapping and retained pack, including `research-value` proposals
whose original columns have not yet been collected. Document validity is separate
from per-record research context, typed value validity and readiness. Saving never
constructs an execution profile, creates an acquisition grant or runs a record.
Runtime canonical document identity and a later executable mapping identity remain
separate from the saved logical recipe.

The normal session/store guards, `If-Match`, append-only recipe versions,
uncertain-write refusal and committed readback still own persistence. An edit
that races the fresh check loses the compare-and-swap. Completed chat saves
reconcile before a new check, including during a Runtime outage. An unchanged
qualification keeps the existing save policy; renaming still appends a recipe
revision without converting the earlier revision into execution permission.

Selected public preparation and conversion preview are currently refused before
ordinary conversion or acquisition. Selected named publication, evaluation,
subscription, job upgrade, draft promotion and ordinary assistant mapping updates
are also closed. Their corresponding ordinary owners reject selected controls,
including after restart. Frozen ordinary evaluation batches retain their own
recipe contract when the current collection changes; completed receipts and
history stay inspectable under their existing reconciliation rules.

Opening those routes requires their separate selected owners and qualification.
The complete aggregate response needs exact accounting of every row, raw framing
and metadata, bounded server and browser consumption, cancellation and private
temporary-data cleanup. The Runner's 37,748,736-byte (36 MiB) reply limit
belongs to one preparation and is not a batch limit. Generated preparation also
needs a profile owner that preserves the full ordinary registry. This change does
not introduce an aggregate cap, another companion, a profile eviction, durable
selected execution or a claim of full release parity.


## Inactive retained selection wire codec

The pure Go and TypeScript codecs specify
`application/vnd.jpack-desk.retained-selection.v1`. No handler, browser controller,
spool or selected public route calls them yet. Ordinary preparation keeps its
existing JSON response and limits. The eventual selected owner must require the
saved exact installed `conversionContract`, the current saved revision and this
explicit transport; the Accept header alone does not select retained behavior.

The complete byte stream is `JPACK-RS/1\n`, a big-endian unsigned 32-bit header
length, header bytes, exactly the requested row frames, one end frame and actual
EOF. A row is byte `01`, a big-endian unsigned 32-bit metadata length, metadata,
and exactly `inputBytes` raw Input bytes. The end is byte `02`, a big-endian
unsigned 32-bit metadata length and end metadata. There is no aggregate Input
array, outer JSON string encoding, in-band refusal or partial success.

Metadata is compact UTF-8 JSON with Go `encoding/json` default string escaping:
short escapes and lowercase C0 hex escapes, escaped `<`, `>`, `&`, U+2028 and
U+2029, and other scalar text as UTF-8. Key order is part of the wire contract:

- Header: `wireVersion`, `viewId`, `preparationId`, `revision`, `packDigest`,
  `conversionContract`, `origin`, `rows`, `maxInputBytes`, `selectionSha256`.
- Contract: `contract`, `spec`, `profile`; spec: `version`, `revision`,
  `schemaSha256`, `corpusSha256`. Identity comes from the installed package helper.
- Row: `index`, `id`, `inputBytes`, `inputSha256`.
- End: `rows`, `inputBytesTotal`, `indexSha256`.

The wire version is integer 1. A view ID is `rv_` plus 48 lowercase hexadecimal
characters; a preparation ID retains its existing `prep_` plus 32 hexadecimal
shape. The exact bare revision SHA binds the saved recipe representation. The
pack digest and tuple must equal the captured source. `origin` is `asserted` or
`generated`; count and order exactly match the captured selection of 1 to 1000
unique saved IDs. `maxInputBytes` is 37,748,736, the existing Base per-row reply
owner. Each payload is nonempty, valid UTF-8 and no larger than that owner; B3
still owns JSON validity, source custody and any tighter negotiated constraints.
The codec treats payloads as opaque bytes. It preserves whitespace, exponent,
fraction, negative-zero and unsafe-integer spellings without parsing or rewriting
Input. A synthetic codec example does not establish B3 or executable acceptance.
There is no recipe-version metadata member or new bound on saved numeric data;
`recipeVersion` is unknown metadata and refuses.

All SHA-256 strings in framing are bare lowercase 64-hex. Selection digest input
is UTF-8 `jpack-desk/retained-selection/1:ids` followed by one NUL, then u32 count,
then for each requested ID u32 UTF-8 byte length and those bytes. Index digest
input is UTF-8 `jpack-desk/retained-selection/1:index` followed by one NUL, the raw
32-byte header digest, u32 count, then each row's u32 index, u32 UTF-8 ID length,
ID bytes, u64 payload length and raw 32-byte payload digest. All integers are big
endian. Digests establish byte consistency and completeness, never authenticity
or permission to acquire, execute, publish or write.

Both readers require exact canonical metadata re-encoding. Duplicate, unknown,
null, reordered, escaped-key and otherwise noncanonical representations refuse.
Readers check framing lengths against DTO-derived limits before allocating:
the header uses the exact captured source and a fixed-width view ID, row metadata
uses that exact requested ID and the fixed per-row owner, and end metadata uses
the checked maximum input total. Decoded ID bytes must fit the captured request's
actual byte count within the existing 2 MiB request owner. There is no arbitrary
per-ID/header allowance or new smaller aggregate cap. Counters use checked int64
in Go and checked safe wire integers in TypeScript; these checks never inspect
or restrict numbers inside saved data or raw Input.

For row metadata, `M = 111 + digits(index) + digits(inputBytes) + QGo(id)`.
Header bytes are `H = 412 + QGo(preparationId) + T + QGo(origin) + digits(N)`,
where T is the exact encoded tuple length. End bytes are
`F = 109 + digits(N) + digits(sum inputBytes)`. Complete stream bytes are
`20 + H + F + sum(5 + M + inputBytes)`. These are accounting identities, not
resource qualification. Readers require the measured content length, all
payload hashes, final count/total/index digest and actual EOF with no trailing
byte. The eventual HTTP owner must validate media type, encoding and success
headers separately and must not widen generic response or storage limits.

The Go incremental writer accepts one Input at a time, reports its raw payload
offset and never retains payloads. Its sink is unusable after any failure. The Go
reader invokes a provisional callback for each verified row and returns only a
bounded descriptor index after EOF. The TypeScript writer awaits each sink write;
the reader pulls one stream chunk at a time and awaits each provisional callback
before progressing. An optional provisional header callback exposes a validated
view reference for best-effort cleanup after a later failure, never readiness.
It owns one row buffer and a request-bounded index digest
material buffer, never an aggregate Input array. UTF-8 validation decodes only
bounded pieces. A callback must release a payload before the next row and cannot
mount, store the aggregate or announce readiness before complete reader success.
A failed or truncated response is unusable; completed acquisitions may already
exist, so transport failure never authorizes automatic full-batch replay.

Shared handwritten fixtures in `internal/desk/testdata/retained-selection-wire`
are consumed by both languages. They include independently calculated complete
framing/digests, opaque lexical examples, exact Go escaping, consistent-digest
malformed UTF-8 and shared metadata/framing/count/hash/order mutations. The
compact positive is read across every two-chunk split and every truncation point.
Fixture buffers accumulate only in tests; they are not maximum-domain or live
preparation evidence.

Subsequent work still owns anonymous private spool admission/seal/readback,
complete successful delivery before a session-bound ephemeral view becomes
readable, authenticated row reads/close/revocation, browser lifecycle and actual
complete-stream resource measurements. No browser persistence, durable artifact,
selected execution path or release qualification is introduced by these codecs.
