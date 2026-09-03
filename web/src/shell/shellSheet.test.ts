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
    expect(rows).toContain('min(var(--console-current), var(--console-cap))')
  })

  it('reserves main’s share in the caps themselves', () => {
    const root = declarations(':root')
    // 40% each side leaves main at least 20% with both panes open.
    expect(root).toMatch(/--side-cap:\s*40vw;/)
    // And the console stops where 120px of route would otherwise go.
    expect(root).toMatch(/--main-floor:\s*120px;/)
    // `max(0px, …)` keeps the track non-negative on a viewport shorter than
    // the reserve; a bare `calc` there would be invalid and take the grid
    // with it. Declared twice so a browser without `dvh` keeps the `vh` one.
    const caps = [...root.matchAll(/--console-cap:\s*([^;]+);/g)].map((m) => m[1]!.trim())
    expect(caps).toHaveLength(2)
    expect(caps.every((cap) => cap.startsWith('max(0px,'))).toBe(true)
    expect(caps[0]).toContain('100vh')
    expect(caps[1]).toContain('100dvh')
    for (const cap of caps) {
      expect(cap).toContain('var(--header-h)')
      expect(cap).toContain('var(--strip-h)')
      expect(cap).toContain('var(--main-floor)')
    }
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

describe('a drawer is the width its pane asks for', () => {
  it('reads --drawer-w, and falls back to the number that was hard-coded', () => {
    expect(values('.desk-drawer', 'width')).toEqual(['min(var(--drawer-w, 320px), 85vw)'])
  })
})
