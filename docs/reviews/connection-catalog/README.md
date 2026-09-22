# Gateway capability catalog acceptance

The companion advertises implemented connection protocols. Desk intersects these
with supported UI handlers, then requests account status only for compatible
providers. This follows the shared-pane candidate in Desk #122; its reviewed
commit is unchanged. The gateway parent is #143 and the catalog commit is
7732913e65366a8e7def1cb09535cc8f92ffbc5b.

The composer, connection catalog and provider detail pane use the same capability
result. Losing a capability cancels pending selection and discards late results.
A failed catalog refresh clears usable shortcuts rather than trusting stale
entries. The open composer menu keeps its row positions; a removed shortcut is
disabled, and newly connected providers appear on its next opening.

Registration and account consent remain separate. Catalog discovery performs no
OAuth or source acquisition. Existing Google registration handlers remain local
UI code, as do provider-specific source ingestion and fixed consent URLs. Unknown
provider IDs and incompatible protocol combinations cannot select new handlers.
This is not an arbitrary extension loader or account-wide assistant search grant.

## Validation

- Web: 3,773 passed, one existing skipped test, 171 files.
- Typecheck and production build passed.
- Twelve locale catalogs: 2,187 keys each, zero placeholder errors.
- Desk Go suite and vet passed. The catalog subprocess tests cover cancellation,
  excessive output, failure with diagnostics, malformed JSON, unsupported version,
  duplicate operations and unexpected fields; HTTP checks cover session guards,
  parameter refusal and no implicit local fallback for an external gateway.
- Gateway adapter tests/vet and core regression tests passed. Its new tests prove
  discovery neither loads publisher registration nor needs/creates account state;
  unsupported control methods retain operator-disable enforcement.
- Isolated Chromium run used a temporary project, private state directory and
  synthetic Obsidian vault. No model requests, personal sources or live OAuth.
  Browser-discovered catalog included exactly the four implemented providers.
  Selection, receipt verification, composer focus and unsent text survived source
  attachment; retained text survived source edits and disconnect. No empty chat
  was persisted. Catalog failure showed one retry surface; retry recovered.
- All 12 locales at 390px passed overflow and page-error checks. Screenshots and
  machine-readable findings are beside this document.

No live Notion or Google sign-in acceptance is claimed. This isolated candidate
was not installed over the user's running Desk. New provider families remain in
[the complete rollout plan](../../design/connections.md).
