# Unified connections: UX review and complete provider scope

2026-09-21. Recommendation and implementation plan, not a claim that all listed connectors are shipped. Reviewed Desk candidate 86ad1efc2886ec702d17d3e472e07ce7cdd64be1 and the existing Google setup design. The user prefers the Google Drive/Gmail right-pane pattern and requests the full previously recommended integration scope, including Obsidian.

## Recommendation

Use one contextual, resizable Connections pane across discovery, setup, account connection, source selection and management. Keep its frame, vocabulary, placement, navigation and feedback consistent. Provider-specific authentication and source metadata remain different where necessary. Reuse the existing Google registration pane infrastructure; do not grow a separate modal family for each provider.

On home chat, the conversation remains in the main area while the connection pane opens on the right. In the pack workspace, where the Assistant already occupies the right pane, temporarily navigate within that pane and provide Back to assistant. Preserve its conversation, draft, scroll, running operation and previous width/open state. Do not create a permanent third column or a second Assistant tab. On narrow screens use the existing accessible drawer/full-width sheet.

## Findings before the refactor (86ad1ef)

1. Admin → Connections has a sound shared setup pane: resizable, sticky header/footer, inline expandable instructions and focus restoration. GoogleRegistrationSetup already separates registration saved from account connected.
2. Personal sign-in uses GoogleConnectionDialog, Gmail selection uses GmailPicker, My connections opens another Dialog, and the Notion/Obsidian candidate adds SourceConnection as a Dialog. The same task therefore has different surfaces depending on the entry point and provider.
3. My connections can stack provider dialogs on its own dialog. Setup-required flows leave chat for Admin; after registration, users must return and find the provider again. Preserve the app-registration/account-consent distinction while removing that navigation detour.
4. Copy alternates among Set up, Configure, Connect, Continue and Manage. Some represent different real steps, but the UI does not present a common lifecycle. Unavailable frequently provides no action or cause.
5. Gmail loads an initial result list, whereas the new note picker initially shows a search instruction. Selection is bounded, but selected-count feedback and failure/retry treatment are not uniform.
6. The composer menu currently lists every implemented service. Adding the full roadmap would make it too long. No provider catalog or capability-based registry currently owns all these views.
7. Application registration and personal account connection are both presented under Connections in different places. They need clear labels and scopes, particularly before future organization features arrive.

Baseline evidence: web/src/admin/ConnectionSettings.tsx; connections/GoogleRegistrationSetup.tsx, GoogleConnectionDialog.tsx, GmailPicker.tsx, PersonalConnections.tsx, SourceConnection.tsx; chat/AttachmentMenu.tsx and ChatPanel.tsx; shell/InspectorPresentation.tsx.

## One entry and one working surface

- Composer +: Upload files, Add link when supported, a short user-pinned/connected provider list, then More connections. Reuse official provider marks; provider name on the left, one short contextual hint on the right. Keep row order stable while the menu is open. Do not list unfinished providers as usable integrations.
- More connections opens the same right pane with a searchable provider list, grouped Connected and Available. Use compact rows rather than large marketing cards. A provider row leads to its detail view inside that pane.
- Account menu → My connections opens the same catalog in management context. It should not start a separate modal hierarchy.
- Admin retains advanced app registration and runtime configuration. Label the application-registration area explicitly (for example App registrations) so it is distinct from My connections. Reuse its setup body when registration is needed from chat; after saving, return to Connect account in the same pane. Saving app configuration must never silently authorize an account.
- The future enterprise edition may add organization-managed connections and approval policy in Admin. Do not present organization controls as functional in today's local product.

## Shared pane anatomy

Start with the existing 480px setup width and maximum 560px, clamped to preserve usable main content. Provide a keyboard-operable divider and reset; remember the pane preference without overwriting the Assistant's width. Use the existing drawer when the two panes cannot fit.

Sticky header: back control when nested, provider mark and name, close icon with tooltip. The account/workspace/vault label sits below in secondary text. Show scope once: Personal · This computer where relevant.

Body: only the fields and information needed for the current state. Use a single scroll region. Put Setup instructions directly below its disclosure toggle; expand initially only when setup is required. Keep official documentation links at the end of those instructions. Keep required fields and the primary action outside the disclosure. Permissions summaries stay readable; exact scopes and implementation details go under Access details.

Sticky footer: one primary action for the current task. Use Continue with Google / Continue with Notion for browser consent, Connect vault for a local folder, Search where the provider requires a query, and Attach 2 items for a selection. The header already provides Close; avoid duplicating Close and Done in every footer. Display operation feedback and errors once, beside the relevant action.

After Attach: close the temporary pane or return to Assistant, focus the composer, and show the selected attachment chips. Do not send the message automatically. After Connect from management: stay in the same pane with the real account state.

## Common lifecycle and precise copy

| State | Message / action |
| --- | --- |
| App registration missing | Setup required / Set up app |
| App ready, no account | Not connected / Connect account |
| Browser consent pending | Waiting for sign-in / Cancel |
| Account connected | Account identity / Browse or Search |
| Expired or revoked authorization | Reconnect required / Reconnect |
| Temporary service failure | Plain-language failure / Retry |
| Local processing unavailable | Specific cause and route to restore it |
| Future managed policy refusal | Managed by your organization; only expose after the policy exists |

Use Connections for linked services and Sources for material attached to a message. Reserve Integration and Adapter for technical documentation. Registration configured, account connected, source selected and assistant allowed to search are separate facts.

## Source selection and assistant use

One selection component handles files, pages, mail, messages, issues, events and records. Keep a shared structure (title, useful secondary metadata, checkbox, preview) with type-specific details: sender/date for mail, path for files, channel/time for messages, issue key/status for work trackers, date/time-zone for events. Do not turn every result into a verbose card. Keep selected-count feedback visible and preserve selection across pagination; account changes invalidate it.

Preview opens within the same pane with Back to results, preserving the query, scroll and selection. Avoid nested popovers for long documents. For Google Drive retain the official browser picker when required; its native UI is an intentional provider-owned step, while the surrounding Desk states remain consistent. OAuth stays in the provider's browser consent screen; do not embed login pages in Desk.

Attaching selected material is distinct from enabling service-wide agent search. A future Use in this chat control can explicitly enable bounded search/read tools, with account/resource scope enforced by the gateway. No silent account-wide permission follows from one attachment. Writes and maintenance watchers need separate capability decisions and interaction designs; the initial rollout is search/read.

## Full provider backlog

All concrete providers from the earlier recommendation are retained. The rollout order is sequencing, not removal from scope.

| Group | Providers / capabilities | Delivery sequence |
| --- | --- | --- |
| Existing Google | Drive (Docs, Sheets, Slides), Gmail; structured Sheets ranges, Gmail threads and selected mail attachments | Shared UX first, then deepen |
| Knowledge | Notion, Obsidian | Current gateway/Desk candidates; move UI to shared pane before rollout |
| Web | Add URL and existing gateway web search/read in ordinary chat | Early shared source capability |
| Microsoft | OneDrive, Outlook Mail, SharePoint, Teams | Account-family slice; respect personal/work capability differences |
| Communication | Slack | Reviewed app registration/distribution plus thread search/read |
| Product and engineering | GitHub, Linear, Jira, Confluence | Shared issue/page/repository selection and citations |
| File storage | Box, Dropbox | Reuse file selection and preview |
| Calendars | Google Calendar, Outlook Calendar | Read event context; separate from pack scheduling |
| CRM | HubSpot, Salesforce | Read selected records for defined pack inputs |
| Support | Zendesk, Intercom | Read selected tickets/conversations |
| Business databases | PostgreSQL first as the existing catalog starting point; additional engines remain unspecified | Operator-configured read-only datasets, bounded queries |

Figma/Canva were explicitly deferred in the earlier recommendation, rather than recommended for this source-reading rollout. Revisit them when design/media authoring becomes a supported Desk task. The phrase business databases did not specify every database engine; do not claim universal database support.

## Architecture and release plan

1. Refactor Desk to the shared pane and provider registry; migrate Drive, Gmail, Notion and Obsidian without changing consent semantics. Cover desktop/narrow layout, keyboard/focus, cancel/close, account switching, errors, themes and all 12 locales.
2. Add gateway capability metadata/lifecycle needed by the larger catalog. Gateway remains authoritative for provider availability, account state, supported operations, auth and retrieval. Desk localizes labels and presents only supported operations. Avoid duplicating hardcoded provider switch statements throughout the UI.
3. Ship provider families in independently reviewable gateway + Desk slices using the shared source contract, provenance, retained snapshots and citation handling. Each requires authentic admission/refresh/revoke/cancellation handling, bounded search/read and fixture tests; no placeholder-only connector counts as delivered.
4. Add explicit per-chat agent search grants after the gateway enforces them. Preserve selected-source attachment independently. Add maintenance watchers and write actions later under their own authorization/preview models.

A consistent pane cannot remove provider registration requirements. For example, Slack's official MCP currently requires a fixed app identity and an eligible internal or marketplace app. Self-hosted setup needs a real route for those requirements; a future hosted service uses organization-owned registrations. No personal publisher registration goes into GitHub releases.

## Implementation checkpoint

The shared pane is implemented for Drive, Gmail, Notion and Obsidian. Composer +
shows connected providers and More connections; the user menu's My connections
opens the same catalog. Google registration is reused inline. The shell keeps its
route-owned Assistant target mounted behind the utility and restores the prior
presentation when it closes. Picker controller state is outside the portal, so a
dock/drawer transition retains a query and source selections. Gmail selections
across pages retain each page's gateway selection context. A successful attachment
returns focus to the composer; failed or canceled attachment stays recoverable.
All new copy is translated in the 12 existing locales.

The table above is the approved backlog, not a list of working connections. The
remaining families still need gateway implementations and their corresponding
review and acceptance checks. The current registry only lists implemented
providers. Add URL and agent-wide search are not exposed as placeholders.

Existing candidates remain draft and subject to gateway material-decision review. This UX review does not authorize changing the gateway review policy or claim live Notion sign-in acceptance.

## Alternatives

| Approach | Assessment |
| --- | --- |
| Shared contextual pane | Recommended for personal Desk: preserves the task, accommodates guidance and source selection, matches the preferred Google setup flow. |
| Full Connections page with a detail pane | Useful later for many accounts, organization policy, audits and bulk administration. Keep as an optional management destination, not a mandatory chat detour. |
| Provider-specific modals | Fine for a short exceptional confirmation. Poor default for setup, long help and repeated source browsing; current duplication would continue. |

## Evidence and interpretation

Linear's [2026 design refresh](https://linear.app/now/behind-the-latest-design-refresh) emphasizes predictable action placement, quiet supporting controls and reduced visual competition. Its [changelog](https://linear.app/changelog/2026-03-12-ui-refresh) describes consistent headers/navigation/view controls. These support a common frame; Linear does not prescribe this exact connection-pane design.

Linear's [Peek documentation](https://linear.app/docs/peek) supports contextual preview with keyboard navigation and Escape. Adapt the context-preserving behavior, not its keyboard-only discovery requirement.

[NN/g's modal/nonmodal guidance](https://www.nngroup.com/articles/modal-nonmodal-dialog/) explains why reference-heavy tasks benefit from keeping the main task available. This supports a modeless desktop pane and task-appropriate narrow-screen fallback.

[WAI's window splitter pattern](https://www.w3.org/WAI/ARIA/apg/patterns/windowsplitter/) describes keyboard operation and naming of the divider. Its example work is noted as incomplete; treat it as interaction guidance and validate the actual component.

Linear's [integration directory](https://linear.app/docs/integration-directory) distinguishes discovery and workspace administration and asks users to consider integration owner/permissions. Its admin-first install policy belongs to its team product; today's personal Desk should not copy that restriction universally.

Provider constraints: [Slack official MCP](https://docs.slack.dev/ai/slack-mcp-server/), [Microsoft delegated/application permissions](https://learn.microsoft.com/en-us/graph/permissions-overview), and [GitHub host integration](https://github.com/github/github-mcp-server/blob/main/docs/host-integration.md). The provider families and scope exclusions are captured in the backlog above.
