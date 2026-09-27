# Minimal workspace review mocks

Static HTML/CSS/JS design previews following the clean-room Jobs and pack reviews. No production component is imported and no application API is called. Data, connection capabilities and sample checks are illustrative.

Individual pages: pack.html, logic.html, rule.html, edit-rule.html, jobs-inputs.html, jobs-source.html, jobs-trigger.html, jobs-review.html.

Each link opens directly into the named view. The top preview selector switches screens and the theme control toggles light/dark. On narrow screens graph nodes become compact reading rows and an open Details/source pane becomes the full working surface. Graph geometry is a static proposal, not an implemented React Flow redesign. Form edits are ephemeral and buttons cannot save, run jobs or connect accounts.

The job example uses installed MCP profiles as an illustrative target state; this does not claim they are configured in the current Desk installation. Scheduling requires an explicit record selector rather than reusing an event parameter. The pack map groups only force-outcome exceptions and never changes policy semantics by rearranging nodes.
