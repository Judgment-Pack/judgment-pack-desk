# Packs collection and preview review

The collection's separate title row repeated sidebar context, while an empty
Inspector could inherit a wide document layout and squeeze pack descriptions.
The preview's heading and technical metadata competed with the collection.

The shared collection header now places navigation and the current view's action
on one row. All packs carries the count; search and Sort remain below. Names,
descriptions and versions align, with a small explicit preview control per row.
Container queries respond to the main pane's actual width, retaining warning
markers when descriptions move into preview.

`InspectorPresentation` lets a route borrow the existing pane and drawer without
changing saved document preferences. Collection preview starts closed and keeps
its own selection, open state and width when visiting a pack. Its width defaults
to 360px and ranges from 320–420px while preserving a 720px main working area;
smaller layouts use the existing modal drawer. These are Desk layout choices.

Preview shows the full available description, version and saved-case availability.
Paths and IDs sit under Technical details. Space toggles preview; arrow keys browse
adjacent entries without navigation. Escape closes preview and returns focus. On
the splitter, Escape resets width, with its tooltip suppressed during keyboard
operation so it cannot consume that gesture. Preview performs no runtime request.

This applies Linear's public [design refresh](https://linear.app/now/behind-the-latest-design-refresh),
[Peek behavior](https://linear.app/docs/peek), and
[view controls](https://linear.app/docs/display-options). It uses Desk's neutral
surfaces, shared typography, controls and restrained Judgment Pack accents.

## Verification

- Production build/typecheck passed.
- 76 focused component checks passed, including state isolation, retained list
  navigation, filtering, keyboard preview and shared divider controls.
- Chrome passed eight viewport/theme/density combinations from 320–1512px,
  preview/document transitions, header alignment, sort selection, resizing,
  153 virtualized rows, long text and empty searches. The final pass also checks
  a malformed-pack warning at phone width and opening a document from the drawer.
  Zero page errors and zero test-suite requests were observed. Results and
  screenshots are in `packs-preview/`.
- The complete component suite (130 files) and Go checks passed on GitHub CI.
  Local broad runs encountered host-load timeouts and the host's inotify limit;
  assertions and timeouts were not relaxed. The 435-configuration route/pane
  gate runs before merge, with its final result recorded on the pull request.

Browser verification uses a disposable copy of the latest runtime's graph project
fixtures and runtime 0.21.0. The design changes introduce no runtime, gateway or
specification contract changes.
