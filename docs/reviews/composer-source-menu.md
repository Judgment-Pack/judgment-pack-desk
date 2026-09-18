# Composer attachment menu

The reference is the user's ChatGPT screenshot: a plus menu contains named
sources, icons, and supporting descriptions. The official [ChatGPT plugin
guide](https://learn.chatgpt.com/docs/plugins) distinguishes installing a service
from connecting its tools; authentication may be required before use.

Desk now uses one AttachmentMenu in both home and pack chat. Upload files opens
the existing multi-file chooser; its description names the supported PDF and
text-file scope. Google Drive has a recognizable monochrome mark, preserving
Desk's neutral icon/color system. Its unavailable state is explicit and includes
the current download-and-upload workaround. Configuring the document extractor
does not enable Drive. The settings action is visually separated below sources.

No fake connection screen, new OAuth flow or provider capability is introduced.
A usable Drive action will require gateway retrieval and authenticated connection
state. This patch only adds the requested provider entry and improves the menu.

The menu reuses Radix and shared surface, hover, typography, spacing and border
tokens. It is anchored above the composer, bounded to the viewport, and flips
when necessary. The trigger tooltip is suppressed while open; Escape restores
focus. Disabled Drive is skipped by keyboard navigation. Item names and hints
have separate accessible labels/descriptions. All new copy is translated into
the existing 11 non-English locales.

Validation: production build/typecheck, localization coverage, 34 existing chat
tests; real browser multi-file chooser, keyboard navigation and Escape checks.
Screens at 1440, 390 and 320px fit with no horizontal overflow. German, Japanese
and Cantonese descriptions fit the narrow menu. No browser errors.

[Desktop](composer-source-menu/desktop.png) ·
[Narrow light theme](composer-source-menu/narrow-light.png) ·
[Browser results](composer-source-menu/results.json)
