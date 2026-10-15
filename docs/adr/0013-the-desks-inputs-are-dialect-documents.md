---
status: proposed
date: 2026-10-15
deciders: maintainer
---

# The Desk's inputs are dialect documents

Issue #398, steps 1, 2 and 6. This record follows the maintainer's decision of
2026-10-15. It records the shared format of Desk's inputs and the boundary
between authoring that format and preparing or verifying an evaluation.

## Context

RFC 0018 was accepted on 2026-10-15 at draft maturity. Its companion document,
[`spec/dialect.md`](https://github.com/Judgment-Pack/judgment-pack-spec/blob/a550d41/spec/dialect.md),
defines the dialect document, preparation and preparation verifier. Its version
2 is the Runner's mapping v2, byte for byte, with the same digest. Its staged
vectors came from the Runner; no released specification version publishes them
and no preparation-verifier conformance claim is available yet.

Desk already authors mapping v2: case parameters and copies, ordered file and
operation sources, request templates, reads, calculation bindings and target
admissions. It asks Runner for each next request, acquires through the gateway,
and returns the retained response to Runner. Runner prepares the facts and
evidence availability and returns lineage; Desk displays the retained result.

Desk also runs AI-assisted research, retaining answers and source references
with gateway receipts. That research run is not yet a dialect source. Making
it one needs an explicit recipe, a generated profile and the research-record
contract proposed for version 3. Publishing a reusable dialect to the Exchange
needs a form that does not pin the publisher's installation.

## Decision drivers

- One declared input format, shared with the specification and Runner.
- Preserve the version-2 contract, member values and canonical digests.
- Keep authoring, acquisition, preparation and verification distinct.
- State byte lineage without making a claim about a source's truth.

## Decision

### 1. Version 2 now

The Desk's inputs are dialect documents. The mapping v2 document handed to
Runner is the version-2 dialect document of §2, and `mappingDigest` is its
digest. `MappingV2` aliases that document's type so their declarations cannot
drift. A pure `web/src/dialect/` module owns the types and the two canonical
forms, with no React or I/O.

Every mapping handed to Runner is written in §1's canonical form, keeping
optional members exactly as supplied. This sorts members on the wire; the
canonical bytes Runner digests do not change. Preparations use §9.5's separate
form: negative zero stays negative zero, and the additional string escapes
are retained. A writer refuses duplicate member names, malformed Unicode and
numbers outside the integer domain before a digest can conceal the difference.

The ten staged vectors are vendored unchanged in the three files needed for
writer tests. The preparation bytes and digests are checked exactly. The raw
mapping files use Runner's struct order, so their canonical bytes and digests
are compared instead of pretending those files already have sorted members.
CI runs the specification's verifier over a vector rewritten by Desk's writer,
and requires a reading and exit status 0.

### 2. Version 3 later

The research run becomes a `research` source under §4.3 in a later step: a
question and constraints, a named model and required evidence fields, acquired
through a gateway profile of class `generated`. A research record supplied
outside that channel can enter only as a selected file, asserted. This change
declares the proposed types; it does not build that producer or change research.

Publishing to the Exchange later uses the unbound form of §4.4. A published
operation names the catalog platform and operation, shape and record; the
adopting installation supplies its profile bindings. A preparation commits the
bound dialect's digest. It does not commit the published dialect's digest or
the binding document's digest. Nothing is published in this step.

### 3. What Desk establishes, and what it does not

A Desk-authored mapping is a dialect document that a preparation verifier
reading its version and source kinds can read with the required case,
acquisitions and trusted profiles. The writer tests and CI reading establish
agreement on the format and canonical bytes. They make no conformance claim
for the staged vectors, and do not turn Desk into a preparation verifier.

Desk does not interpret a preparation or run a derivation rule. It does not
verify a record signature or replace Runner's authoritative validation. It
does not claim source truth, model correctness, session completeness, operator
intent or that a mapping is suitable for a pack. Section 13's distinction
remains: derivations are recomputable, acquisitions establish byte lineage
under receipts, and asserted inputs remain the operator's say.

## Alternatives considered

- **A Desk-private shape.** It would duplicate the mapping contract and require
  another translation at the Runner boundary. The shared dialect already
  describes the version-2 input Desk produces.
- **Wait for version 3.** It would postpone a common name, types and byte checks
  for a format already implemented. Version 3's research and publication work
  can follow without changing version 2.

## Consequences

- Good: one type and canonical writer for the mapping contract, exercised
  against independent pinned data and the specification's verifier.
- Bad: the vendored corpus and its manifest must be updated deliberately when
  the pinned specification changes; canonical numbers cannot pass through a
  lossy parse before validation.
- Neutral: Runner still prepares, the existing Jobs workflow stays in place,
  and research sources and Exchange publishing remain later work.

## More information

- [Mapping v2 in Jobs](../design/jobs-mapping-v2.md).
- [RFC 0018, accepted](https://github.com/Judgment-Pack/judgment-pack-spec/blob/a550d41/rfcs/0018-dialect.md).
- [Staged vectors and provenance](https://github.com/Judgment-Pack/judgment-pack-spec/blob/a550d41/conformance/dialect/README.md).
