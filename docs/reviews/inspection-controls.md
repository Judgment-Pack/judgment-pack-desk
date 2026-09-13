# Consistent inspection controls

The Overview used underlined text, quiet buttons and a bordered navigation
button for related reading interactions. Outcome labels such as Resolve looked
like commands even though clicking them only opened a definition. Inspector
member lists had another independent treatment. Retry and Author controls still
used legacy global button classes.

`InspectionRow` now gives conditions, evidence, outcomes, source references and
Inspector lists one pattern: displayed information with a trailing chevron,
neutral hover/current fill and a stable text color. The accessible name describes
viewing the item, and native buttons support Enter and Space. Current information
uses `aria-current`, not a toggle's pressed state. Labels and values wrap without
changing the exact pack content. Existing pointer selection and drawer behavior
remain in the route.

`Disclosure` provides the in-place expansion pattern for author descriptions and
Inspector technical, file, reference and check details. Its leading chevron
rotates with native details/summary state. Controlled attention disclosures keep
their existing open-state behavior. Group lists no longer repeat a visible raw
JSON array; the exact array remains under Technical details.

Overview keeps the active-view underline. View logic is a quiet navigation link
beside its section heading, and one redundant section separator is removed.
Retry connection and Author actions use the shared Button. Their callbacks,
write guards and disabled conditions remain intact; the unused global button
and link-button CSS is removed. These are local product choices informed by
[Linear's design refresh](https://linear.app/now/behind-the-latest-design-refresh),
[Linear's settings redesign](https://linear.app/changelog/2024-12-18-personalized-sidebar),
and the W3C [button](https://www.w3.org/WAI/ARIA/apg/patterns/button/) and
[disclosure](https://www.w3.org/WAI/ARIA/apg/patterns/disclosure/) patterns.

Validation includes a route regression that checks each Overview action's exact
pointer, current-item feedback, read-only runtime calls and collapsed technical
JSON. The browser regression uses copied fixtures, a disposable configuration
and generated test credential. It checks mouse/Enter/Space, stable hover and
selection geometry, disclosures, long labels, many outcomes, empty groups,
Inspector scroll reset, docked/drawer forms, both themes and density settings,
and unchanged fixture contents. No user session or credentials are accessed.

```sh
JPACK_BIN=/path/to/jpack PLAYWRIGHT_CHROME=/path/to/chrome \
  node scripts/inspection-controls-check.mjs /path/to/jpack-desk \
  /path/to/vendor-onboarding.pack.json
```

Port 8805 must be free. The browser check writes desktop, light and narrow
screenshots under `/tmp/jp-controls-overview-*.png`. Full test, build and required
containment results are recorded in the PR.
