# Reviewed job upgrades

An upgrade starts from the current immutable job release and revision, resolves the
project pack alias using the pack's canonical identity, and saves a separate durable
Job draft before editing. The candidate carries its base release/revision and the
exact attempted release once a write starts. These fields do not restore approval.

The review compares saved and candidate pack bytes, mapping, source profiles and
published Dialect identity. Existing runs keep their pins. Triggers remain paused;
Dialect subscriptions retain their existing release and need a separate review.

Manual input retains exact JSON numbers and missing/false/zero/null distinctions.
Mapped inputs retain the published Dialect and binding document. Selected files,
picker grants and generated research handoffs are obtained again. Reviewed-record
upgrades use the existing server evaluation-review snapshot, never a browser copy
of signed acquisition bytes. Server revision and project/test guards remain held
through Runner admission.

Runner owns the atomic job CAS and immutable completion receipt. Desk reserves the
review for that exact upgrade, retains the receipt in the shared evaluation journal,
and does not enqueue record execution. A lost acknowledgement is reconciled through
Runner's read-only receipt endpoint before checking changed mutable inputs. Browser
retry and draft reload follow the same ordering, including after later revisions.
Unknown receipt APIs fail closed. Restoring a draft never replays a write.

Validation: all 344 jobs UI tests, TypeScript, locale completeness for all 12 locales,
file/memory evaluation journals, real Runner/Runtime upgrades and evaluation recovery,
read-only proxy recovery, exact numbers and binding identity. Same-vendor clean-room
Codex review completed under the user's explicit exception; findings were corrected
and the final review reported no actionable issues.

## Remaining #404 / #402 work

This completes the existing-job upgrade review/recovery slice. It does not close
all of #404 or #402. The audit found these remaining differences:

- Manual pack edits and Dialect publications share Runtime version arithmetic, but
  compatibility-impact selection and the complete base/candidate review are not yet
  persisted in one reusable manual/chat artifact-review controller.
- Named Dialect publication remains a separate manual action. Chat mapping proposals
  save recipes through the same authoritative checks; they do not publish a named
  artifact or authorize a job upgrade. A future publication tool must use the existing
  shared approval dispatcher, retain candidate/check identity, and stay unavailable
  in Plan until explicitly transitioned.
- `propose_dialect` shares Runtime checks and server CAS, but its failed checks are
  returned as bounded corrective tool guidance; it does not yet persist a failed
  candidate through `correctArtifact` as pack proposals do.
- Existing subscriptions deliberately retain their release. A generic reviewed
  subscription upgrade must compare and freeze source/record semantics separately;
  editing a recipe or upgrading a job does not grant that permission.

These are implementation tasks, not permission inferred from passing tests. The
upgrade changes must not be described as closing either tracking issue wholesale.
