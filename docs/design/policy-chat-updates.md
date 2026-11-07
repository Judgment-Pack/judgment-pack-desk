# Sparse Policy chat updates

Desk #627 changes the host-owned `propose_policy_update` input. `revision` is
required, alongside at least one of `title`, `narrative` or `artifactVersion`.
Omitted fields mean unchanged. Explicit empty strings retain their existing
meaning and bounds. Null, unknown/case-alias fields, wrong types, missing or
foreign revisions and revision-only objects are refused. Complete-field callers
remain valid; supplying an equal value does not grant any new effect.

Browser and private Go readers construct the complete candidate from the exact
held request-time candidate and supplied fields. They never fill omissions from
newly fetched mutable state. A later saved revision still marks the complete
proposal stale and prevents save until a fresh request. Durable receipt readers
reconstruct the same candidate from the held intent and reject substituted
omitted fields. Prior complete-field receipts retain their meaning.

A title-only model call omits the unchanged narrative. The full 131,072-character
narrative remains in saved/request context and the complete retained proposal.
This avoids unnecessary outbound copies while keeping Agent's canonical output
guard unchanged. Requested large replacement values still pass through that
guard and its documented limits.

Plan and child proposal restrictions, current authority, exact revision/CAS,
review and save approval remain in force. The tool only retains a review card;
it does not save, approve, publish or alter workflow. Shared handwritten vectors
in `internal/desk/testdata/policy-sparse-updates.json` are read by both languages.
Actual managed API/native fixtures retain the complete large candidate; stale,
malformed, Plan, cancellation and receipt-substitution checks cover host custody.
