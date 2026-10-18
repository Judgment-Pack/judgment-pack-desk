# Exact-version Policy coverage reviews (#483)

Coverage is a private Desk review workspace linking one retained local Policy
publication to one retained Pack publication. It records an administrator's
interpretation of those exact bytes. It does not publish either artifact,
register governing authority, change a job, or authorize a run or action.

The first scope is **Whole published narrative**. Desk creates one explicit
`whole-policy` span over the entire UTF-8 narrative. Markdown headings are not
requirement identities. The four declarations are **Implemented in pack**,
**Manual handling**, **Awaiting interpretation**, and **Out of scope**. Each
requires its corresponding rationale, handoff or explanation; implemented
declarations also select typed elements from the exact Pack. Neither the number
of selected elements nor a successful structural check establishes semantic
completeness. This workspace therefore shows no coverage percentage.

## Saved basis and review

The Policy publication, Pack publication, their exact content references and the
declaration form a retained candidate. Editing creates another candidate and
invalidates its current review while preserving history. Selected releases stay
fixed when mutable drafts or newer publications change. The Policy revision in
review requests is a stale-editor guard, not an atomic check of source freshness
or lifecycle eligibility.

Saving a draft does not require Runtime. Explicit **Check** uses Runtime's
experimental Policy prototype 0.0.2 boundary, introduced by Runtime #294 against
the non-normative Spec #212 profile. It verifies declared structure and exact
references; it does not decide whether the Policy has been implemented correctly.
The pinned Runtime v0.34.0 does not support this boundary. Desk reports missing
support and keeps draft/history access available; it does not infer support from
a version string or substitute a local validator.

**Record review** binds the current authenticated administrator, exact candidate
and comment. Approval repeats the check server-side; a previous browser result
is not a grant. Requesting changes is a review record without an assertion that
checks passed. A local administrator may review their own work, which supports
single-person Desk use but is not independent or team approval.

## Storage and recovery

Coverage has its own bounded optional stream in the existing private Policy
ledger. Its revision and history are separate from Policy draft review and
publication. Exact Pack bytes are retained in the Policy blob closure, alongside
the candidate and review material. Mutable Pack aliases and filesystem paths
are never accepted as a substitute for a selected publication.

Durable save/review commands use the existing operation-ID receipt and read-back
recovery boundary. An uncertain response holds the editor and recovers the
original operation; it does not create a replacement write. Server authorization
and frozen basis checks are repeated before commit. Runtime runs outside storage
locks. Lists/options are private queries; resolving a selection and checking a
candidate are explicit commands and are never polled.

Policy archives include the complete coverage byte closure. Restore preserves
history and marks earlier reviews as historical. It does not recreate the
original Pack publication ledger or revive authority. Retained bytes remain
inspectable/checkable if that publication is absent; recording a new approved
review requires the exact original publication to be present again. No mutable
alias reattachment occurs.

## Existing chat integration

**Discuss review** prepares an unsent request in the existing linked Policy
session. It names an exact saved coverage candidate, preserves Plan mode and
attachments, and asks before appending to a nonempty composer. A bounded read-only
host tool retrieves that candidate under current Policy access and the shared
context/Plan rules. Discussion cannot save, approve or adopt a declaration.
Deleting chat does not delete coverage.

Graph consumers, individually edited narrative sections, several Policies in one
review, team sign-off, governing registration and Runner/Gateway admission are
subsequent contracts. A historical or withdrawn publication can be discussed
without making it eligible for execution.
