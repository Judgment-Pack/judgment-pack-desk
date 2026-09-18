# Composer attachment menu

The reference is the user's ChatGPT screenshot: a plus menu contains named
sources, icons, and supporting descriptions. The official [ChatGPT plugin
guide](https://learn.chatgpt.com/docs/plugins) distinguishes installing a service
from connecting its tools; authentication may be required before use.

Desk uses one AttachmentMenu in both home and pack chat. Upload files opens
the existing multi-file chooser. Each row puts its short description beside its
label and wraps naturally when a translation or narrow viewport needs more room.
Google Drive remains disabled with the visible description "Not available yet".
Document configuration stays in Admin, outside the attachment menu. Configuring
the document extractor does not enable Drive.

The current Drive glyph is custom, not an official Google asset or a third-party
icon-library export. It preserves the user's earlier neutral/green/gold palette
restriction. The [official Google brand guide](https://developers.google.com/workspace/drive/api/guides/branding)
provides a multicolor product logo and permits resizing without changing its
appearance. Switching to that asset needs a provider-logo exception to the
palette rule; it must not be presented as an official monochrome mark.

No fake connection screen, new OAuth flow or provider capability is introduced.
A usable Drive action will require gateway retrieval and authenticated connection
state. This patch only adds the requested provider entry and improves the menu.

The menu reuses Radix and shared surface, hover, typography, spacing and border
tokens. It is anchored above the composer, bounded to the viewport, and flips
when necessary. The trigger tooltip is suppressed while open; Escape restores
focus. Disabled Drive is skipped by keyboard navigation. Item names and hints
have separate accessible labels/descriptions. All new copy is translated into
the existing 11 non-English locales.

## Recommended connection flow (not implemented)

The [ChatGPT connection guide](https://learn.chatgpt.com/docs/plugins) describes
authentication at installation or first use. It does not establish that every
provider and client uses a particular modal. [Workspace controls](https://learn.chatgpt.com/docs/enterprise/apps-and-connectors)
separate integration availability, role access, allowed actions and service
authorization. Desk should adopt that separation:

- Connected and allowed: select Google Drive to open the file picker.
- Allowed, not connected: show a compact account/permissions dialog, then start
  provider sign-in. Preserve the user's composer and return to file selection.
- Disabled by workspace policy: explain that an administrator manages access;
  do not offer self-service enablement or an OAuth action.
- Not implemented: keep the current explicit unavailable state.

Admins configure workspace availability, scopes and allowed accounts in Admin.
Users connect their own identity only where policy permits. A modal is suitable
for account connection, not for granting workspace-wide integration permission.
Authorization must be enforced on the server; hiding configuration in this menu
does not establish enterprise policy enforcement. No OAuth, permission model or
new gateway capability is added by this styling patch.

Validation: production build/typecheck, localization coverage, 34 existing chat
tests; real browser multi-file chooser, keyboard navigation and Escape checks.
Screens at 1440, 390 and 320px fit with no horizontal overflow. German, Japanese
and Cantonese descriptions fit the narrow menu. No browser errors.

[Desktop](composer-source-menu/desktop.png) ·
[Narrow light theme](composer-source-menu/narrow-light.png) ·
[Browser results](composer-source-menu/results.json)

Compact follow-up: production build/typecheck, all 2,043 localized messages,
14 existing chat/upload tests, and real-browser keyboard, Escape, native upload
chooser, 1440/390/320px, dark/light and all 11 translated locales pass. The
English menu is approximately 83px tall. No horizontal overflow or browser
errors. The broader containment script was attempted but stops before layout
checks: it still expects launch to redirect to `/packs`, while home is now `/`.
That pre-existing harness assumption is not changed in this menu patch.

[Compact desktop](composer-source-menu/compact-desktop.png) ·
[Compact narrow light](composer-source-menu/compact-narrow-light.png) ·
[Compact browser results](composer-source-menu/compact-results.json)
