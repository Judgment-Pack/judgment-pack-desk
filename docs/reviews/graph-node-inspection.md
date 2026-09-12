# Graph node inspection regression

The previous browser build reproduced the report: clicking Rules left
Applicability selected, retained `at=/applicability`, and opened the general
Pack outline. The React Flow attribution was visible.

The shared graph now disables that badge through React Flow’s public option.
Fixed nodes use `nopan`, so small pointer movements do not turn inspection into
a canvas pan. Nodes expose button/pressed semantics and keep Enter/Space access.

The pack handler now routes multi-item and empty nodes to a selected group;
previously it changed selection only when a group contained exactly one item.
The Inspector lists the selected group’s entries, and choosing one restores its
real document pointer. Group IDs are view state, never synthetic JPS pointers.

Validation:

- Production frontend build and all 3,039 tests passed across 126 files.
- Chromium checked all seven nodes: rules, evidence, exceptions, resolution,
  outcomes, sources and applicability. Exactly one node is selected each time.
- Mouse, Enter, Space and touch open the corresponding Inspector. A small pointer
  movement leaves the canvas still; empty-canvas panning and zoom remain usable.
- Selection is retained across reload and the 640px Inspector drawer transition.
  An empty Sources group opens a named empty state. Light and dark render without
  the React Flow badge; no browser exceptions occurred.
- Browser work used a copied fixture, disposable configuration and a generated
  test credential supplied explicitly to server and browser. No existing session
  credentials or launch-log tokens were read.

The required 435-configuration containment result is recorded in the PR before
merge. See the shared [design rules](../design-system.md#relationship-map-interaction).


Run the actual React Flow regression after building the frontend and desk binary:

```sh
JPACK_BIN=/path/to/jpack PLAYWRIGHT_CHROME=/path/to/chrome \
  node scripts/graph-interaction-check.mjs /path/to/jpack-desk \
  /path/to/judgment-pack-demo/projects/enterprise-demo/packs/vendor-onboarding.pack.json
```

The script uses port 8803, creates its own project with a copy of the fixture,
and cleans up its server and temporary data. It also derives an empty-Sources
case without modifying the supplied fixture. The component tests cover routing
independently of the React Flow renderer; this script covers actual hit testing.
