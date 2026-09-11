/**
 * The shell sheet's three load-bearing rules, read off the committed file.
 *
 * **Why a sheet test and not a rendered one.** jsdom implements the cascade
 * but not layout: it will report `height: 100dvh` and lay nothing out, so a
 * `getBoundingClientRect` in this environment cannot tell a scrolling console
 * from a clipped one. What it *can* hold in place is the declarations the
 * browser measurement was taken against — so the numbers live in the PR and
 * the rules live here, and a change that quietly reverts one fails.
 *
 * The DOM half of the console rule — that `Tabs.Root` actually carries the
 * class these declarations select — is asserted in `BottomPane.test.tsx`,
 * because a rule with no element is not a fix.
 */
import { readFileSync } from 'node:fs'
import { join } from 'node:path'
import { describe, expect, it } from 'vitest'

const SHEET = readFileSync(join(import.meta.dirname, '..', 'shell.css'), 'utf8')
/** The token file, for the half of the measure that is a number rather than a rule. */
const TOKENS = readFileSync(join(import.meta.dirname, '..', 'styles.css'), 'utf8')

/** One rule's declarations, by selector, at any indentation. */
function declarations(selector: string): string {
  const opening = SHEET.indexOf(`\n  ${selector} {`)
  expect(opening, `${selector} is declared`).toBeGreaterThan(-1)
  const start = SHEET.indexOf('{', opening)
  const end = SHEET.indexOf('}', start)
  return SHEET.slice(start + 1, end)
}

/** Every declared value for one property in one rule, in source order. */
function values(selector: string, property: string): string[] {
  return [...declarations(selector).matchAll(new RegExp(`^\\s*${property}:\\s*([^;]+);`, 'gm'))].map(
    (match) => match[1]!.trim()
  )
}

describe('the frame has a definite height', () => {
  it('sets height rather than min-height on the grid', () => {
    // `min-height` leaves the available space indefinite, so the `1fr` row
    // grows to fit its content instead of dividing the viewport: a long Admin
    // page or a thirty-pack rail stretched the grid, `.desk-main` never became
    // the scroll container, and the always-visible strip went below the fold.
    expect(values('.desk', 'min-height')).toEqual([])
    expect(values('.desk', 'height')).toEqual(['100vh', '100dvh'])
    expect(values('.desk', 'overflow')).toEqual(['hidden'])
  })

  it('gives every pane that scrolls a floor to shrink to', () => {
    // A grid item's automatic minimum size is its content, so a pane without
    // this refuses to shrink and pushes the row open again.
    for (const pane of ['.desk-main', '.desk-inspector', '.desk-console']) {
      expect(values(pane, 'min-height'), `${pane} can shrink`).toEqual(['0'])
    }
  })
})

describe('the console is a flex column all the way down', () => {
  it('declares the middle link the tab root sits on', () => {
    // `.desk-console` is the fixed-height flex parent and `.desk-console-body`
    // claims `flex: 1`, but the tab root between them was an ordinary block:
    // the body had neither a flex parent nor a constrained height, so a log
    // longer than the pane was clipped by the console's `overflow: hidden`.
    const tabs = declarations('.desk-console-tabs')
    expect(tabs).toMatch(/display:\s*flex;/)
    expect(tabs).toMatch(/flex-direction:\s*column;/)
    expect(tabs).toMatch(/flex:\s*1;/)
    expect(tabs).toMatch(/min-height:\s*0;/)
    expect(values('.desk-console-body', 'overflow')).toEqual(['auto'])
  })
})

describe('the status strip keeps its warning', () => {
  it('lets the connection text give way and never the refusal link', () => {
    // The lane used to carry `overflow: hidden` with the nowrap connection
    // sentence first, so the cue that the configuration was refused was the
    // half that disappeared — at exactly the width where the screen is
    // smallest and the operator is least likely to open Admin.
    expect(values('.desk-strip-left', 'overflow')).toEqual([])
    const connection = declarations('.desk-strip-connection')
    expect(connection).toMatch(/overflow:\s*hidden;/)
    expect(connection).toMatch(/text-overflow:\s*ellipsis;/)
    expect(connection).toMatch(/min-width:\s*0;/)
    const warn = declarations('.desk-strip-warn')
    expect(warn).toMatch(/flex:\s*0 0 auto;/)
    expect(warn).toMatch(/white-space:\s*nowrap;/)
  })
})

describe('no pane may eat the frame, whatever the file says', () => {
  it('caps both side columns and the console against the viewport', () => {
    // The decoder refuses an absurd dimension by name, which is the right
    // place for it — but a size legal on a 27-inch monitor still eats the
    // frame on a phone, and the frame is clipped and does not scroll. So the
    // grid takes the smaller of the configured track and a viewport-relative
    // cap.
    const rule = declarations('.desk')
    const columns = /grid-template-columns:([^;]+);/.exec(rule)![1]!
    expect(columns).toContain('min(var(--rail-current), var(--side-cap))')
    expect(columns).toContain('min(var(--inspector-current), var(--side-cap))')
    const rows = /grid-template-rows:([^;]+);/.exec(rule)![1]!
    const normalised = rows.replace(/\s+/g, ' ')
    expect(normalised).toContain(
      'min( var(--console-current), max(var(--console-cap), min(var(--console-floor), var(--console-room))) )'
    )
  })

  it('reserves main’s share in the caps themselves', () => {
    const root = declarations(':root')
    // 40% each side leaves main at least 20% with both panes open.
    expect(root).toMatch(/--side-cap:\s*40vw;/)
    // And the console stops where 120px of route would otherwise go.
    expect(root).toMatch(/--main-floor:\s*120px;/)
    // Exactly one unguarded `--console-cap`, and it is the `vh` one. A second
    // declaration here is not a fallback: a custom property is not validated
    // at parse time, so `100dvh` is a valid token stream to a browser that has
    // never heard of `dvh` and would *win* — the invalidity then surfaces at
    // substitution and takes the whole `grid-template-rows` with it.
    const caps = [...root.matchAll(/--console-cap:\s*([^;]+);/g)].map((m) => m[1]!.trim())
    expect(caps).toHaveLength(1)
    expect(caps[0]!.startsWith('max(0px,')).toBe(true)
    expect(caps[0]).toContain('100vh')
    expect(caps[0]).not.toContain('100dvh')
    expect(caps[0]).toContain('var(--header-h)')
    expect(caps[0]).toContain('var(--strip-h)')
    expect(caps[0]).toContain('var(--main-floor)')
  })

  it('puts the dvh value behind @supports, where a fallback actually works', () => {
    // The two-declaration fallback is correct for a real property and wrong
    // for a custom one, which is the whole of this rule.
    const guard = SHEET.indexOf('@supports (height: 100dvh)')
    expect(guard, 'the dvh override is guarded').toBeGreaterThan(-1)
    const block = SHEET.slice(guard, SHEET.indexOf('\n  }\n', guard))
    expect(block).toContain('--console-cap')
    expect(block).toContain('100dvh')
    // And `height` itself keeps the plain pair, because there the fallback is
    // the mechanism: an unsupported `100dvh` makes that declaration invalid at
    // parse time and the `100vh` before it stands.
    expect(values('.desk', 'height')).toEqual(['100vh', '100dvh'])
  })

  it('gives an open console a floor the cap cannot take away', () => {
    // On a viewport too short for the reserve the cap reaches zero, and a
    // console the viewer has opened would render at no height at all while
    // its toggle still said expanded. The floor is the decoder's own minimum
    // for a console; main scrolls and takes the squeeze.
    expect(declarations(':root')).toMatch(/--console-floor:\s*80px;/)
    // Collapsed is still exactly zero: `--console-current` is `0px` then, and
    // `min()` picks it over the floor.
    expect(declarations(':root')).toMatch(/--console-current:\s*0px;/)
  })

  it('bounds the floor by the room that actually exists, so the strip never goes', () => {
    // The floor without this traded one defect for the other: 80px on a 109px
    // viewport pushed the status strip out of a frame that does not scroll,
    // which is exactly what the cap exists to prevent. `--console-room` is
    // everything between the header and the strip, and the floor cannot ask
    // for more than that.
    const room = [...declarations(':root').matchAll(/--console-room:\s*([^;]+);/g)].map((m) =>
      m[1]!.trim()
    )
    expect(room).toHaveLength(1)
    expect(room[0]!.startsWith('max(0px,')).toBe(true)
    expect(room[0]).toContain('100vh')
    expect(room[0]).toContain('var(--header-h)')
    expect(room[0]).toContain('var(--strip-h)')
    // It reserves nothing for main — main is what gives way here.
    expect(room[0]).not.toContain('var(--main-floor)')
    // And its dvh twin is guarded, on the same terms as the cap's.
    const guard = SHEET.indexOf('@supports (height: 100dvh)')
    const block = SHEET.slice(guard, SHEET.indexOf('\n  }\n', guard))
    expect(block).toContain('--console-room')
  })
})

describe('the strip’s cue fits the strip', () => {
  it('paints a short spelling below 600px and the full one above', () => {
    // The full sentence is about 263px in the strip's own face; a 320px
    // viewport leaves roughly 232px beside the console button. Unshrinkable
    // and unwrappable by design, it painted across that button and off the
    // edge of a frame that clips.
    expect(declarations('.desk-strip-warn-short')).toMatch(/display:\s*none;/)
    const narrow = SHEET.slice(SHEET.indexOf('@media (max-width: 599px)'))
    expect(narrow.slice(0, 260)).toMatch(/\.desk-strip-warn-full\s*\{\s*display:\s*none;/)
    expect(narrow.slice(0, 260)).toMatch(/\.desk-strip-warn-short\s*\{\s*display:\s*inline;/)
  })
})

describe('the measure is left-aligned, at one gutter, capped by kind', () => {
  it('never centres the column, and starts it at the gutter token', () => {
    // The defect: `max-width: 60rem; margin: 0 auto` centred the content in
    // the main pane, so on a 1893px Admin there was dead space on both sides
    // and the content was aligned to nothing — not to the rail, and not to the
    // middle either, because Admin's own 44rem sat left-aligned inside it. An
    // app with a persistent left rail does not centre.
    const margin = values('.desk-measure', 'margin')
    expect(margin).toEqual(['0'])
    expect(margin.join(' '), 'a centred measure has no left edge to hold').not.toContain('auto')
    expect(values('.desk-measure', 'max-width')).toEqual(['var(--measure-wide)'])
    // Both numbers come from the scale, so a compact desk tightens the gutter
    // as it tightens everything else — and the left edge is the gutter.
    expect(values('.desk-measure', 'padding')).toEqual([
      'var(--density-section) var(--density-gutter) 4rem'
    ])
  })

  it('reads the three kinds a route can state, each from its own token', () => {
    // `wide` is the default and is still declared. Without the rule the page
    // would still be 72rem, so a route that stated nothing would look right
    // and be held by nothing — which is why `routes/measure.test.ts` requires
    // the attribute and this requires the rule that reads it.
    expect(values('.desk-measure:has([data-measure="form"])', 'max-width')).toEqual([
      'var(--measure-form)'
    ])
    expect(values('.desk-measure:has([data-measure="wide"])', 'max-width')).toEqual([
      'var(--measure-wide)'
    ])
    // `full` is the one kind with no token, because there is no number in it.
    expect(values('.desk-measure:has([data-measure="full"])', 'max-width')).toEqual(['none'])
  })

  it('defines both caps and the gutter on :root, and tightens the gutter', () => {
    // The caps are not on the density scale: a measure is a reading width and
    // not a rhythm, and `--measure-form` at a compact density would be a form
    // whose columns move when somebody tightens the row height.
    expect(TOKENS).toMatch(/\n {2}--measure-form: 44rem;/)
    expect(TOKENS).toMatch(/\n {2}--measure-wide: 72rem;/)
    expect(TOKENS).toMatch(/\n {2}--density-gutter: 1\.5rem;/)
    // The compact value is held strictly smaller by `ui/palette.test.ts`,
    // which sweeps the whole `--density-` prefix; what is held here is that
    // the gutter is on that scale at all, which is what puts it in the sweep.
    expect(TOKENS).toMatch(/\n {2}--density-gutter: 1\.25rem;/)
  })

  it('takes the gutter narrower by viewport, and not by density', () => {
    // 2rem each side of a 320px screen is a third of the screen spent on
    // nothing, whichever density is chosen — so it is keyed on the width, at
    // the shell's own narrow breakpoint, and deliberately not in the compact
    // block, where it would tighten a 1900px desk and leave a comfortable
    // phone at 2rem.
    const narrow = TOKENS.slice(TOKENS.indexOf('@media (max-width: 599px)'))
    expect(narrow.slice(0, 120)).toMatch(/:root \{\s*--density-gutter: 1rem;/)
    const compact = TOKENS.slice(
      TOKENS.indexOf(':root[data-density="compact"] {'),
      TOKENS.indexOf('@media (max-width: 599px)')
    )
    expect(compact).not.toContain('--density-gutter: 1rem;')
  })

  it('leaves no page holding a measure of its own', () => {
    // `.admin { max-width: 44rem }` was the width a label column and a value
    // column need, measured on that page and living in that page's module —
    // so the next form-shaped page had nowhere to read it from and would have
    // spelt 44rem again. The page states `data-measure="form"` now.
    const admin = readFileSync(
      join(import.meta.dirname, '..', 'routes', 'AdminView.module.css'),
      'utf8'
    )
    // Comments out: the module still *says* what the number was and where it
    // went, and a check that could not tell a sentence from a declaration
    // would be asking the file to forget its own history.
    expect(admin.replaceAll(/\/\*[\s\S]*?\*\//g, '')).not.toContain('max-width')
  })
})

describe('a drawer is the width its pane asks for', () => {
  it('reads --drawer-w, and falls back to the number that was hard-coded', () => {
    expect(values('.desk-drawer', 'width')).toEqual(['min(var(--drawer-w, 320px), 85vw)'])
  })
})
