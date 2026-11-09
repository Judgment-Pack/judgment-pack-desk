# Selected retained-column recipe shape

A recipe qualification can retain an optional `conversionContract` identity with
exactly `contract`, `spec` and `profile`. `spec` has exactly `version`, `revision`,
`schemaSha256` and `corpusSha256`. The identity must equal the installed Agent
package helper (`DialectRetainedColumnDraft` / `DIALECT_RETAINED_COLUMN_DRAFT`).
Operation roles are derived by the host and are never stored in this identity.

The selected logical mapping is version 2, has `profile: "base"`, an empty
`sources` array, and a case with `facts` and `evidence` arrays. Facts have exactly
`target`, `source: "/retained"` and `conversion`. The conversion has
`contractVersion: "1"`, `column`, and one of `text`, `decimal`, `number`,
`boolean` or `research-value` as its `kind`. Only boolean adds both `trueValue`
and `falseValue`, with distinct nonblank spellings. Evidence has exactly
`requirement`, `source: "/retained"` and
`availability: {contractVersion: "1", column}`. Column names and boolean
spellings are Unicode scalar strings of at most 160 UTF-8 bytes; columns need
only be nonempty. Boolean blank handling follows the selected Spec whitespace
list. Conversion sidecars must have the complete same fact-target set and exact
kind/spellings as the document rules. They do not supply executable values.

The pure builders require the caller's explicit exact identity. The extractors
check the logical template and sidecar agreement. Their success does not check
pack coverage, authenticate source history, preserve raw cells, negotiate owner
roles, prepare input, or authorize a save or evaluation. The generated owner must
later materialize these logical leaves in its trusted source read-copy, rather
than treating them as generated case facts.

This initial contract slice keeps public qualification admission closed for
selected tuples, profiles and typed leaves. Public selected save, prepare and
evaluation support requires the subsequent custody, owner and route slices.
Legacy recipes omit every new member; their bytes are not migrated. Their
existing decoding behavior is retained when no new control is present. Explicit
nulls, aliases, duplicates, unknown members and unsupported selected identities
are refused by the selected readers. Call the duplicate-aware text reader
before parsing incoming selected documents; an already parsed object cannot
prove that its original text had unique member names.

The shared handwritten vector at
`web/src/dialects/testdata/retained-recipe-shapes.json` covers every conversion
kind and availability member. Its Go and TypeScript readers also carry explicit
legacy omission and malformed selected-shape cases.
