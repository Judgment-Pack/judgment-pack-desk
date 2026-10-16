# Shared Runtime Dialect authoring checks

Desk #402 consumes the offline Runtime tools introduced by Runtime #275 and
extended to format 3 by #285. `POST /api/dialect-authoring/check` checks exact
supplied document and optional pack snapshots. It discovers supported formats
from `dialect contract`; executable version strings are not capability gates.

Manual conversion previews, `propose_dialect` in the existing session chat and
Jobs mapping previews use `web/src/dialect/authoring.ts`. A refused check retains
Runtime diagnostics. Incomplete coverage blocks admission; explicitly declared
omissions remain review-required. Checks neither acquire sources nor establish
that values have the right types. Desk conversion checks and Runner binding,
receipt, preparation and evaluation checks still apply.

New or changed recipe qualifications and draft publication recheck on the server
before the existing compare-and-swap write. A historical read, draft autosave,
unchanged qualification or completed proposal replay does not require Runtime.
A proposal never grants permission to save or execute: existing manual review,
Plan phase, Ask me/Auto scopes and action admissions remain authoritative.

The subprocess uses the existing held-directory, environment and timeout guards.
Its private temporary directory is outside the project watcher; only the supplied
pack snapshot is written there, with owner-only access. Dialect bytes arrive on
stdin. Output is capped at 4 MiB to accommodate Runtime's bounded coverage report
and escaped canonical document. Missing/unsupported/malformed/truncated or
interrupted replies fail closed. Exact canonical document, digest and pack digest
are checked before returning an accepted result.

Converted asserted records and reviewed research produce mappings at the server
handoff. Runtime checks and canonicalizes those actual produced documents before
Runner receives them. New version-2 documents explicitly include the historical
`provider: ""`, `facts: null` and `evidence: null` envelope and omit empty sources,
matching Runner's existing digest identity. Historical documents and version-3
imports are not rewritten. Case data and signed acquisition payloads retain
numeric spellings, false, zero and null independently of mapping canonicalization.

Runtime Dialect reference tools are immutable context references. Offline checks
are available in Plan but are never cached as immutable workspace observations.
Starter generation and artifact proposals remain unavailable in Plan. Future
Dialect AI tools must use this check boundary and the existing session,
correction and approval framework; no separate provider or retry loop is added.

Verification covers real Runtime format-2/3 acceptance/refusal, coverage,
large reports, exact case values, authenticated HTTP admission, completed replay,
changed candidates, unavailable Runtime and subprocess fault cases. Jobs tests
pass retained pack bytes, including a JSON integer outside JavaScript's exact
range, instead of serializing a parsed pack object. Runner integration tests
exercise retained records and incremental subscriptions.

The integer `version` on historical format-2/3 documents remains a format
discriminator. Named publications below use a separate artifact release version.

## Named publications and frozen jobs

Spec #180 is implemented by merged Spec #187 (RFC 0021). The named forms use
`formatVersion: "2.0.0"` or `"3.0.0"`, an opaque `id`, and a separate x.y.z
artifact `version`. Integer formats retain their original meaning and bytes.
Runtime supplies all validation, canonical identity and version arithmetic.

Recipe → Pack → Published dialect checks the saved recipe, its exact pack bytes
and the selected imported-record or reviewed-research translation. Explicit
publication retains the canonical document, recipe and pack in the private
artifact repository and the shared immutable version ledger. History is paged.
Unchanged completed requests reconcile from that receipt even if Runtime is
unavailable. A changed candidate or version needs another check; a check does
not grant publication permission. This manual endpoint is not an assistant
Auto/Plan capability. Future chat publication must use a scoped action adapter.

New job reviews use the matching published translation. Once a dialect has a
publication, changed recipes require republishing before another job review.
Imported records use the named family-2 form. Reviewed research uses an unbound
family-3 operation and retains its separate binding and bound execution digest;
its generated origin is not relabeled asserted. Existing integer-format jobs
continue unchanged. Every incremental subscription prepares new records against
its retained release, including when the first or a newer publication appears.
Jobs never silently adopt the latest dialect. Recipe changes still pause a
subscription for explicit review. Shared form edits omit optional empty members
for strict formats; imported JSON is validated unchanged, never auto-repaired.

Real Gateway/Runner/Runtime tests cover both handoff forms, named company →
selected company → people → tested job execution, generated export provenance,
and subscription restart/outbox recovery across later publications. Both local
file and memory repository adapters run the journey. This does not claim a
cloud database backend or acquisition for unimplemented Spec source kinds.
