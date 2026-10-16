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

## Remaining delivery

#400 still needs fresh provider acquisition into the portable research response contract, published Exchange use-case entries, migration of legacy authoring content and generic downstream execution. Arbitrary page crawling needs an accepted profile/endpoint solution; cross-stage selection and fan-out depend on the still-draft workflow contract in Spec #160. This increment does not replace existing recipes, records or reviewed evidence.

#404 still needs to extend the persisted compatibility controller across pack manual/chat authoring and publication, and expose a separate upgrade review for incremental subscriptions. Existing jobs and subscriptions keep their retained releases. Automatic named Dialect publication requires a resource-aware adapter in the existing approval registry; a research or pack-update allowance must never be reused for it.

#402's shared correction checkpoint is implemented here. Named publication chat actions and broader manual/chat compatibility-controller coverage remain part of the coordinated #404 follow-up. No new provider loop, approval bypass or alternative catalog was introduced.
