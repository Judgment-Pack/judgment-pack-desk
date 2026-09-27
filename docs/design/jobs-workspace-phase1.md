# Jobs workspace: first implementation

Local schedules and event delivery are now implemented in [the next phase](jobs-local-triggers.md). This document records the initial workspace scope.

This implements the first stage of the [Jobs design review](../../web/mockups/jobs-operations-review/DESIGN.md).

## Working flows

- Jobs and all Runs are separate destinations under Jobs. Job rows include the
  fixed pack release and five recent execution states. Run rows separate execution
  state from the policy decision. Attention filters include interrupted/failed
  execution, unresolved decisions and requested handoffs.
- Search and filters run in the Runner before cursor pagination. Desk forwards only
  the supported query parameters. List summaries exclude inputs and audit bodies.
- Creation proceeds through Job, Inputs, Trigger and Review. Saved tests run against
  the exact release snapshot. Changes to the pack, test suite, project or sample
  invalidate release review. Unrun tests remain explicitly unrun.
- Manual facts use inferred field types; JSON remains available for other shapes and
  exact numbers. False, zero, null and unknown are distinct. Evidence availability
  is a separate declaration; choosing a document does not mark it satisfied.
- Mapping v2 assigns each fact and evidence requirement to case inputs, a selected
  file or a trusted source operation. Adding a source does not move existing
  assignments. Deliberate omissions are recorded. Advanced derivation remains JSON.
- Named sources open the existing Details pane. Operation and request configuration
  must be applied before preview; partially edited configuration invalidates the
  preview. Generated values require explicit target admission.
- Every operational run starts with empty case inputs and fresh file selection.
  Release samples are rehearsals and are never silently reused. Mapped responses
  still go through Runner verification and retain lineage and verification exports.
- Job and run briefs continue to use the right rail. Runs and artifacts remain in
  the local Runner store, independently of chat storage.

## Deliberately later

Manual and authenticated API submissions are the supported triggers. Durable local
schedules, authenticated event ingress, overlap/missed-run policies, cloud scheduler
adapters, release promotion, notifications and retention/backup need their backend
contracts before UI enablement. Direct HTTP/model catalog adapters also remain a
Gateway prerequisite; configured supported MCP profiles may return generated values.

## Verification

- Focused Jobs, test-workspace field, palette and scroll-containing-block tests.
- Runner tests using the real Runtime binary, including cross-page search, safe list
  summaries, recent-run bounds, job scope, attention filtering and race detection.
- Real Desk/Runner proxy tests for session/origin guards and forwarded query validation.
- Production build and all twelve locale catalogs.
- Isolated browser creation and execution for manual and mapping-v2 file inputs,
  explicit release review, fresh run inputs, attention/search filters and verification
  export availability. Desktop and narrow light/dark layouts checked for page overflow.

No operational data migration is required. Runner's new list fields are derived
summaries; stored jobs, releases and runs retain their existing representation.
