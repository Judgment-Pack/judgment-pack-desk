# Plain-language pack views and pinned pane headers

The pack exposed several names for the same concept across its map, list,
document, Inspector and editor. The shared terminology registry now supplies
labels such as When this pack applies, Evidence needed, Decision rules, Special
cases, Result handling, Possible outcomes and Handoff settings. Known comparison
operators, unknown policies and trace observations also have display labels.
Exact document pointers, keys, enums, operand types and author values remain
unchanged; unknown vocabulary retains its original spelling.

Essential descriptions are visible. Optional examples for unknown-condition
policy and fallback use the reusable InfoHelp popover with click/tap, keyboard
activation, Escape dismissal and focus restoration. Technical details retain
the original JSON and document path. Existing Radix tooltips remain for brief
action hints; ordinary labels do not become hidden hover controls.

The main page and Inspector previously scrolled their headers with their
content. PageHeader/PageBody now share a fixed-header and scrolling-body
contract. The right pane applies the same separation, with its active tab body
scrolling below the pane title and tab list. New selections start at their
heading. The document observer uses the actual scrolling body.

Questions appear in the body with a measured two-line preview and an explicit
Read full question control. Expanding one never changes the header height.
Long titles stay compact, with their full text available in More details.

Validation uses an 80-rule copied fixture and deliberately long questions,
titles and Inspector prose. The browser regression covers independent scroll,
main/Inspector headers, Inspector tabs, deep links, group selection, help focus,
light/dark themes, narrow drawers, short windows, divider limits/reset, the
bottom console and the empty Inspector. It uses its own configuration, server
and explicit disposable test credential and cleans them up afterward.

```sh
JPACK_BIN=/path/to/jpack PLAYWRIGHT_CHROME=/path/to/chrome \
  node scripts/pane-help-check.mjs /path/to/jpack-desk \
  /path/to/vendor-onboarding.pack.json
```

Port 8804 must be free. The graph interaction regression remains on port 8803.
The full test, build and required containment results are recorded in the PR.

Design references: [Linear's design refresh](https://linear.app/now/behind-the-latest-design-refresh),
[Linear's settings refresh](https://linear.app/changelog/2024-12-18-personalized-sidebar),
[Radix Tooltip](https://www.radix-ui.com/primitives/docs/components/tooltip), and
[Radix Popover](https://www.radix-ui.com/primitives/docs/components/popover).
The local spacing, wording and help rules are product choices, not claims
about private Linear tokens.
