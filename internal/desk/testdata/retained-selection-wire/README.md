# Retained selection wire vectors

These are handwritten transport examples derived from the closed DTO and digest
domains, not recorded from the Go/TypeScript writer. The manifest retains each
literal header, row metadata, opaque payload hex, end, length, offset and digest.
The `.wire.hex` files spell complete frames in hexadecimal. Independent Python
standard-library source arithmetic assembled those specified pieces and computed
SHA-256; it never imported or executed Desk/product code. Both language suites
consume every positive, invalid fixture and mutation from the same manifest.

`compact` has one asserted row with exactly two payload bytes `{}`. `lexical`
has two generated rows, including Go HTML/C0/U+2028/U+2029 escaping, quote,
backslash and supplementary Unicode in an ID. Payload bytes deliberately retain
unsafe integer, negative zero, fraction, exponent, whitespace and escaped lone
surrogate spellings. These bytes are opaque transport examples, not actual B3 or
executable fact acceptance witnesses. `invalid-payload-utf8` has consistent
framing/digests for a malformed UTF-8 byte and must refuse.

Mutation edits apply sequentially to a named original fixture. Each edit replaces
`remove` bytes at the current zero-based `offset` with `hex`. Default claimed
content length is the changed byte length; an explicit `contentLength` overrides
it. Optional `recordIds` changes only the captured expected selection. Both
readers must refuse every mutation. Tests also enumerate all compact truncation
points and all two-chunk splits; no expected vector is captured from a writer.
