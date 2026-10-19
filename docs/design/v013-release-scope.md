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
| Reusable templates | Generic authoring and exact pinned Exchange-template import with reviewed local connection binding are covered by pinned two-installation fixtures; live source quality is not established. #398 itself explicitly excludes Exchange publishing. |
| Main signing and witness work | Preserve independent Runner key-list checks, per-run export verification, inherited signing-variable isolation and optional witness checkpoint statements/coverage from main. Operator-held metadata does not establish independent trust. |
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

Status recorded 2026-10-19. The earlier cumulative interaction review and
source CI [39275718610](https://github.com/Judgment-Pack/judgment-pack-desk/actions/runs/39275718610)
remain evidence for their inspected source. Later published-companion integration
is recorded in [#527](https://github.com/Judgment-Pack/judgment-pack-desk/pull/527)
and [#528](https://github.com/Judgment-Pack/judgment-pack-desk/pull/528), with all
15 checks green in CI
[39294713397](https://github.com/Judgment-Pack/judgment-pack-desk/actions/runs/39294713397).
The subsequent Policy handoff fix in
[#530](https://github.com/Judgment-Pack/judgment-pack-desk/pull/530) has bounded
source-review acceptance and 28 focused tests plus type checking passed. It merged
as `0c7d1364eb1982e4520cdf8143f398139188db3b`, tree
`5ded52ca8f5af7da45cad22fc94b90c16ddd8bad`, identical to reviewed source
`093e8d8a76c3f29e56939af8824a745bb0f4eac0`.
[CI 39301122129](https://github.com/Judgment-Pack/judgment-pack-desk/actions/runs/39301122129)
completed all 15 checks successfully or with the intentional development-only
skip. Its seven manual-admission checks and enrolled executor passed against
published companions. Rebuilt native acceptance is recorded below.

- [x] Integrate the combined Dialect, mapping/Jobs, shared AI/UI and Policy source
  scope, with recorded material reviews and bounded cumulative interaction review.
- [x] Verify integrated Dialect output with pinned fixtures, canonical/typed
  preparation and negative/refusal checks in source CI. This is not a released
  conformance claim; exact mapping bytes and local credential custody remain distinct.
- [x] Validate generic authoring and signed Exchange-template import/local binding
  through source fixtures, including two installations and unsupported/unbound
  source refusals. These fixtures do not establish live research quality.
- [x] Verify and pin official immutable Runtime `v0.36.0` at
  `20d6bf86b23f1794340fdc648201dda2e80a4095` and Runner `v0.24.0` at
  `393b5df0f60204fe76b9dcb4bf1ba85d0e5865ff` archives/checksums/attestations.
  Gateway `v0.21.0` and harness `v0.1.0` are unchanged; Runtime `v0.35.0` stays held.
- [x] Pass all seven actual manual-admission checks and enrolled executor acceptance
  against those exact adopted archives in CI 39294713397. This does not enroll
  the user's workspace or establish every native UI/installation flow.
- [x] Complete isolated fresh owner login, one new manual Job, `v0.12.0` upgrade
  preserving original records/Job/release/run without replay, offline byte/mode
  backup restore, and default-off profile refusal checks on the earlier frozen
  candidate identified in [local validation](v013-local-validation.md#release-acceptance-checkpoint--2026-10-19).
- [x] Complete installed company research → selected-company person draft/research
  → mapping/testing → new Job → verified export with a configured synthetic
  external Gateway adapter. This uses four local model calls and two loopback
  GETs; it is not managed `adapter-web` public-HTTPS or live-quality acceptance.
- [x] Verify rebuilt native Policy create → saved page → exactly one request-changes,
  correct review state without stale warning, and read-only reload after #530.
  Earlier lost-reply/unsaved-exit evidence retains its bounded scope; delayed and
  independently cancelled handoffs use the production-shaped source regressions.
- [x] Measure all 12 native Library locales at 1440/390px (24 states), visually
  inspect all 11 non-English narrow screenshots, and verify appearance persistence
  plus dirty-guard Keep editing/unload protection. Earlier 103-state source and
  17-state native evidence retain their historical totals.
- [x] Verify native Plan → explicit Start work → refresh, normalized rate-limit
  failure after bounded refusals → refresh → explicit retry success, and saved-chat
  scoped Auto grant → refresh → same-grant revoke → refresh to Ask me. Empty
  unsaved-chat grant is refused. This does not claim a positive Auto Pack mutation,
  expiry wait or changed-file invalidation; source coverage remains separate.
  Restart/cancellation/partial/stale/unavailable research subsets retain dated
  source evidence; the synthetic journey does not newly certify every subset.
- [ ] **Deferred by the user:** the 50-candidate live pilot, manual adjudication
  and usage/reliability assessment tracked by #369. Live research quality remains
  **UNVERIFIED**. Deferral is not a pass or a claim of discovery reliability.
- [x] Complete historical 5,696-message and #530-tree 6,611-message catalogues
  in all 11 translated languages with strict locale/source checks; see
  [localization evidence](../localization.md). These checks do not establish
  final translated layout acceptance or independent native-speaker validation.
- [x] Complete #530 candidate CI with all 15 checks successful or intentionally
  development-skipped, including packaging/companion checks; verify the exact
  Linux/amd64 native acceptance artifact. Platform packaging evidence remains
  in the linked CI rather than a claim of local macOS execution.

- [ ] Reconcile main's signer/witness source and dependency changes, verify their
  interactions with frozen companions and retained historical releases, and accept
  the bounded native signer/witness/read-only record delta on the final source.
  The #530 native run does not exercise these subsequently merged changes.
- [x] Complete the reconciled 6,792-key catalogues in all 11 translated languages:
  182 new signer/witness messages replace one retired message. All 2,002 new values
  are integrated, existing values preserved, and canonical strict source/catalogue
  and token-preservation checks pass with zero placeholder errors. The author
  self-reviewed the translations; this does not claim independent native-speaker
  validation or final native signer/witness layout acceptance.

Publication remains gated by complete-release-diff review, the final integration
PR's required CI and release acceptance. Their disposition is recorded on that
PR/release record; this prepublication checkpoint does not claim a published tag.

The independent Spec [Policy draft prerelease](https://github.com/Judgment-Pack/judgment-pack-spec/releases/tag/policy-v1.0.0-draft.1)
is now accepted at draft maturity, exact format `1.0.0`, Core unchanged. Desk
`v0.13.0` / Runtime `v0.36.0` deliberately retain earlier experimental prototype
`0.0.2` / candidate `1.0.0` bytes; publication does not adopt official schema
identities or result maturity in these products. Official software adoption is
[Runtime #293](https://github.com/Judgment-Pack/judgment-pack-runtime/issues/293),
a separate follow-up rather than a requirement to relabel the existing experiment.

Dated checks and exact candidate identities remain in
[v0.13 local validation](v013-local-validation.md). Current catalogue status lives
in [Desk localization](../localization.md). Historical test totals describe their
specific snapshots and must not be presented as final integrated release results.
