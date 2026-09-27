# Jobs operations design review

Review artifacts only; no production application changes. All jobs, connection names, results, dates and readiness checks shown are illustrative. Static controls navigate mock states or display a preview notice. No provider calls or writes occur.

- [Open the static mock](http://localhost:5173/mockups/jobs-operations-review/index.html#jobs)
- [Open the image gallery and full review](http://localhost:5173/mockups/jobs-operations-review/review.html)
- [Design and architecture](DESIGN.md)

21 states cover list, runs, creation, manual forms, mapped sources, API/MCP, model and storage configuration, local/manual/event triggers, future cloud adapters, release review, fresh operational inputs, run preview, initial brief generation, saved brief, execution settings and the empty state. Use the screen selector or previous/next controls above the app frame. A theme control switches light and dark.

Each state has `<state>-dark.png` and `<state>-light.png`. Selected compact layouts have `<state>-mobile.png`. Screenshots are rendered from the deterministic HTML/CSS mock, with the existing Desk font and matching hand-drawn icon geometry. Vite serves this directory at the URLs above; it is not included in the production application build. `review.html` is a rendered copy of DESIGN.md plus the image gallery.

Verified: 42 desktop screenshots (21 states × 2 themes) and 16 tablet/phone layout checks; no document overflow or browser script errors. Panels and tables have their own scroll areas. Validation does not imply implementation of the proposed features.
