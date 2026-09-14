# Create pack workspace mocks

These are rendered React fixture mocks using Desk's current type, spacing, color
and control tokens. They are a separate development entry, not the production
Create route. Neutral surfaces and selection states remain unchanged; green/gold
appear only in restrained brand, primary-action and status accents.

| Stage | Dark | Light |
| --- | --- | --- |
| Initial brief | [Image](initial-dark.png) | [Image](initial-light.png) |
| Active drafting | [Image](active-dark.png) | [Image](active-light.png) |
| Review changes | [Image](review-dark.png) | [Image](review-light.png) |

[Narrow chat view](active-mobile.png).

The initial page centers one composer. After starting, the main pane displays the
draft and test evidence while the right pane holds the conversation. Headers stay
outside scrolling content; the composer stays reachable. On narrow screens,
Draft/Chat switches preserve the workspace instead of compressing both panes.
The bottom panel contains activity detail. It is not a second chat or inspector.

Preview, Changes, Tests and Sources separate understanding the current draft from
reviewing the assistant's work. Create pack becomes available at review. The final
production action must create only the reviewed candidate through the existing
write path. Editing should reuse this workspace with Apply changes and a saved
baseline rather than another set of components.

The mock reuses `Button`, `TextArea`, `Select`, `SegmentedControl`, Radix-backed
tooltips and the structured `ConditionTree`. Its shell is an isolated fixture;
production must reuse `AppShell` and its pane resizing constraints.

## Open and exercise

From `web`, start `npm run dev`, then open
`http://localhost:5173/authoring-preview.html`. Query parameters select
`state=initial|active|review`, `theme=dark|light` and
`density=comfortable|compact`.

Start drafting, Stop, Review draft, Preview/Changes/Tests/Sources and the narrow
Draft/Chat switch demonstrate navigation. Attachment selection/drop adds names;
it does not extract file content. Models, tools, messages and test evidence are
fixtures. Create pack explicitly reports that no pack was created. The footer
labels the page as a design preview and provides stage/theme controls.

To regenerate screenshots and check layouts with an installed Chromium:

```sh
PLAYWRIGHT_CHROMIUM_EXECUTABLE_PATH=/absolute/path/to/chromium node scripts/authoring-preview.mjs
```

The script checks all three stages at five viewport sizes (390–1440px), both themes
and both densities. It also exercises stop/review/test navigation and fixed header
position. This is fixture layout evidence, not production chat accessibility or
agent integration certification.
