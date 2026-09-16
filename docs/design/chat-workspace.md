# Chat workspace

Status: implemented on `feat/chat-workspace`; independent review required before merge.
Storage and engine decisions are recorded in [ADR 0002](../adr/0002-project-conversations-and-chat-workspace.md).

The current Create page asks for metadata before the conversation, loses the run
on navigation, and makes the reader choose between details and the assistant.
The replacement starts with a single composer. Opening a draft docks the same
chat beside it; creating a pack links that chat to the saved pack.

## Regions and transitions

- Left: Create pack, Packs, then recent chats. View all opens searchable history.
- Main: conversation until the person chooses Open draft; then the draft, its
  sources, tests, and final review. Do not automatically move focus on generation.
- Right: Assistant on authoring and pack document routes. Conversation selection and New
  chat live in its header. One pack may have multiple conversations.
- Bottom: selected details and Activity, underneath main only. The Assistant
  stays full height. Both dividers use the same accessible separator component.
- Narrow screens: explicit Chat/Draft navigation; no forced three-column layout.
- Missing configuration: retain the composer and open a Configure AI modal using
  Admin's existing form. Include a link to the full Admin page.

Create pack starts an unbound chat. New chat from a pack retains the explicitly
shown pack context. Switching conversations restores the draft, text, model and
view. It never submits, cancels, or changes a running task. One active operation
per project in each window is allowed initially, with an explicit Stop action. The first send
names a chat; rename, pin, archive and delete affect history, never pack files.

## Persistence and trust

Conversation checkpoints live outside the project under the chassis' protected
local configuration directory. The API is authenticated and origin checked,
project scoped, size bounded, versioned and optimistic-concurrency checked.
Files are owner-only, opened without following links, and replaced atomically.
Browser storage holds layout preferences only, never transcripts or API keys.

Persisted readiness, receipt verdicts, checks and approval tokens are not
authority. Restoring a checkpoint must make creation unavailable until sources
and expectations have been verified and the exact candidate rechecked. Reload
never starts paid work. Unsaved persistence failures remain visible and prevent
a false "saved" indication. Deleting a chat does not delete a pack or its
research record. Existing explicit expectation-approval and Create gates remain.

## Visual rules

Use existing tokens, Radix primitives and typography. Selection uses neutral
surfaces and unchanged text colors; green and gold are restrained semantic
accents. Keep headers and composers outside their scrolling content. Details
are supplemental, not a second copy of the main document. Controls describe
actions (Open draft, New chat, Create pack), and activity reports actual work.

## References

- Linear, [Behind the latest design refresh](https://linear.app/now/behind-the-latest-design-refresh): quieter navigation and compact consistent controls.
- Linear, [Linear Agent](https://linear.app/docs/linear-agent): persistent conversations, recent history and visible running state.
- WAI-ARIA, [Window Splitter](https://www.w3.org/WAI/ARIA/apg/patterns/windowsplitter/): focusable separators, range values and keyboard resizing.

These inform the design; they are not a claim to reproduce Linear's private
design system. Validation covers restore and session isolation, long text,
small viewports, keyboard resizing, configuration recovery and creation gates.

## Verification

- Web production build and component/engine regression suite.
- Native runtime replay of expectation admission and explicit correction.
- Go custody, authentication, origin, size, unsafe-file and stale-write tests.
- `scripts/chat-workspace-check.mjs`: isolated browser flow through setup,
  session switching/reload, fresh validation, draft logic, keyboard/pointer
  resizing, short/narrow windows, creation and retained conversation.
- `scripts/containment-check.sh`: the existing route/width/pane matrix, extended
  to include a real saved conversation. Main-chat routes have no second
  Assistant pane, so their valid configurations exercise the bottom panel only.

Browser checks use a copied runtime project, temporary private configuration,
and an owned server process. They do not call a model or change the running Desk.

Validation recorded for this branch: all 11 isolated browser scenarios passed.
The full layout sweep exercised 508 configurations; 485 passed and 23 hit an
obsolete assertion that the right divider must end above the bottom panel.
The assertion now checks the full-height right pane, its matching divider,
and the bottom panel staying beneath main only. All 124 desktop configurations
were rerun and passed, including all 23 affected cases. The default gate still
runs all breakpoint widths; an optional scoped rerun explicitly labels its
limited coverage. The copied fixture remained unchanged.

## Screenshots

Real browser renders using an isolated fixture, without a model call:

![Chat landing, dark](chat-landing-dark.png)
![Chat landing, light](chat-landing-light.png)
![Draft beside the same conversation](chat-workspace-dark.png)
![Pack details below main, Assistant stays visible](chat-pack-details-dark.png)
![Searchable conversation history](chat-history-dark.png)
