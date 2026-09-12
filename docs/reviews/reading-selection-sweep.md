# Reading and selection sweep

The previous reading page outlined selection in the same green as keyboard
focus, scrolled long conditions horizontally, and shrank nested code below the
metadata type size. Inspector copies repeated the selection targets and nested
tabs. The sweep fixes those shared behaviors and reduces supporting chrome.

## Reference and decisions

[Linear’s design refresh](https://linear.app/now/behind-the-latest-design-refresh)
supports quieter supporting UI and coherent foundations. Its
[editor improvements](https://linear.app/changelog/2024-04-24-editor-improvements)
provide a useful reference for a compact table of contents. The desk’s explicit
focus and wrapping rules follow
[visible keyboard focus](https://www.w3.org/WAI/WCAG22/Understanding/focus-visible.html)
and [text reflow](https://www.w3.org/WAI/WCAG22/Understanding/reflow.html).
These are desk implementation decisions, not claims about Linear’s private CSS.

| Finding | Shared correction |
| --- | --- |
| Selection looks like a focused input | Neutral block background, unchanged text, separate keyboard focus |
| Long applicability/rule/exception operands scroll sideways | Default wrapping; complete array entries stack when long |
| Inspector repeats addressed blocks | Read-only copies without duplicate IDs, focus stops or selection |
| Descriptions look like JSON strings | Prose by default, exact quoted value in a disclosure |
| Nested code shrinks twice | One 12px source size with inherited inner code |
| Inspector has three navigation levels | One mode row with item details and counted disclosures |
| Full document appears to be Logic | Explicit Full document active label |
| Outline wraps into a dense strip | Portaled On this page control, current section and omission links retained |
| Metadata actions are indented relative to values | Reusable inline button variant |
| Ordinary validation competes with content | Quiet success/provenance text; failures remain visible |

## Verification

- Production build and all 3,033 tests passed (125 files). Regression coverage
  includes exact typed operands, nested pointers, text-selection preservation,
  read-only Inspector copies, clipboard success/failure and wrap toggling.
- Browser checks used a copied fixture and a newly generated credential supplied
  explicitly to the disposable server and browser. No user session or log
  credential was read. Pointer selection measured `rgb(34, 35, 38)`, matching
  dark neutral selection, with no outline. Keyboard focus remained visible.
- Long array entries, including a 560-character unbroken operand, retained their
  complete value. Exact JSON copied identically after changing wrap mode.
- Light and dark layouts were checked at 1700, 1100, 640, 479 and 320px; no page
  horizontal overflow or browser exceptions were observed.
- The mutation-needle scan retains the existing eight stale needles and one
  ambiguous Go needle; the two affected document-renderer needles were updated.
  This is not a claim that the whole mutation matrix passes.

The required 435-configuration containment gate is recorded in the PR before
merge. Reflow checks do not constitute complete accessibility certification.
