# Gateway-driven connection presentation

Status: proposed, September 2026. Implemented foundation, subject to review and
acceptance; not a claim that Dropbox, S3, Spaces or Azure are already connected.

Desk now reads catalog v3 from its installed gateway. Compatible resource-v1
providers use one setup form, browser sign-in controller, search/selection pane,
attachment pipeline and verifier. Provider name is data, not a UI-handler lookup.
Setup labels and instructions come from bounded localized gateway metadata.
Provider icons can be bounded inline PNG; absent marks use a generic link glyph.
Existing Google/Notion/Obsidian marks remain bundled fallbacks.

Admin → Connections and the chat connection picker both read this catalog.
Admin lists every advertised provider with its current status; compatible new
providers open the shared connection pane without a Desk-specific settings row.
Google Desktop registration remains available in Admin. Opening Admin only reads
the catalog and status; connecting and changing credentials remain explicit actions.

Unknown interaction protocols are visible with Update Desk to use this connection,
and remain inactive. Discovery failure clears available actions. Descriptor or
account changes cancel pending work and discard stale selections. Existing Google
registration/native picker behavior and legacy source checks stay supported.

The common resource proof binds the signed provider/acquisition source, selected
id, grant commitment and retained bytes. A receipt and seal are verified against
the current personal pin whenever a retained document is used. The generic
contract has its own source kind; existing proofs are not loosened to accept
arbitrary provider names. The gateway owns authentication and retrieval.

Local startup reads a separate bounded gateway-owned source plan. Browser metadata
cannot name executable code. Adapters must exist in the verified installation
manifest, and arguments are passed through the existing fixed CLI without a shell.

## Independent local gateway replacement

The default bundle is still pinned by Desk's reviewed GatewayRevision. To build
only a different reviewed gateway package from a checkout:

```sh
python3 scripts/build-bundle.py --gateway-checkout /path/to/gateway \
  --gateway-revision FULL_REVIEWED_COMMIT_SHA --gateway-only \
  --output /path/to/staged-bundle
```

This does not rebuild the Desk executable or web assets. The script prints the
exact SHA-256 of gateway-bundle.json. After choosing and installing that trusted
bundle alongside the existing Desk executable, set
`JPACK_DESK_GATEWAY_MANIFEST_SHA256` to that digest for the Desk process and restart.
Approval comes from the local operator, never desk.json, a project, a catalog or
the browser. An empty/incorrect approval is refused. A modified executable fails
its manifest hash check. Unsupported protocol versions remain unavailable.

This is an explicit local installation mechanism, not an automatic download,
in-app updater or remote publisher signature. Packaging a provider with a new
interaction/result protocol still requires a compatible Desk release. Public
bundles continue to exclude personal Google registrations and credentials.

## Validation target

Test an unknown provider supplied entirely by a synthetic replacement companion:
catalog discovery, setup, selection, actual gateway-signed acquisition, retained
attachment verification, draft/focus preservation and no empty saved chat. Keep
the Desk executable byte-identical throughout replacement. Fixtures use no real
account or AI API. Repeat existing Obsidian smoke and twelve-locale narrow layout
checks. Provider-specific live OAuth and cloud permissions are later acceptance.


## Cloud roadmap review follow-up

The earlier cloud roadmap review predates this implementation. Its remaining
shared concerns are now explicit: resource identity in status; four-file/4-MiB
limits; text versus path-prefix queries; 50-item/48-KiB cursor pages; disabled
rows with reasons; credential replacement; and truthful local disconnect versus
remote revocation. The gateway response records the finding dispositions and
provider-specific decisions still required.

The pane preserves the submitted query across pages, keeps eligible selections
within the connection epoch, refuses repeated cursors, and clears selections
when the configured resource changes. Unknown unavailability reasons disable
selection. The gateway must still recheck eligibility and grants before reading;
UI metadata grants no access. Resource-v1 accepts PDF and supported text through
one source contract, with actual PDF producer and signed consumer fixtures.

This is a flat bounded source list; no folder tree, full-text object search,
pre-attachment content preview, automatic restore or infrastructure actions are
claimed. The original review does not approve these commits. Both exact heads
and the parent stack still require the repository's independent review.
