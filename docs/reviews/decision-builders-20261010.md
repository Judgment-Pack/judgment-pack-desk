# Decision builder workspace

The graph builder uses the existing application Assistant pane and a bottom selection editor. Pack editing and the pack-creation form reuse the same bottom editor and structured fields. This follows the selected-task/configuration-below pattern in [Databricks task configuration](https://docs.databricks.com/aws/en/jobs/configure-task), adapted to Desk's [Linear-informed design system](../design-system.md).

## Ownership and placement

- `AppShell` still owns the full-height right pane, its tool rail, expansion and narrow-screen takeover. Builder routes publish the existing `ChatPanel`; there is no nested chat pane or second conversation store.
- `BuilderSplit` owns only the main work surface and bottom selection editor. Its shared `PaneDivider` supports pointer and keyboard resizing. Expansion/collapse retains the content and viewer height preference.
- Graph Build, Tests, Settings and Source remain in one mounted workspace. Selection opens the bottom editor without switching the shell to Details. Source has the full working area. Save review uses the existing guarded graph-write confirmation in a dialog.
- Pack Build, Tests, Settings and Source retain the existing editing session, span-preserving writer, incomplete operand storage, undo and save guard. Rules, exceptions, outcomes, evidence and sources use their existing field components. Wide rule editors put conditions alongside properties.
- Chat graph identity is separate from pack identity. Send-time graph bytes bind proposed edits; another graph/path or changed draft cannot silently accept a proposal. Applying is one undo step, and saving remains explicit.

## Semantics

Connections transfer outcome IDs and/or evidence availability. Every graph node runs. Diagram position is presentation, not priority or conditional execution. Graph Tests explicitly use saved graph/pack files; opening a view does not run tests. Pack draft rehearsals retain their existing runtime path.

Settings retains the full pack document editor as an escape hatch for less common members, alongside Source for exact JSON editing. The runtime remains the validator; a visually well-formed diagram is not proof of policy correctness.

## Prior implementation review

Compared the parent of PR #360 (`194aa26`) to its merge (`5c0a3ad`). `AppShell`, `RightPane`, `InspectorSlot`, `shell.css`, `PackView` and `PackAssistant` were unchanged. The composition route lacked an Assistant portal, while selection details were sent to the shell's Details tool. The fix integrates that route with the existing shell and moves editing into the bottom pane.

## Verification

Browser checks use an isolated fixture desk, without configured AI credentials. They cover graph viewport retention, keyboard resizing, expanded and narrow-screen panes, tool/view switches with an unsent message, pack draft editing, and keeping Assistant available in Tests. Keeping a graph draft is also exercised through the real private-history API: reload restores it, and New chat preserves its original owner and graph bytes.

Regression coverage includes stale graph proposals, guarded graph writes, unfinished pack operands across all editor views, malformed selected items, shared-buffer undo and opening Assistant without leaving pack editing. No paid model call is made during the browser check.

New interface messages are extracted into the English catalog. Other locales currently use the repository's English fallback for these additions.

## References

- [Databricks task configuration](https://docs.databricks.com/aws/en/jobs/configure-task)
- [Databricks monitoring](https://docs.databricks.com/aws/en/jobs/monitor)
- [Linear interface refresh](https://linear.app/now/behind-the-latest-design-refresh)
- [Linear display options](https://linear.app/docs/display-options)
