# Pack and graph workspaces

Both use the same order: **Overview, Logic, Sources, Tests, Review, Brief,
History, Usage**. A shared More menu holds sections that do not fit. The active
section remains visible. Switching sections preserves the global chat pane;
source details, revision comparison and history details stay in the main pane.

| Section | Primary purpose |
| --- | --- |
| Overview | Purpose, possible outcomes or final graph result, essential context. |
| Logic | Pack rules or graph composition; retain diagram and list views. |
| Sources | Declared references and retained source material. Graphs group the references of their current saved packs and identify the steps using each pack. |
| Tests | Independent cases, rehearsals and findings. Opening the tab never runs tests. |
| Review | Current readiness, actionable findings and the existing finalization/write flow. |
| Brief | Retained brief revisions, with explicit generation and regeneration. Opening the tab does not call a model. |
| History | Read-only retained revisions, checks, briefs and matching Runtime decision records. |
| Usage | Referencing graphs, jobs and links to their runs. Unsupported graph jobs remain explicit. |

## History boundaries

History is not a complete authoring or action audit journal. It displays the
records that exist, without inventing authors, timestamps, verified receipts or
approval. Draft changes without timestamps say “Time not recorded.” Graph draft
snapshots and test observations held only in the browser session are labelled.
Retained checks do not become operational decisions. Revision comparison moves
from Review into History.

Operational records are read from the current project's configured Runtime trail
using the definition's document identity, not its local configuration alias.
`GET /api/definitions/history?kind=pack|graph&id=...&before=...` uses the existing
authentication and audit-file containment/locking rules. Each request reads at
most 4 MiB and returns at most 50 matching records. Earlier records are loaded
explicitly; filters and export apply to loaded events. Incomplete or unreadable
lines are reported, not presented as completed decisions.

The record text is retained and exported unchanged (excluding its JSONL newline),
with a SHA-256 digest of those bytes. Input details retain numeric lexemes instead
of passing them through JavaScript numeric conversion. Displaying a record does
not verify its signature, chain, checkpoints, timestamps or cited receipts; the
view links to Decision safeguards for the existing project verification tools.
Receipt references and selected evaluation notes do not imply that Desk retained
or verified a complete execution trace.

This UI change adds no Runtime, Gateway or Runner contract and does not provide
missing authoring events or operational support for graph jobs.

History event titles and their subtitles share one aligned button. Detail views
lead with a plain-language result, recording time and available business context.
Content fingerprints and saved JSON stay under a collapsed **Technical details**
section, with explanations of their purpose. A mismatch means the saved results
differed from expectations; missing or unrecognized results never imply a pass.
