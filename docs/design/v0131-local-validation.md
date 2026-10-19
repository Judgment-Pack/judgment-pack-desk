# Local v0.13.1 Policy smoke checklist

This is a checklist for explicit testing after the release is available, not a
record of completed native acceptance. It complements the retained
[Dialect → Pack → Job workflow evidence](v013-local-validation.md#isolated-installation-upgrade-and-retained-journey).
Use the existing local Desk UI and a clearly named new test Policy/Pack. Preserve
old data, Jobs and their frozen releases. These steps do not authorize source
connections, model/provider requests, outreach, enrollment or migration.

1. Confirm the installed Desk release and its Runtime component match the
   published release record. Record exact versions before testing. Open existing
   Policy/adoption history read-only; retain its original report references.
2. In Library → Policies, create a new Policy draft or import a small local test
   document. For an import, inspect retained original bytes separately from the
   authored narrative. Save, reload and confirm the saved candidate and version.
3. Review the exact Policy candidate, then explicitly approve and publish it.
   Confirm that draft saving alone did not create approval or publication.
4. Choose an existing appropriate test Pack publication with exact saved bytes.
   In the Policy's coverage view, select that publication and declare the
   whole-narrative handling using the existing controls. Review the exact
   statement/reference inventory, run its coverage check and explicitly record
   the coverage review. Prototype coverage remains legacy evidence; it does not
   constitute official draft adoption or permission to execute.
5. Open the Pack's Governing policies view, select the same exact publication
   and the complete intended approved coverage set, then prepare adoption.
   Inspect the immutable candidate, source/material digests and retained check
   reference. Record that fresh Runtime output uses official Spec
   `0799b37b74e35c4a86bfbf70f7bee80bdb7bbe94`, maturity `draft`, format `1.0.0`
   and complete successful result semantics. A completed check is not approval.
6. Explicitly review the whole saved adoption candidate. Confirm that approval
   causes a fresh check of the unchanged exact document and current complete
   basis. Reload and verify retained review/history. A later draft edit or stale
   selected basis must require renewed review rather than carrying approval.
7. If an isolated test installation with an older Runtime is already available,
   verify the actionable required-draft message and retained selections/history.
   Do not replace the running session's Runtime or downgrade enrolled storage to
   manufacture this case. Interrupted writes use the existing read-only recovery
   control; never automatically replay them.
8. Inspect old Jobs/results without running or upgrading them. Their Runtime and
   release identity must remain frozen. If separately performing the earlier
   Dialect → Pack → Job journey, follow its existing explicit review and test
   steps in a new isolated fixture; this checklist grants no Job execution.

Record release/build identity, steps actually performed and any exact failure.
Keep CI, unit checks and native observations distinct. No live research pilot,
provider quality or source/admission authority is established by this checklist.
