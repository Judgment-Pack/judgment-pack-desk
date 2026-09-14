# Packs workspace review

The sidebar previously promoted a raw file editor, graph operations and matrix
reports alongside Packs. Graph and matrix pages also executed tests on entry,
and the old project home could run graph suites to discover inventory.

Packs now owns All packs, Tests and Pack flows. Project files moves to the project
menu, which retains the unsaved-change indicator. Existing URLs remain valid and
no new route segment reserves a pack ID. Flow diagrams read declarations before
any test exists; tests run only on command, with optional detailed traces and
progress in Console. Results remain scoped to their connection and revision.

The design follows Linear's public [sidebar guidance](https://linear.app/changelog/2024-12-18-personalized-sidebar)
and [design refresh](https://linear.app/now/behind-the-latest-design-refresh): quiet
global navigation, contextual tools, shared compact controls and restrained
selection. These are Desk design decisions using its existing tokens, rather than
claims about Linear's internal component implementation.

## Upstream compatibility

Verified upstream main branches on 2026-09-14 in isolated clones:

| Repository | Revision | Relevant contract |
| --- | --- | --- |
| Runtime | `0c8a4b6` — 0.21.0 | ADR-0029 read-only graph inventory/documents, ADR-0030 revision binding, ADR-0031 opt-in node traces, ADR-0032 handoff-target assertions |
| Gateway | `bd4f1fd` | Engine image pins runtime 0.21.0; source acquisition and receipt verification remain gateway responsibilities |
| Specification | `ea1648c` | Core 0.2.0-draft; graph RFC 0002 and lineage RFC 0014 remain Draft proposals, not new Core requirements |

The runtime used by the browser check is built from that verified main revision.
This change introduces no gateway calls or new specification semantics. It keeps
runtime graph composition visibly experimental and feature-detects MCP tools.
The shared runtime, gateway and spec main checkouts were fast-forwarded to these
revisions. The runtime task-file edit was preserved byte for byte, and the
`bin/jpack` used by Desk's development launch was rebuilt as 0.21.0.

## Validation

`scripts/packs-workspace-check.mjs` uses a disposable project copied from the
latest runtime's graph fixtures. It checks navigation, explicit test calls,
file-change isolation, node/edge keyboard inspection, cached results, source-file
access, existing pack IDs named tests/flows, narrow panes, long content, empty
inventory and malformed documents. Its screenshots and measured results live in
`packs-workspace/` beside this review.

The production frontend build/typecheck and all 3,118 component tests passed.
The browser run covered six viewport/theme/density combinations from 360 to 1700px,
with zero page errors and exactly one test-suite call, following the explicit
Run tests command. Browsing, inspection, file changes and reconnects do not run
suites. The full 435-row route/pane containment gate is recorded separately.
