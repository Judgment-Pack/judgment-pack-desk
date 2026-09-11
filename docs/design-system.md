# Unveil design system

Unveil uses a calm workspace shell and clearly grouped settings. Neutral
navigation leaves attention on the task; teal identifies primary actions.
Settings are comfortable to read, while lists and inspectors remain compact.
Admin → Assistant and Packs are the first complete examples of this direction.

## One source of truth

| Concern | Owner |
| --- | --- |
| Color, typography, spacing, radius, density | `web/src/styles.css` |
| Header, navigation, main, inspector, console geometry | `web/src/shell.css` |
| Theme and density application | `web/src/config/theme.ts` |
| Saved appearance preferences | `web/src/shell/appearanceState.ts` |
| Reusable controls and settings sections | `web/src/ui/` |
| Feature composition | Its route or feature CSS module |

Use the existing user menu's appearance controls for **System / Light / Dark**
and **Comfortable / Compact**. A viewer's preference overrides the project's
configured appearance. Do not introduce a second theme store or route-specific
palette. Existing explicit sidebar widths remain respected; the new default is
224px.

## Live reference

Run `npm --prefix web run dev` and open `/design-system.html`. This development
page renders the actual Button, Field, Input, Select, TextArea and SettingsSection
components. It needs no runtime or credentials. Its theme and density controls
apply only to that page and do not persist preferences. Example actions have no
side effects. The production build's entry remains `index.html`.

Use this page to check default, hover, focus, disabled, invalid and loading
presentations before applying component changes to a product workflow.

## Foundations

| Token | Comfortable value / purpose |
| --- | --- |
| `--text-page` | 24px, page titles |
| `--text-heading` | 20px, major sections |
| `--text-body` | 15px, body text and settings group titles |
| `--text-label`, `--text-control` | 14px, labels and controls |
| `--text-sm` | 13px, helpers and secondary text |
| `--text-xs` | 12px, compact metadata |
| `--space-1` … `--space-7` | 4, 8, 12, 16, 24, 32, 48px |
| `--density-control` | 36px; 32px in Compact |
| `--density-form-control` | 40px; 36px in Compact |
| `--density-row` | 40px; 32px in Compact; matches list virtualization |
| `--density-gutter` | 24px; 20px in Compact; 16px on narrow screens |
| `--radius`, `--radius-sm` | 8px controls/panels; 6px nested items |
| `--measure-form`, `--measure-wide` | 704px forms; 1152px documents/lists |

Sizes assume the default 16px root. Use rem-based typography so browser text
preferences and zoom remain effective. Use `--density-*` for dimensions that
should tighten with the user's preference and `--space-*` for fixed relationships.
A settings container selects the shared size with
`--density-control: var(--density-form-control)`; it must not target descendants
to override Button, Input or Select dimensions.

Use semantic colors: `--ink` for primary text, `--ink-soft` for secondary content,
`--ink-faint` for helpers, `--accent` for actions, and `--danger` / `--warn` for
their respective states. Every color is defined in both palettes. Do not add
literal colors or radii to component modules.

## Components and page patterns

- Use `Button` variants `primary`, `secondary`, `quiet` and `danger`. A loading
  action is disabled, says what is happening, and can carry `aria-busy`.
- Use `Field` for an associated label, helper and validation error. Use the
  shared controls inside its render callback so their accessibility wiring is
  retained. A placeholder does not replace a label.
- Use `SettingsSection` to group related fields. Its optional footer holds
  actions belonging to that group. It owns its padding and border; children
  own their internal spacing. Choose heading level 2 or 3 to match the page.
- Use an inline action row for changes saved across multiple groups. Name the
  scope, such as **Save API key** or **Save settings**. Never float a save bar
  over scrolling content.
- Place destructive actions in a separate section and retain explicit
  confirmation for removal. Destructive actions must use the shared danger
  variant, not a feature-specific color override.
- Keep runtime paths and diagnostic details in a disclosure or inspector.
  Display read failures and actionable errors without requiring discovery.
- Preserve data provenance: runtime verdicts, model IDs, diagnostics and tool
  permissions retain their meaning when their presentation changes.

## Cascade and maintenance

`main.tsx` imports `styles.css` before `shell.css`. The order declaration
`@layer base, shell` places element defaults below navigation styling. Feature
and UI modules remain unlayered, so their own classes take precedence. Do not
fix a cascade conflict with increasingly specific descendant selectors.

New UI primitives live beside their own CSS module in `web/src/ui/`. Add them to
the live reference when they define a reusable pattern. Extend existing tokens
before introducing a new component-specific scale. Existing route styles can be
migrated incrementally; the reference page is not a claim that every legacy
screen has completed visual review.

Build and run the component suite after changing shared components. The existing
palette, convention, and density checks validate the foundations. Use the real
browser containment check before merging stylesheet changes. Review Admin,
Packs, a populated pack, a dialog, and a narrow layout in both themes; passing
source tests alone does not demonstrate visual quality.

## Reviewed examples

These captures use copied demo data and a placeholder key. Provider replies in
connection tests were mocked; they are examples of the UI states, not evidence
of a live provider connection.

- [Assistant setup, dark](design/admin-assistant-dark.png)
- [Inline saving and separate removal](design/admin-actions-dark.png)
- [Packs navigation](design/packs-dark.png)
- [Populated pack, light](design/pack-light.png)
- [Shared component reference, dark](design/design-system-dark.png)
