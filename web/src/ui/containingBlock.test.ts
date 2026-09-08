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
 * *document's* scrollable overflow. On `/admin` that measured
 * `document.scrollingElement.scrollHeight` 2439 against an `innerHeight` of
 * 800: the browser painted its own scrollbar and the whole 100dvh shell could
 * be scrolled up out of the window. (The configuration those figures were
 * taken in — 1400x800, an assistant key stored, without which the endpoint
 * form renders no pickers and there is nothing to measure — is named once, in
 * the comment above `.desk` in `shell.css`; every number here is that one.)
 * The three elements past the fold were the
 * 1px `select[aria-hidden="true"]` that Radix renders beside every Select
 * trigger inside a `<form>` — the assistant endpoint form's Wire protocol,
 * Engine and Thinking pickers, at y 1808, 2341 and 2438.
 *
 * **So this holds the pair and not the five names.** Any rule in any of these
 * sheets that scrolls must also position itself, whoever adds it and whenever.
 * A list of the panes that were broken in September would be satisfied by the
 * sixth scroller somebody writes in October.
 *
 * **And exactly this much.** The swept set is every rule that *authors* a
 * scrolling overflow — `overflow`, `overflow-x`, `overflow-y`,
 * `overflow-block` or `overflow-inline` with `auto`, `scroll` or `overlay`
 * among its tokens — plus the frame, which clips deliberately and has to
 * contain what it clips. Two kinds of thing are outside it and named here
 * rather than left to be discovered:
 *
 * - **A rule that merely clips is not held.** `overflow: hidden` on an
 *   ellipsis label, a segmented control, a popup, a `.json` block or a code
 *   frame clips text, not positioned boxes, and those rules ship without a
 *   `position` on purpose. The frame is the exception because a positioned
 *   descendant *is* what escaped it.
 * - **A scroll container the user agent makes is outside it.** A `textarea`
 *   computes `overflow: auto` with no sheet saying so, and a `select`'s
 *   listbox is drawn by the platform; a test that reads sources can see
 *   neither. `ui/TextArea.module.css` carries `position: relative` by hand for
 *   that reason, with the reason written above it.
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

/**
 * One rule's selector list, split on the commas that separate selectors.
 *
 * Not `split(',')`: `:is(.a, .b)` and `:not(.x, .y)` carry commas of their
 * own, and a splitter that broke on those would compare half a selector
 * against the container set and match nothing.
 */
function selectorList(selector: string): string[] {
  const out: string[] = []
  let buffer = ''
  let quote: string | undefined
  let parens = 0
  for (const character of selector) {
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
    if (parens === 0 && character === ',') {
      out.push(buffer.trim())
      buffer = ''
      continue
    }
    buffer += character
  }
  out.push(buffer.trim())
  return out.filter((one) => one !== '')
}

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

/** The selectors the sweep above holds, one per member of each rule's list. */
const containerSelectors = new Set(containers.flatMap((rule) => selectorList(rule.selector)))

/**
 * Every rule that takes one of those selectors' position away again.
 *
 * **Why a second pass at all.** The sweep above is *per rule*, and the cascade
 * is not. `.desk-main { overflow: auto; position: relative }` satisfies it and
 * a single later line — `.desk-main { position: static }` at the foot of the
 * sheet, or `@media (max-width: 900px) { .desk-main { position: static } }`
 * three hundred lines down — undoes the whole change while every assertion
 * stays green. Both were measured: 24 of 24 passing, and the pane no longer a
 * containing block.
 *
 * A `position` on a container's selector must therefore still position.
 * `static` is the one that was measured; `initial`, `unset`, `revert` and
 * `revert-layer` all compute to it, and `inherit` computes to whatever the
 * parent has, which is not a promise — so this reads the whitelist and not a
 * blacklist: a value that is not one of the four positioning keywords fails.
 *
 * It is deliberately stricter than the cascade. A `.list` in one module and a
 * `.list` in another are different classes once the module hash is on them and
 * neither can reach the other, but this compares selector text across every
 * sheet: the message names the sheet, so a real collision costs one rename and
 * a missed override costs the bug this branch exists to fix.
 */
function unpositionedBy(all: Rule[], selectors: Set<string>) {
  return all.flatMap((rule) =>
    selectorList(rule.selector)
      .filter((one) => selectors.has(one))
      .flatMap((one) =>
        positions(rule)
          .filter((value) => !POSITIONED.has(value))
          .map((value) => `${rule.where}  ${rule.selector}  ${one} → position: ${value}`)
      )
  )
}

describe('every scroll container is a containing block', () => {
  it('found the sheets and the scrollers, so the sweep below is not vacuous', () => {
    // A walker that returns nothing passes every rule under it. These two
    // numbers are the ones that make the sweep mean something; they are floors
    // and not equalities, because adding a scroller is allowed and adding one
    // that does not position itself is what fails.
    expect(sheets.length).toBeGreaterThanOrEqual(34)
    expect(sheets.map(short)).toContain('shell.css')
    expect(sheets.map(short)).toContain('styles.css')
    expect(containers.length).toBeGreaterThanOrEqual(18)
    // Eighteen is seventeen authored scrollers and the frame, and the frame is
    // the one the floor alone could not notice going: it reaches this set
    // through a clause of its own, so a floor of seventeen stayed green with
    // that clause deleted and `.desk` — the element the whole measurement was
    // taken on — silently unheld.
    expect(
      containers.some((rule) => rule.selector === '.desk'),
      'the frame is in the swept set: it clips on purpose, and clips nothing it is not the ' +
        'containing block of'
    ).toBe(true)
  })

  it.each(containers.map((rule) => [`${rule.where}  ${rule.selector}`, rule] as const))(
    '%s positions itself',
    (name, rule) => {
      const declared = positions(rule)
      expect(
        declared.length,
        `${name} is in the swept set — it authors a scrolling overflow, or it is the ` +
          'frame, which clips on purpose — but declares no position: an absolutely ' +
          'positioned descendant is then laid out against the initial containing block, ' +
          'is neither scrolled nor clipped by this rule, and extends the document'
      ).toBeGreaterThan(0)
      for (const value of declared) {
        expect(POSITIONED.has(value), `${name} declares position: ${value}`).toBe(true)
      }
    }
  )

  it('and no other rule anywhere unpositions one of them again', () => {
    expect(
      unpositionedBy(rules, containerSelectors),
      'a scroll container is a containing block only while nothing takes its position back: ' +
        'each line above is a rule whose selector is held by the sweep and which declares a ' +
        'position that does not position, so the pane stops containing its absolutely ' +
        'positioned descendants and the document grows again — at the width the media query ' +
        'names, if it is inside one'
    ).toEqual([])
  })

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

  it('walks into a media block and a supports block to find the rule inside', () => {
    // The cascade pass is worth nothing if the reader stops at an at-rule:
    // `@media (max-width: 900px) { .desk { position: static } }` is a pane
    // that contains nothing on a narrow viewport and a green suite on every
    // width, which is the worst shape a guard can have.
    const nested = rulesIn(
      '@media (max-width: 900px) {\n' +
        '  @supports (height: 100dvh) {\n' +
        '    .desk-main, .desk { position: static }\n' +
        '  }\n' +
        '}',
      'fixture.css'
    )
    expect(nested.find((r) => r.selector === '.desk-main, .desk')?.declarations).toEqual([
      { property: 'position', value: 'static' }
    ])
    expect(unpositionedBy(nested, new Set(['.desk']))).toEqual([
      'fixture.css  .desk-main, .desk  .desk → position: static'
    ])
    // And the four spellings of the same thing, plus a positioning one that
    // must not be reported.
    for (const value of ['static', 'initial', 'unset', 'revert', 'revert-layer']) {
      expect(
        unpositionedBy(rulesIn(`.desk { position: ${value} }`), new Set(['.desk'])).length,
        value
      ).toBe(1)
    }
    expect(unpositionedBy(rulesIn('.desk { position: sticky }'), new Set(['.desk']))).toEqual([])
  })

  it('splits a selector list on its own commas and not on a functional one', () => {
    expect(selectorList('.desk-main, .desk')).toEqual(['.desk-main', '.desk'])
    expect(selectorList(':is(.a, .b) .desk, .desk-rail')).toEqual([
      ':is(.a, .b) .desk',
      '.desk-rail'
    ])
  })

  it('reads a position through !important', () => {
    // The false positive this replaces: `position: relative !important`
    // positions exactly as `position: relative` does, and a reader that
    // compared the whole string called it unpositioned.
    const parsed = rulesIn('.a { overflow: auto; position: relative !important; }')
    expect(positions(parsed[0]!)).toEqual(['relative'])
  })
})
