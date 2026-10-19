# Portable authoring and retained version review

This increment advances Desk #400, #402 and #404. It does not close their remaining delivery requirements.

## Authoring checks

`checkArtifact` adapts the shared correction controller to host tools. The host retains the exact candidate before checking and checkpoints Runtime findings before returning them to the existing managed chat turn. A refused proposal, transport interruption or cancellation does not erase the candidate. Further corrections use the current turn's request budget; this adapter makes no model call and grants no write permission.

`propose_dialect` and manual recipe saving still use the authoritative Runtime document check. Restored findings are display evidence. Saving rechecks the current candidate, recipe revision, pack and conversions. Old failed findings stop displaying after a fresh successful preview or an editor change.

## Publication review

The shared artifact review record separates storage revision, proposed artifact version, canonical candidate reference, prior publication identity, compatibility judgment and exact dependencies. Reviews and their bytes are immutable retained artifacts. A private, bounded pointer per target selects the latest review for recovery. Selecting a version or restoring a review does not grant write authority.

Named Dialect publication captures the previous publication, including explicit first-publication absence. It checks that base again under the publication ledger lock. Compatibility impact and explanation belong to the exact retained review; changing them invalidates that review. The committed publication records the accepted review reference, even if a newer unaccepted review exists.

An unchanged recipe/document after a record refresh produces a durable request receipt pointing to the existing publication. Retrying that exact request resolves the receipt before Runtime availability or the recipe's current revision is checked. It never creates another version. Historical ledger entries omit new optional members, preserving their existing identities.

The browser waits for a fresh recovery read, including reconnect after an offline cached mount, before enabling version edits. It preserves a review's comparison base rather than replacing it during history refresh.

## Shared publication admission follow-up

Pack and graph files now enter the same compatibility gate whether edited manually or saved by chat. Review and lock displays the prior artifact version and exact proposed version, records the compatibility classification and reason, then requires a separate lock confirmation. Runtime checks grammar and arbitrary-size version arithmetic; Desk does not infer compatibility from passing tests. A changed published artifact must match Runtime's next patch, minor or major choice. Ordinary draft saving still does not publish a release.

The accepted immutable review references are written into a bounded pending publication record before Runtime changes the public lock. Recovery adopts those references only when the committed lock names that exact configuration and document closure. A newer unaccepted review cannot replace the selected one. If the files already match but the history receipt is missing, Review and lock offers explicit history confirmation. It reconciles the committed lock without invoking Runtime or publishing again, including repeat confirmations. Multiple aliases compare genuinely new releases against the highest published version of the identity.

Configuration upgrades use read-only publication admission before their existing rollback transaction. They cannot replace existing artifacts, retire a legacy identity whose only index is the current lock, or strand an accepted pending review. Those cases require ordinary Review and lock first. Truly new artifacts remain covered by the configuration upgrade's existing exact-file confirmation; no synthetic compatibility judgment is written. Failed upgrades preserve the entire original tree, signing state and audit folders.

Manual compatibility edits use the shared unsaved-work guard and retain their frozen review when the underlying files change. A changed snapshot cannot check or publish those stale edits. Dialect checks refresh the retained-review cache, so discarding a later manual edit restores the checked candidate rather than an earlier empty cache.

## Exchange configuration

Imported definitions now lead to a generic configuration form using declared source requirements, research fields and case parameters. Users select a pack and installation-owned profiles; model names and recipe instructions remain part of the immutable definition. Bindings contain only local profile assignments. The saved job draft retains the Exchange import identity.

Runner receives an import-only request and computes the bound mapping itself. Presets with no bindable sources use their ordinary mapping. The current UI refuses unsupported source kinds. Imported definitions remain frozen in the Job editor; changing a definition requires a separately reviewed local Dialect.

For this increment, research sources consume explicitly supplied retained response envelopes. Runner validates their receipts and derives facts. Uploads reject invalid UTF-8, preserve numeric spellings, coordinate cancellation, invalidate old previews and participate in unsaved-work protection. No legacy Desk entity values are relabelled as authentic research acquisitions.

## Validation

- Full frontend suite: 6,737 passed, 6 skipped; two bundle checks initially failed because the clean checkout had no build. Production build and the bundle tests subsequently passed.
- Focused configuration tests: 45 passed, including non-GTM configuration, exact imported definitions, wrong-model refusal and local-file-only presets.
- Final targeted bundle/publication/configuration tests: 23 passed, including offline cached-null recovery.
- `go test ./...` passed against Runtime `0.33.0-dev.8335027`.
- Real Runner HTTP round trips used newly signed synthetic company/person and work-item fixtures. Both prepared through `/v1/inputs/next`; after restart, retained responses prepared again without acquisition and with identical mapping digests. A correctly signed mixed-session fixture was refused with `acquisition-sessions`. This was preparation/replay testing, not a live provider search or operational job run.
- All 11 locales: 5,807 messages translated, no placeholder errors.
- Two independent clean-room Codex reviews found recovery, binding and file-import issues; these were corrected. These are same-vendor reviews.

Follow-up validation: the full Go suite passed (239.8 seconds) with both Runtime and Runner integration binaries. The full frontend suite passed 6,746 tests with six skips and one timeout in an unchanged research-status test; that test and the affected review surfaces passed in isolation (31 tests). The final receipt-recovery delta passed its focused Go tests and 20 UI tests. Production build and locale checks passed. A separate clean-room Codex review found no remaining P1/P2 issues after the recovery fixes; this is a same-vendor review.

## Remaining delivery

#400 still needs fresh provider acquisition into the portable research response contract, published Exchange use-case entries, migration of legacy authoring content and generic downstream execution. Arbitrary page crawling needs an accepted profile/endpoint solution; cross-stage selection and fan-out depend on the still-draft workflow contract in Spec #160. This increment does not replace existing recipes, records or reviewed evidence.

#404's persisted compatibility controller now gates pack and graph publication for both manual and chat-saved files. It still needs a separate upgrade review for incremental subscriptions. Existing jobs and subscriptions keep their retained releases. Automatic named Dialect publication requires a resource-aware adapter in the existing approval registry; a research or pack-update allowance must never be reused for it.

#402's shared correction checkpoint is implemented here. Named publication chat actions remain part of the coordinated #404 follow-up. No new provider loop, approval bypass or alternative catalog was introduced.
