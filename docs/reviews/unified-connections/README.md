# Shared connection pane browser check

The production candidate bundle was tested with isolated XDG directories and a
synthetic Obsidian vault. No live accounts, model API calls, credentials or user
chats were used. `results.json` records the browser assertions.

- Connect a vault, search, select and attach its note through the real gateway.
- Read the verified snapshot; exclude a hidden configuration note.
- Keep the unsent composer draft, return focus to it after attachment, and create
  no saved empty chat.
- Retain the original snapshot after editing the source and disconnecting.
- Open the shared narrow drawer at 390×720 in all 12 locales; no horizontal
  document overflow or browser errors.

The screenshots show setup, selected sources and Japanese narrow layout. The
resizable desktop pane has a shared header, one body scroller and a pinned action
footer. Google registration uses its existing inline setup guide. Live Notion
OAuth still needs user acceptance after gateway review; the fixture smoke does
not claim that acceptance.

Unit checks additionally hold Assistant/main DOM retention and focus restoration,
source selection across dock/drawer portal moves, account/configuration changes,
late cancellation, attachment capacity and Gmail pagination context binding.

The original session-local smoke driver is now preserved as
[`scripts/check-connections.mjs`](../../../scripts/check-connections.mjs), with
configurable paths. See [the recovery review evidence](../notion-review-recovery/README.md)
for invocation and a fresh run, including route-change dismissal.
