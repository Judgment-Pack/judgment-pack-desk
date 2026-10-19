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
| Portable Dialects | Spec-backed document types, canonical form/digest, staged-vector verification, mapping production and research/import source authoring. The [#398](https://github.com/Judgment-Pack/judgment-pack-desk/issues/398) integration is part of this same release. |
| Reusable templates | Generic authoring and exact pinned Exchange-template import with reviewed local connection binding are implemented and covered by source fixtures; final installed acceptance remains open. #398 itself explicitly excludes Exchange publishing. |
| Existing v0.13 foundations | Reviewed Web access, private-data freshness, local install/restart behavior, Jobs activity, retained input/release references and saved-record verification. |
| Local Policy lifecycle | Unified Library, retained originals, local-administrator review/publication, connected-source observations and consent, comparison, coverage/adoption review and bounded recovery. Optional `local-admin-manual-pack/1` remains a private experimental whole-workspace profile requiring explicit enrollment and compatible companions; it is not normative Policy adoption or team RBAC. |
| Localization and release quality | Complete final catalogs in all 11 non-English languages after integration, then pass strict checks, representative translated layouts and release acceptance. Earlier complete catalog snapshots are historical evidence. |

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

Checked source items below refer to `c4e6ef5c6170e7d134c8c3960adc7a9c909bc2bb`
(tree `ad65c7b1921c9edad46c103d00b9e289d1bf9e61`) and recorded Desk CI
[39275718610](https://github.com/Judgment-Pack/judgment-pack-desk/actions/runs/39275718610).
The cumulative review found no open P1/P2 in the inspected interactions; it reused
earlier exact material reviews and does not certify final installed acceptance.

- [x] Integrate the combined Dialect, mapping/Jobs, shared AI/UI and Policy source
  scope, with recorded material reviews and bounded cumulative interaction review.
- [x] Verify integrated Dialect output with pinned fixtures, canonical/typed
  preparation and negative/refusal checks in source CI. This is not a released
  conformance claim; exact mapping bytes and local credential custody remain distinct.
- [x] Validate generic authoring and signed Exchange-template import/local binding
  through source fixtures, including two installations and unsupported/unbound
  source refusals. These fixtures do not establish live research quality.
- [x] Pass the actual combined manual source acceptance's seven required checks
  with Runner `d903728fb38368a5f0574990e01bc27acb2a2803` and Runtime
  `55a23b884f0ce67fe6a9141b04e081a518fd5d61`; this is not archive/installed acceptance.
- [x] Verify exact official immutable Runtime `v0.36.0` and Runner `v0.24.0`
  release archives, checksums and attestations, and adopt source commits
  `20d6bf86b23f1794340fdc648201dda2e80a4095` and
  `393b5df0f60204fe76b9dcb4bf1ba85d0e5865ff` in the component lock. Gateway
  `v0.21.0` and harness `v0.1.0` are unchanged; Runtime `v0.35.0` stays held.
  Artifact verification is separate from installed Desk acceptance and does not
  migrate or enroll a workspace.
- [ ] Repeat all seven actual manual checks and native bundle smoke against
  that exact adopted archive set. Pin adoption is not a passing result for these
  checks or for new installed/enrollment acceptance.
- [ ] Run the final isolated browser/end-to-end journey on the frozen candidate: design/import a
  Dialect, research and review records, map/test a pack, create and run a Job,
  inspect retained results, select companies, and research people. Cover restart,
  cancellation, partial results, stale revisions and unavailable connections;
  Policy lifecycle/recovery and shared Plan/Ask me/Auto/error/retry/refresh/dirty/theme flows.
- [ ] Complete isolated fresh-install and `v0.12.0` upgrade acceptance, retaining
  legacy records/Jobs and checking explicit enrollment, backup/restore and refusal
  paths without changing live user data.
- [ ] **Deferred by the user:** the 50-candidate live pilot, manual adjudication
  and usage/reliability assessment tracked by #369. Live research quality remains
  **UNVERIFIED**. Deferral is not a pass, and fixture success alone does not
  establish live discovery quality. This pilot is deferred rather than claimed
  complete; the other release gates remain pending as recorded here.
- [x] Complete the historical 5,696-message catalog snapshot in all 11 non-English
  languages. This does not establish completion of the later integrated catalogs.
- [x] Complete the integrated 6,611-message catalogues in all 11 translated
  languages and pass strict locale/source checks; see the dated
  [localization evidence](../localization.md). This does not establish layout acceptance.
- [ ] Complete representative translated critical layout checks on the final
  release candidate.
- [ ] Pass frontend/backend checks, packaging and companion verification on the
  final integrated revision, then obtain release acceptance before publishing.

Dated checks and local implementation evidence remain in
[v0.13 local validation](v013-local-validation.md). Current catalog status lives
in [Desk localization](../localization.md). Historical test totals describe their
specific snapshots and must not be presented as final integrated release results.
