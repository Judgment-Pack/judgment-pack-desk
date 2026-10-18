# Local policy governance delivery boundary

Status: implementation planning, 2026-10-17. This document describes the local
Library change and the next proposed slice. It does not claim that Policy
storage, review, publication, synchronization or execution admission exists.

## Library foundation

[Desk #451](https://github.com/Judgment-Pack/judgment-pack-desk/issues/451)
tracks the unified Library navigation slice, based on integration/v0.13 commit
`0135ecfbdcbe2fe3e0421fe054ae879f22fc0284`.

The local change merges existing Pack, Graph, Dialect and draft inventories into
the retained Library browser, with All/Packs/Graphs/Dialects filters. Browser
keys are kind-qualified and separate from existing storage/folder keys. The
quick switcher discovers Dialects and their independent drafts. The Library rail
destination covers existing Dialect URLs. Existing detail URLs, artifact bytes,
chat scopes, reviews and execution pins retain their current behavior.

The selected type is an optional workspace-scoped layout preference. The
`jpack-folders.json` v1/v2 assignment contract is unchanged. Dialects have no
folder assignment and are discoverable in Home/all views; they are excluded
from folder moves and chat-backed draft deletion. Their existing editors own
their actions. No Policies filter or governance control is advertised.

## Next bounded local slice

The local installation has one authenticated administrator. Policy authoring,
review and publication may be performed by that same administrator when the
configured workflow allows it. A requirement for an independent reviewer must
be explicit and configurable. Multi-user authorization is a later dependency
for team/guest workflows, not a prerequisite for useful local policy management.

Implement a private Policy application service with a storage interface and its
file-backed adapter. Reuse the existing private-data containment, bounded locks,
atomic file writes and retained-artifact repository. The current
`dialectDraftRepository` is Dialect-specific; policies must not masquerade as
Dialect recipes. Keep the new service separable from its file adapter so future
workspace storage and authenticated resource authorization can replace the
local boundary without rewriting the editor or portable content.

Proposed private service records:

- A stable local policy identity independent of title, folder and chat.
- A mutable draft head with an opaque CAS revision and exact retained content
  reference. Each saved candidate retains immutable content and revision history.
- An explicit local workflow configuration whose revision forms part of the
  review basis. The initial supported workflow can allow the same local owner
  to author, review and publish. Unsupported independent/team workflows must
  remain unavailable rather than silently self-approving.
- Review decisions pinned to the exact candidate and workflow revision, with
  the actor derived from the authenticated server session. An edit retains old
  decisions as history and requires review of the new candidate.
- A separate immutable publication and receipt binding exact reviewed bytes,
  policy identity and selected version. Duplicate versions refuse overwrite.
  Draft saved, reviewed and published remain distinct states.

Proposed commands are `createPolicy`, `savePolicyDraft`, `reviewPolicyCandidate`
and `publishPolicyCandidate`, plus authorized list/read/history operations.
All UI and later chat adapters must call these same commands. Draft saves check
the original revision; review/publication check the exact retained candidate and
workflow basis. Command identity, exact request digest and persisted outcome
must support recovery without repeating a write. Use the shared durable
operation boundary and failure registry; do not create a second permission,
retry or approval system. Publication needs one transaction covering the current
review basis, version uniqueness and resulting head/receipt. Existing file locks
provide local coordination only; they are not a distributed team transaction.

Use the existing editor fields, contextual pane and `UnsavedChanges` registry.
Lists use `privateQuery`; background refresh must not replace an editor's frozen
revision. Save conflict recovery preserves the local buffer and shows the saved
revision for explicit resolution. Chat deletion must never delete policy work.
Manual local authoring can land before AI authoring; later AI proposals inherit
`runAssistantSession`, Plan admission, shared approvals, correction and recovery.
A model cannot manufacture a reviewer identity or treat a successful document
check as publication.

Minimum acceptance evidence: create/save/reload; retained history after chat
deletion; two editors refusing a stale save without losing either candidate;
changed content invalidating current review; publication requiring exact current
review; one configured local owner completing the workflow; independent review
refusing self-approval when configured; immutable version conflicts; lost-write
response recovery; revoked-session refusal; failed persistence protecting exits.

## Inter-repository dependencies and limits

- Spec owns eventual portable Policy/adoption semantics, reference and canonical
  profiles. Private local draft records are not a standardized Policy export.
- Runtime supplies deterministic checks only for a supported, pinned contract.
  The editor must not advertise interoperable validation until that exists.
- Gateway supplies actual connected source observations. Automatic sync requires
  one real supported provider, lifecycle authority mapping, reconciliation and
  gap/freshness handling; a file reader alone is not this capability.
- Runner and actual action executors need retained release/adoption closure and
  authoritative start admission before withdrawal protection can be claimed.
  Policy publication does not upgrade or authorize existing jobs.
- Desk's existing OIDC/password/local session consumer is single-owner
  ([ADR 0007](../adr/0007-authentication-and-identity-boundary.md)). Team and guest
  access require current workspace/resource permissions on every read, search,
  retained chat retrieval, export and mutation, plus appropriate storage and
  Gateway delegation. The local service must not accept client-supplied roles.

Packs/graphs remain executable authority; structured requirements are optional.
No policy-specific evaluator, AI loop, generic workflow builder, remote service
deployment or current-run withdrawal enforcement is part of this Library slice.
