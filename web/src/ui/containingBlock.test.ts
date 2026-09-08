/**
 * A scroll container is a containing block — read off every committed sheet.
 *
 * **Why the source and not a render.** vitest runs with `css: false`, so no
 * stylesheet is processed at all: a pane whose `position` was deleted renders
 * exactly like one that still carries it, and jsdom lays nothing out, so a
 * `getBoundingClientRect` here could not tell a document that scrolls from one
 * that does not. The browser measurement lives in the PR body; what lives here
 * is the source that measurement was taken against, in the `palette.test.ts`
 * idiom.
 *
 * **The defect this replaces.** `overflow` scrolls and clips only the
 * descendants whose containing block lies inside the scroller. Every pane of
 * the shell was `position: static`, so an absolutely positioned descendant was
 * laid out against the initial containing block instead — not scrolled with
 * its pane, not clipped by the frame, and its static position counted into the
 * *document's* scrollable overflow. On `/admin` at 1400x800 that measured
 * `document.scrollingElement.scrollHeight` 2439 against an `innerHeight` of
 * 800: the browser painted its own scrollbar and the whole 100dvh shell could
 * be scrolled up out of the window. The three elements past the fold were the
 * 1px `select[aria-hidden="true"]` that Radix renders beside every Select
 * trigger inside a `<form>` — the assistant endpoint form's Wire protocol,
 * Engine and Thinking pickers, at y 1808, 2341 and 2438.
 *
 * **So this holds the pair and not the five names.** Any rule in any of these
 * sheets that scrolls must also position itself, whoever adds it and whenever.
 * A list of the panes that were broken in September would be satisfied by the
 * sixth scroller somebody writes in October.
 */
import { readFileSync, readdirSync } from 'node:fs'
import { join, relative, sep } from 'node:path'
import { describe, expect, it } from 'vitest'

const SRC = join(import.meta.dirname, '..')

/** One rule: the prelude that opened it, and the declarations it owns. */
interface Rule {
  selector: string
  declarations: { property: string; value: string }[]
  where: string
}

/**
 * Every rule in a sheet, at any nesting depth, with the declarations it owns
 * and not those of the rules nested inside it.
 *
 * `declarations.ts` splits a sheet into `property: value` pairs and drops the
 * selectors, which is the right shape for "does this sheet spell a colour" and
 * the wrong one here: the claim is about a *pair inside one rule*, so a sheet
 * that declares `overflow: auto` in one rule and `position: relative` in
 * another must fail. The splitter is the same one, extended to keep the
 * bracket depth it is already tracking.
 */
function rulesIn(sheet: string, where = ''): Rule[] {
  const text = sheet.replace(/\/\*[\s\S]*?\*\//g, '')
  const rules: Rule[] = []
  const open: Rule[] = []
  let buffer = ''
  let quote: string | undefined
  let parens = 0

  const flush = () => {
    const chunk = buffer.trim()
    buffer = ''
    // An at-rule with no block — `@import url(x);` — is not a declaration.
    if (chunk === '' || chunk.startsWith('@')) return
    const colon = chunk.indexOf(':')
    if (colon === -1) return
    const rule = open.at(-1)
    if (rule === undefined) return
    rule.declarations.push({
      property: chunk.slice(0, colon).trim().toLowerCase(),
      value: chunk.slice(colon + 1).trim()
    })
  }

  for (const character of text) {
    if (quote !== undefined) {
      buffer += character
      if (character === quote) quote = undefined
      continue
    }
    if (character === '"' || character === "'") {
      quote = character
      buffer += character
      continue
    }
    if (character === '(') parens += 1
    if (character === ')') parens = Math.max(0, parens - 1)
    if (parens === 0 && character === '{') {
      const rule: Rule = { selector: buffer.trim().replace(/\s+/g, ' '), declarations: [], where }
      buffer = ''
      open.push(rule)
      rules.push(rule)
      continue
    }
    if (parens === 0 && character === '}') {
      // A rule may end without a trailing semicolon.
      flush()
      open.pop()
      continue
    }
    if (parens === 0 && character === ';') {
      flush()
      continue
    }
    buffer += character
  }
  return rules
}

/**
 * A declared value with `!important` taken off it.
 *
 * `position: relative !important` positions exactly as `position: relative`
 * does, and a reader that compared the whole string would call the first one
 * unpositioned — a false failure that teaches the next author to delete the
 * test rather than the flag.
 */
function tokens(value: string): string[] {
  return value
    .toLowerCase()
    .split(/\s+/)
    .filter((token) => token !== '' && token !== '!important')
}

/** A value that puts the element on a scrollbar, by token and not by substring. */
function scrolls(value: string): boolean {
  const words = tokens(value)
  // `overlay` is the legacy spelling of `auto` — removed from the standard,
  // still parsed by the engines this desk runs in, and a scroll container
  // wherever it is honoured.
  return words.includes('auto') || words.includes('scroll') || words.includes('overlay')
}

/**
 * The five properties that can author a scrolling overflow, logical spellings
 * included. `overflow-wrap`, `text-overflow`, `overflow-anchor` and
 * `overflow-clip-margin` share the prefix and create no scroll container, so
 * they are deliberately not here.
 */
const OVERFLOW = new Set([
  'overflow',
  'overflow-x',
  'overflow-y',
  'overflow-block',
  'overflow-inline'
])
const POSITIONED = new Set(['relative', 'absolute', 'fixed', 'sticky'])

/**
 * Every stylesheet under `web/src`, by extension and not by name.
 *
 * The first draft of this walked `*.module.css` and joined `shell.css` and
 * `styles.css` on by hand, which is a list of names wearing a sweep's clothes:
 * a third plain sheet — `packs/extra.css`, say — with `overflow: auto` in it
 * was invisible to the whole file and passed green. A sheet is a `.css` under
 * `src`; there is no other kind.
 */
function everySheet(directory: string): string[] {
  const found: string[] = []
  for (const entry of readdirSync(directory, { withFileTypes: true })) {
    const path = join(directory, entry.name)
    if (entry.isDirectory()) found.push(...everySheet(path))
    else if (entry.name.endsWith('.css')) found.push(path)
  }
  return found
}

const sheets = everySheet(SRC).sort()
const short = (path: string) => relative(SRC, path).split(sep).join('/')
const rules = sheets.flatMap((path) => rulesIn(readFileSync(path, 'utf8'), short(path)))

/** Every declared `position` value in one rule, `!important` stripped. */
const positions = (rule: Rule) =>
  rule.declarations
    .filter((d) => d.property === 'position')
    .map((d) => tokens(d.value)[0] ?? '')

/**
 * The rules this invariant is about: anything that scrolls, plus the frame,
 * which clips instead. `.desk` is `overflow: hidden` on purpose — the scrolling
 * belongs to the panes — and it is exactly as much a containing block for that:
 * an unpositioned frame does not clip a descendant it is not the containing
 * block of, which is how the skip link and the bubble selects escaped it.
 */
const containers = rules.filter((rule) => {
  const overflow = rule.declarations.filter((d) => OVERFLOW.has(d.property))
  if (overflow.some((d) => scrolls(d.value))) return true
  return rule.selector === '.desk' && overflow.length > 0
})

describe('every scroll container is a containing block', () => {
  it('found the sheets and the scrollers, so the sweep below is not vacuous', () => {
    // A walker that returns nothing passes every rule under it. These two
    // numbers are the ones that make the sweep mean something; they are floors
    // and not equalities, because adding a scroller is allowed and adding one
    // that does not position itself is what fails.
    expect(sheets.length).toBeGreaterThanOrEqual(34)
    expect(sheets.map(short)).toContain('shell.css')
    expect(sheets.map(short)).toContain('styles.css')
    expect(containers.length).toBeGreaterThanOrEqual(17)
  })

  it.each(containers.map((rule) => [`${rule.where}  ${rule.selector}`, rule] as const))(
    '%s positions itself',
    (name, rule) => {
      const declared = positions(rule)
      expect(
        declared.length,
        `${name} scrolls or clips its content but declares no position: an absolutely ` +
          'positioned descendant is then laid out against the initial containing block, ' +
          'is neither scrolled nor clipped by this rule, and extends the document'
      ).toBeGreaterThan(0)
      for (const value of declared) {
        expect(POSITIONED.has(value), `${name} declares position: ${value}`).toBe(true)
      }
    }
  )

  it('refuses an overflow value it cannot read', () => {
    // A sweep is only as good as its reading. `overflow: var(--x)` is a value
    // this file cannot resolve — the custom property may be `auto` on one
    // route and `hidden` on another — so a rule spelt that way would slip
    // through the filter above and be held by nothing. It is not a lint: it is
    // the one shape that makes the sweep quietly incomplete, so it fails here
    // and the author either writes the keyword or comes and changes this.
    const unreadable = rules.flatMap((rule) =>
      rule.declarations
        .filter((d) => OVERFLOW.has(d.property) && /var\(/i.test(d.value))
        .map((d) => `${rule.where}  ${rule.selector}  ${d.property}: ${d.value}`)
    )
    expect(
      unreadable,
      'an overflow written as a custom property cannot be read from the source, so this ' +
        'file cannot tell whether the rule scrolls: spell the keyword, or teach this test ' +
        'to resolve it'
    ).toEqual([])
  })

  it('holds the frame and its four panes by name as well', () => {
    // The sweep above reaches `.desk-console` only through the frame clause, and
    // it clips rather than scrolls — so the four panes and the frame are named
    // here too. This is the belt; the sweep is the braces, and it is the sweep
    // that catches the scroller nobody has written yet.
    for (const selector of [
      '.desk',
      '.desk-rail',
      '.desk-main',
      '.desk-inspector',
      '.desk-console'
    ]) {
      const rule = rules.find((r) => r.where === 'shell.css' && r.selector === selector)
      expect(rule, `${selector} is declared in shell.css`).toBeDefined()
      expect(positions(rule!), `${selector} is a containing block`).toEqual(['relative'])
    }
  })
})

describe('the rule reader itself', () => {
  it('keeps each rule’s own declarations apart from its neighbour’s', () => {
    // The defect this guards: a reader that pooled a sheet's declarations would
    // pass a sheet that scrolls in one rule and positions in another, which is
    // the exact shape of the bug.
    const parsed = rulesIn('.a { overflow: auto; }\n.b { position: relative; }')
    expect(parsed.map((r) => r.selector)).toEqual(['.a', '.b'])
    expect(parsed[0]!.declarations).toEqual([{ property: 'overflow', value: 'auto' }])
    expect(parsed[1]!.declarations).toEqual([{ property: 'position', value: 'relative' }])
  })

  it('reads a rule nested inside an at-rule and inside another rule', () => {
    const parsed = rulesIn(
      '@layer shell {\n  .a { overflow: auto;\n    &:hover { position: fixed; }\n    color: red }\n}'
    )
    const a = parsed.find((r) => r.selector === '.a')!
    expect(a.declarations).toEqual([
      { property: 'overflow', value: 'auto' },
      { property: 'color', value: 'red' }
    ])
    expect(parsed.find((r) => r.selector === '&:hover')!.declarations).toEqual([
      { property: 'position', value: 'fixed' }
    ])
  })

  it('does not read a declaration out of a comment or out of a value', () => {
    const parsed = rulesIn('.a { /* position: relative; */ grid-template: min(1fr, 2fr) / auto; }')
    expect(parsed[0]!.declarations).toEqual([
      { property: 'grid-template', value: 'min(1fr, 2fr) / auto' }
    ])
  })

  it('tells a scrolling value from a word that merely contains one', () => {
    expect(scrolls('auto')).toBe(true)
    expect(scrolls('hidden auto')).toBe(true)
    expect(scrolls('scroll')).toBe(true)
    expect(scrolls('hidden')).toBe(false)
    // `overflow-wrap: anywhere` is a different property, and `autofill` is not
    // `auto`: a substring test passes both.
    expect(scrolls('anywhere')).toBe(false)
    expect(scrolls('autofill')).toBe(false)
    // The legacy spelling of `auto`, and a scroll container wherever it is
    // still honoured.
    expect(scrolls('overlay')).toBe(true)
    // `!important` changes which rule wins, not what the value means.
    expect(scrolls('auto !important')).toBe(true)
    expect(scrolls('hidden !important')).toBe(false)
  })

  it('knows the logical spellings of overflow, and the one word that is not one', () => {
    // `overflow-block` and `overflow-inline` are the same property in a
    // writing-mode-relative dress; a sweep that knew only the physical pair
    // would miss a scroller spelt either way.
    expect(OVERFLOW.has('overflow-block')).toBe(true)
    expect(OVERFLOW.has('overflow-inline')).toBe(true)
    expect(OVERFLOW.has('overflow-wrap')).toBe(false)
  })

  it('reads a position through !important', () => {
    // The false positive this replaces: `position: relative !important`
    // positions exactly as `position: relative` does, and a reader that
    // compared the whole string called it unpositioned.
    const parsed = rulesIn('.a { overflow: auto; position: relative !important; }')
    expect(positions(parsed[0]!)).toEqual(['relative'])
  })
})
