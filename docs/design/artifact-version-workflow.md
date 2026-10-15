# Pack and Dialect artifact versions

Status: agreed product direction; shared-contract and authoring implementation are
tracked in [Spec #180](https://github.com/Judgment-Pack/judgment-pack-spec/issues/180)
and [Desk #404](https://github.com/Judgment-Pack/judgment-pack-desk/issues/404).
These issues are assigned to Brian Jin. The new portable format is not enabled.

## What the audit found

A pack's `version` already uses exactly `MAJOR.MINOR.PATCH`, without leading zeroes,
prerelease suffixes or build metadata. Its `specVersion` is independent. Desk
accepts the version supplied by a form or checked AI proposal. The shared Runtime
arithmetic tool and Desk's capability-gated pack editor now provide patch, minor,
and major choices from the saved base. The reviewed publication and job-upgrade boundaries now retain exact versions,
as described below. Dialect `version: 2|3` identifies its document format, and
Desk's `recipeVersion` counts local recipe changes. Those are different identities.

## Implemented authoring slice

Runtime PRs #276, #278 and #279 provide offline Dialect encoding, version-2 document
validation, authoring resources and declared pack coverage. PR #281 adds strict
x.y.z validation, exact comparison and all three bump choices through
`get_artifact_version_options` and `jpack artifact version`. Numeric arithmetic
does not determine compatibility or grant save permission.

The pack editor discovers the version tool and requests choices from its frozen
saved base. Choosing a value edits the existing draft; repeating a selection does
not increment an already proposed value. Existing draft edits remain intact, and
Save retains the existing write guards. Reconnecting or changing the saved base
invalidates old requests. Older Runtime installations retain manual version entry.
The read-only tool follows the shared Plan and context policies, and all eleven
non-English catalogs include the controls.

Independent same-vendor Codex review and local integration checks cover this work.
These capabilities do not imply a new stable release.

## Reviewed publication and job upgrades

The existing **Review & lock** action is Desk's publication boundary. Saving a
file remains draft authoring. On confirmation, Desk retains the configuration and
all reviewed pack/graph bytes in its private content-addressed repository, then
locks exactly that snapshot and records an immutable publication receipt. The
ledger binds `(kind, id, version)` to exact bytes. It refuses conflicting identity
reuse and requires a higher Runtime-verified version when replacing an existing
artifact alias or path. Adding a reference to an existing immutable version is
allowed. Restoring older content into an existing alias needs a new version.

Retention precedes locking so a process interruption cannot lose the bytes of a
published lock. The next confirmation reconciles the current public lock into the
ledger before attempting another publication. A failed or uncertain write is
reported without replay. The ledger and content are private, bounded, and ignored
by Git. History exposes retained versions with verified bytes and bounded stable
pagination; it never converts an unavailable store into an empty history.

Jobs remain pinned until **Upgrade job** completes the existing checked-release
wizard. Runner requires an explicit review plus the current job revision/release,
retains every creation and upgrade receipt, and leaves older runs pinned. Pending
source occurrences block the upgrade. Existing triggers must be paused and stay
paused with a new trigger revision; resuming a trigger needs a fresh review.
Connected Dialect subscriptions need a new subscription review. The UI discovers
the `job-upgrades` capability and hides this action on older Runner versions.

Runtime owns strict numeric version operations; Desk and Runner own their separate
publication/admission policies. None of these operations infers compatibility,
automatic major bumps, permission to write, or passing expected outcomes. Plan,
Ask me/Auto, and the shared AI correction framework retain their existing guards.

The new portable Dialect artifact identity still depends on Spec #180. First-job
promotion and the impact policy after job use must be settled separately. Current
integer Dialect formats 2 and 3 are preserved.

## Shared workflow

1. Start an edit from a saved artifact identity, exact content and storage revision.
   Autosave preserves the draft; it does not publish another artifact version.
2. Review a proposed Patch, Minor or Major bump, displaying the saved and proposed
   values. For `0.1.0`, these are `0.1.1`, `0.2.0` and `1.0.0`. The increment uses
   exact integer arithmetic. A new artifact can start at `0.1.0`.
3. Review the impact independently of the number. A compatible correction suggests
   patch, an additive compatible feature suggests minor, and a breaking change
   suggests major. A policy change may be breaking even when its fields and tests
   are unchanged. Document the compatibility promise during 0.x; never infer it
   from passing tests or a model's confidence.
4. Bind validation, tests, diff and approval to the complete candidate, including
   its proposed version. Changing the candidate invalidates prior checks.
5. Save through the existing Ask me/Auto framework and an authoritative storage
   guard. A version choice is not permission to write. Refuse stale bases and
   changed content under an existing published identity. Identical completed
   publications reconcile without a second write. Uncertain writes stop for
   reconciliation; they never trigger an automatic retry.
6. Keep saved versions available. Restoring earlier content creates a new version.
   Jobs and subscriptions keep their reviewed version and digest until an explicit
   upgrade is reviewed. Research results, record imports and job runs do not bump
   the reusable definition.

## Format version and storage revision

The new Dialect format needs separate members for its format discriminator and
artifact identity/version. `formatVersion` is a proposed field name, not a current
Spec member. Both existing format families need explicit treatment; supporting a
string `"2.0.0"` must not silently discard version-3 sources or bindings.

Spec owns this decision and the canonical identity rules. Runtime supplies the
canonical writer, validator and digest/version tooling; Runner pins execution to
those artifacts; Exchange enforces publication identity. Desk owns the shared
review/save experience. Gateway changes only where its actual references require
it. Integer operation counts, local revision tokens and cryptographic digests do
not become semantic release versions.

A fresh local Desk workspace removes a local-data migration requirement. It does
not permit rewriting historical published schemas, canonical vectors or signed
receipts. No workspace data has been deleted. The reset follows a validated writer
and reader rollout and must preserve credentials, signing keys and installations
unless explicitly included in a separate reset scope.
