# Notion and Obsidian connections

Notion and Obsidian join Google Drive and Gmail in the shared Connections pane.
The chat attachment menu offers connected providers and More connections; the user
menu opens the same catalog through My connections. Setup, sign-in and selection
stay in one contextual pane, preserving the current chat and unsent composer text. Search results are not attached until selected. Up to four selected
notes become independently verified, retained snapshots with citations and links
back to the source. Removing a connection never rewrites previous chat evidence.

Notion opens browser OAuth from a deliberate click, with automatic client
registration in the gateway. The user chooses a workspace in Notion. No credential
file is requested. The UI discloses that Notion may include connected workspace
sources in search, while Desk accepts only matching Notion pages for attachment.
The gateway exposes search/read tools only. No page changes are offered.

Obsidian connects an existing vault folder on the machine running Desk. A short
expandable guide explains where Obsidian displays that path. Browser uploads do
not disclose an absolute local folder path, so this first local connection accepts
that path explicitly. It requires no Obsidian plugin, remote REST service, or OAuth.
An Obsidian vault on another machine needs a local copy first. The picker reads
Markdown notes and excludes hidden configuration/trash and existing symlinks.

Provider lifecycle, custody and all upstream access remain in gateway companions.
Desk relays control requests, exposes actual connection state, binds selected IDs
to the signed acquisition arguments, checks the new source contract, and provides
source reading/citations. The runtime/evaluator does not need a new protocol.
Notion uses the gateway's MCP receipt shape; local Obsidian snapshots use command.
Public builds continue to contain no publisher Google registration.

The shared UI supports all 12 current locales. The new Notion and Obsidian marks
come from official assets, documented beside their local SVGs. Google Drive and
mail continue to use the existing Desk glyphs. Tests use synthetic accounts,
notes and signed records. At the originally reviewed SHA the connected-source
fixtures were unsigned records; the remediation adds signed Notion and Obsidian
v3 receipts, commitments and seals in `signedConnected.ts` and attack tests that
sign the deliberately inconsistent relationship before verifying it. A successful fixture test is not a live Notion OAuth
acceptance test. The user's working Google Drive connection is outside the test
fixtures and must be preserved during any eventual local update.

This slice authorizes user-selected snapshots. Whole-workspace agent search,
background indexing, source watchers and provider write actions require separate
capability grants and are not inferred from attaching a note.

## Original candidate validation (before review remediation)

- Final shared-pane web suite: 3,756 passed, one skipped across 169 files.
  Focused shell/pane/style checks passed 497 tests, including draft retention,
  cancellation, account changes, portal moves and paginated selection contexts.
- Desk backend suite and local companion/pin checks passed.
- All 926 existing mutation needles still match. Locale checks cover all 12 locales
  with no missing translations or placeholder errors; the UI copy audit passed.
- Built the complete candidate bundle, including `adapter-sources`, against gateway
  commit `5346eb6f158d035693c3205ad0f555d85d78d53f`. The public Google registration
  sentinel remains unconfigured.
- Browser smoke used a temporary vault and the actual bundled gateway: connect,
  search, select, retrieve and verify a note; exclude hidden configuration; preserve
  unsent text and create no empty chat; retain the snapshot after editing its source
  and disconnecting. All 12 locales fit a 390-pixel viewport without horizontal
  overflow, with no browser errors.
- Gateway required adapter/core tests, vet, attachment schema checks and the frozen
  corpus passed. The new connection, attachment and HTTP MCP packages also pass
  the race detector. The optional full adapter race run hit existing wall-clock
  assertions in the process/PDF test packages; the ordinary required suite passed.

See [the shared connection design and full backlog](../design/connections.md).

The maintainer has supplied an independent Anthropic review of the parent gateway
and Desk SHAs. Findings and implementing dispositions are recorded on gateway
#143. A live Notion OAuth acceptance test remains outstanding. The candidate has not replaced the user's installed Desk.
