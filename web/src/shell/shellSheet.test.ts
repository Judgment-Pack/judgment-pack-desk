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

describe('a drawer is the width its pane asks for', () => {
  it('reads --drawer-w, and falls back to the number that was hard-coded', () => {
    expect(values('.desk-drawer', 'width')).toEqual(['min(var(--drawer-w, 320px), 85vw)'])
  })
})
