# Combined v0.13 release scope

Decision: 2026-10-15. The previously separate v0.14 Dialect work is folded into
v0.13. Codex's current Desk work and Claude's ongoing implementation belong to
one release scope. This is a development plan, not a release announcement or a
claim that every item has passed acceptance.

## Included work

| Area | Scope and current boundary |
| --- | --- |
| Dialect authoring and research | Generic recipes, supplied records and imports, bounded company/person research, retained sources, selected-record reprocessing, review and export. Tracked by [#369](https://github.com/Judgment-Pack/judgment-pack-desk/issues/369). |
| Dialect-to-pack and Jobs | Reviewed mappings, typed facts, pack snapshots, preparation previews, tests made from reviewed records, durable evaluation and incremental Job connections. Tracked by [#371](https://github.com/Judgment-Pack/judgment-pack-desk/issues/371). |
| Shared AI interactions | Shared context and project memory, planning, Ask me/Auto permissions, request accounting, progress, recovery, bounded self-correction, checked proposals, saved sessions and provider-neutral behavior. |
| Shared workspace UX | Consistent loading, notices, chat actions, dirty state, toolbars, tables, navigation, test pages and record-review workspaces. |
| Portable Dialects | Spec-backed document types, canonical form/digest, staged-vector verification, mapping production and research/import source authoring. Claude's ongoing [#398](https://github.com/Judgment-Pack/judgment-pack-desk/issues/398) integration is part of this same release. |
| Reusable templates | Generic authoring and pinned Exchange-template consumption with local connection binding are the agreed portability direction. These remain implementation/acceptance work; #398 itself explicitly excludes Exchange publishing. |
| Existing v0.13 foundations | Reviewed Web access, private-data freshness, local install/restart behavior, Jobs activity, retained input/release references and saved-record verification. |
| Localization and release quality | Current catalogs translated in all 11 non-English languages; re-extract after integration, translate any new messages, then repeat strict checks and release acceptance. |

## Implementation lanes

Codex preserves and checkpoints the existing Desk research, chat, preparation,
review and shared UI changes. Claude's coordinated work adds the portable
`web/src/dialect/` contract module, its mapping producer integration, ADR and CI
verification. The plural `web/src/dialects/` directory remains the workspace UI.

The mapping and Jobs integration touches an existing boundary: reconcile exact
files/functions at integration rather than overwriting either contributor's
work. The research-source bridge is delivered as a pure function and then wired
into the existing research worker. Separate branches and reviews are delivery
mechanics for this one release, not separate product scopes.

## Ownership and execution limits

The spec defines portable meaning; Exchange distributes reusable content; Desk
provides authoring, review and local bindings; Runner owns preparation and durable
execution; Gateway owns acquisition; Runtime evaluates decisions. Preserve those
boundaries instead of creating a Desk-only preparation engine.

A typed version-3 document does not establish an executable source or a verified
preparation. Report unsupported capabilities explicitly. Version-2 interoperability
must retain the bytes and semantics currently supplied to Runner, and verification
must include negative vectors and expected refusals.

The current company-to-person path uses an explicit selected-company handoff.
Automatic company-to-person research orchestration and graph Jobs need their own
supported execution contracts and acceptance. They are not implied by a decision
graph, a portable Dialect, or the merging of release scopes. Automatic outreach
and external CRM writes remain outside this work.

## Shared release gates

- [ ] Integrate Codex's checkpoint and Claude's portable-Dialect work, reconcile
  overlapping mapping/Jobs changes, and complete the required reviews.
- [ ] Verify the integrated Dialect output against the pinned spec fixtures and
  verifier; keep draft/staged-vector status distinct from a released conformance
  claim. Preserve exact mapping bytes and local credential custody.
- [ ] Complete and validate the generic template import/authoring path; explicitly
  report unsupported or unbound sources instead of presenting them as executable.
- [ ] Run an isolated end-to-end journey on the integrated build: design/import a
  Dialect, research and review records, map/test a pack, create and run a Job,
  inspect retained results, select companies, and research people. Cover restart,
  cancellation, partial results, stale revisions and unavailable connections.
- [ ] Complete the reviewed live pilot and usage/reliability assessment required
  by #369. Fixture success alone does not establish live discovery quality.
- [x] Complete the current 5,696-message catalog in all 11 non-English languages.
- [ ] Re-extract catalogs after integration, translate any new messages in all
  11 languages, and pass strict locale/source and representative layout checks.
- [ ] Pass frontend/backend checks, packaging and companion verification on the
  final integrated revision, then obtain release acceptance before publishing.

Dated checks and local implementation evidence remain in
[v0.13 local validation](v013-local-validation.md). Current catalog status lives
in [Desk localization](../localization.md). Historical test totals describe their
specific snapshots and must not be presented as final integrated release results.
