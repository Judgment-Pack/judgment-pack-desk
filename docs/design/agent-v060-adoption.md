# Agent v0.6.0 component adoption

Desk pins the released Go module and browser harness from
[Agent v0.6.0](https://github.com/Judgment-Pack/judgment-pack-agent/releases/tag/v0.6.0).
Both release tags resolve to `3f308215afadd9d72ae963ebea1062247d1963ba`.
The harness archive SHA-256 is
`5bc4e0314d79b2f5c68296b0ebd0e0f8044444d4a9f50d68b6c0712c40b5c2b0`;
the component lock owns its version, commit, release URL and digest. The npm
lockfile binds the same bytes by SHA-512, and go.sum retains the resolved module
checksums. Runtime, Runner, Gateway and unrelated package pins stay unchanged.

The release archive and checksums file have verified asset attestations constrained
to the Agent repository, release workflow, v0.6.0 tag and exact source commit.
That proves asset provenance, not a reproducible rebuild or Git tag-signature
verification. The release tag's signature is reported as unknown_key and the
workflow-created Go tag is unsigned.

## Compatibility boundary

The released approval-pause fix persists the tool-description framing marker and
reuses it on continuation. Upstream release certification compares the actual
adapter prefix for both approved and refused continuations. Desk's host regression
retains the marker through the actual durable pause, executor lease reacquisition,
canonical ReadExecution and authenticated approval delivery; the same marker must
produce the same framed description bytes. This test dispatches no capability.

Published checkpoint readers also retain approval.marker, checkpoint.skills and
sourceFigures.skills. The saved-chat regression passes the new metadata through
readExecution and the actual checkpoint encode/decode readers, retaining exact
case-sensitive arguments and existing usage/grants. Historical records are not
skill declarations, approvals or new tool grants.

The event list explicitly handles the released approval-waiting, skill-loaded and
skill-unloaded events. Compact lists retain approval waiting and filter the
observational skill lines. These statuses introduce no approval control or skill
workflow. The exact AssistantSession member guard gains only the published
optional skills field and remains closed to other additions.
An injected engine in the actual AssistantPane path also observes that the Desk
constructs no skills declaration while retaining its existing proposal tool.

The existing actual API/Eino, native Codex parent-child-parent, critic, current
custody and projection fixtures remain the compatibility baseline. The standard
projection runner still requires six ordinary, six Unicode and four case-sensitive
history observations through the real browser readers. Focused adoption checks
exercise these bindings and the browser host/context contracts; CI retains full
Go/Web/platform and mutation validation.

v0.6.0 has the same browser dependency map as the previously pinned harness. Only
the harness archive and Go module change; no unrelated dependency upgrade is
needed. The browser's new optional brief, memoryFor and projectInstructions hooks
keep their documented empty defaults. Required host hooks remain supplied and
checked; wiring browser prompt parts remains Desk #546. Existing server-owned Host
parts are unchanged.

## Limits

This update declares no skill, adds no load/unload UI or approval workflow, and
expands no tool ceiling. Host-declared skills and their display/re-approval remain
Desk #565; tool-source and prompt-part workflows remain #548 and #546. Existing
private factories stay unregistered. Graph integration, domain actions, final
workflow/CAS migration, browser removal and complete production cutover remain
separate work under #537. Component adoption does not certify those domains.
