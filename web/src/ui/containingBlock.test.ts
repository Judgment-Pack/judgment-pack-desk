/**
 * A scroll container is a containing block — read off every committed sheet.
 *
 * **What this file is.** A *source reader*. It parses the `.css` files under
 * `web/src` and states a claim about what they say. It does not compute the
 * cascade. Three drafts of it tried to, and each was wrong in a way that
 * reported green on a broken pane: selector text compared as a string, a
 * first-match lookup, a nested `&` that put the override back out of reach.
 * A guard that emulates the cascade nearly right is worse than one that does
 * not try, because it is believed.
 *
 * **The boundary, in one paragraph.** This file holds the *declaring rules*:
 * that each rule which authors a scrolling overflow also declares a position
 * that positions, and that the frame and the four panes do so under their own
 * exact selector. A
 * later rule that changes a held element's *computed* position — by an
 * ancestor selector, an id, an attribute, a nested `&`, a `:global`, an inline
 * `style`, a `position` written from script, from this sheet or any other — is
 * outside a source reader and is not claimed here. That half is measured in a
 * real browser by `scripts/containment-check.sh`, which loads a built chassis
 * in Chrome and reads the computed `position` of every pane and
 * `document.scrollingElement.scrollHeight` against `innerHeight` on every
 * route at every width the sheets author. CI supplies no runtime binary and no
 * project, so there is nothing for the chassis to serve; the gate is run by
 * hand. Run before every merge that touches a stylesheet. This is a
 * convention; nothing automated enforces it.
 *
 * **Why source at all, then.** vitest runs with `css: false`, so no stylesheet
 * is processed: a pane whose `position` was deleted renders here exactly like
 * one that still carries it, and jsdom lays nothing out, so a
 * `getBoundingClientRect` in this suite could not tell a document that scrolls
 * from one that does not. Reading the sheet is the only thing a unit test on
 * this project *can* do about it — so it does that, exactly, and says where it
 * stops.
 *
 * **The two things it holds.**
 *
 * 1. **Every rule that authors a scrolling overflow.** `overflow`,
 *    `overflow-x`, `overflow-y`, `overflow-block` or `overflow-inline` with
 *    `auto`, `scroll` or `overlay` among its tokens — plus the frame `.desk`,
 *    which clips on purpose. Each declares a `position` that positions in the
 *    same rule or, when the rule is nested (a prelude beginning `&`, or an
 *    at-rule prelude), on the nearest ancestor style rule that names the same
 *    element: `.x { position: relative; &.dense { overflow: auto } }` passes,
 *    and the same without the position fails naming `.x › &.dense`.
 * 2. **The frame and the four panes, by exact selector, in `shell.css`.**
 *    Every rule whose selector list contains `.desk`, `.desk-rail`,
 *    `.desk-main`, `.desk-inspector` or `.desk-console` *as that whole
 *    selector* is read: at least one declares a positioning value, and none
 *    declares a value that does not position. **This is a same-selector
 *    check** — `.desk-main { position: static }` written a second time is
 *    caught, and `body .desk-main { position: static }` is not, because it is
 *    a different selector and matching it would be emulating the cascade
 *    again.
 *
 * A `<textarea>` scrolls with no authored overflow and renders no element
 * children, so it can mislay nothing; no list is kept for it.
 *
 * **The defect all of this replaces.** `overflow` scrolls and clips only the
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
 * The three elements past the fold were the 1px `select[aria-hidden="true"]`
 * that Radix renders beside every Select trigger inside a `<form>` — the
 * assistant endpoint form's Wire protocol, Engine and Thinking pickers, at
 * y 1808, 2341 and 2438.
 *
 * **So this holds a shape and not five names.** Any rule in any of these
 * sheets that scrolls must also position itself, whoever adds it and whenever.
 * A list of the panes that were broken in September would be satisfied by the
 * sixth scroller somebody writes in October.
 *
 * **And one thing that is deliberately outside the sweep.** A rule that
 * *merely clips* is not held: `overflow: hidden` on an ellipsis label, a
 * segmented control, a popup, a `.json` block or a code frame clips text, and
 * text has no containing block to be laid out against. The frame is the
 * exception because a positioned descendant *is* what escaped it.
 */
import { readFileSync, readdirSync } from 'node:fs'
import { join, relative, sep } from 'node:path'
import { describe, expect, it } from 'vitest'

const SRC = join(import.meta.dirname, '..')

/**
 * One rule: the prelude that opened it, the declarations it owns, and the rule
 * it was opened inside.
 *
 * The parent link is what makes nested CSS readable. `@media (…) { … }` written
 * *inside* `.desk-main` is a rule whose prelude is not a selector at all, and
 * `&.dense { … }` is one whose prelude names the same element in another word;
 * in both cases the `position` that reaches the element may be written on the
 * rule above. Without the link a reader compares `@media (min-width: 600px)`
 * against a set of names, matches nothing, and reports correct code as broken —
 * which an earlier draft did.
 */
interface Rule {
  prelude: string
  declarations: { property: string; value: string }[]
  where: string
  parent?: Rule
}

/**
 * Every rule in a sheet, at any nesting depth, with the declarations it owns
 * and not those of the rules nested inside it.
 *
 * `declarations.ts` splits a sheet into `property: value` pairs and drops the
 * selectors, which is the right shape for "does this sheet spell a colour" and
 * the wrong one here: the claim is about a *pair on one element*, so a sheet
 * that declares `overflow: auto` in one rule and `position: relative` in
 * another rule must fail. The splitter is the same one, extended to keep the
 * bracket depth it is already tracking and to remember the enclosing rule.
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
      const rule: Rule = {
        prelude: buffer.trim().replace(/\s+/g, ' '),
        declarations: [],
        where,
        parent: open.at(-1)
      }
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

/** A prelude that opens a block but names no element. */
const isAtRule = (prelude: string) => prelude.startsWith('@')

/**
 * A nested prelude that names the *same* element as the rule above it.
 *
 * `&.dense`, `&:hover`, `&[data-x]` are the enclosing element with something
 * added, so a `position` written on the enclosing rule reaches them.
 * `& .child` and `& > .child` name a descendant and are not the same element,
 * so the walk stops there rather than crediting a child with its parent's
 * declaration.
 */
const isSameElement = (prelude: string) => /^&(?![\s>+~])/.test(prelude)

/**
 * The rule whose prelude actually names the element these declarations style.
 *
 * For an ordinary rule that is itself. For `@media`, `@supports`, `@layer` or
 * `@container` nested inside one, it is the enclosing selector rule: an
 * `overflow` written in such a block is an overflow *on that element*.
 */
function owner(rule: Rule): Rule | undefined {
  let node: Rule | undefined = rule
  while (node !== undefined && isAtRule(node.prelude)) node = node.parent
  return node
}

/** The selector that governs a rule — `''` for an at-rule with no selector above it. */
const effectiveSelector = (rule: Rule) => owner(rule)?.prelude ?? ''

/**
 * How a rule is written in a message: the chain of preludes it was opened
 * inside, outermost first, so a failure names the element and the condition
 * both — `.x › &.dense`, `.desk-main › @media (min-width: 600px)`.
 */
function describe_(rule: Rule): string {
  const chain: string[] = []
  let node: Rule | undefined = rule
  while (node !== undefined) {
    chain.unshift(node.prelude)
    node = node.parent
  }
  return chain.join(' › ')
}

/**
 * A declared value, lowercased, with `!important` taken off it.
 *
 * Two false results this replaces. `position: relative !important` positions
 * exactly as `position: relative` does, and a reader that compared the whole
 * string called it unpositioned — a false failure that teaches the next author
 * to delete the test rather than the flag. And `position:static!important`,
 * spelt with no space, hid a real override from a reader that only filtered a
 * separate `!important` token out of the split. The flag is stripped by
 * pattern, before tokenising, so its spelling cannot matter.
 */
function tokens(value: string): string[] {
  return value
    .toLowerCase()
    .replace(/!\s*important\s*$/, '')
    .split(/\s+/)
    .filter((token) => token !== '')
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

/** A `.css` file, whatever case the extension was typed in. */
const isStylesheet = (name: string) => name.toLowerCase().endsWith('.css')

/**
 * Every stylesheet under `web/src`, by extension and not by name.
 *
 * The first draft of this walked `*.module.css` and joined `shell.css` and
 * `styles.css` on by hand, which is a list of names wearing a sweep's clothes:
 * a third plain sheet — `packs/extra.css`, say — with `overflow: auto` in it
 * was invisible to the whole file and passed green. A sheet is a `.css` under
 * `src`; there is no other kind, and the extension is matched case-insensitively
 * because a file system that distinguishes `.CSS` from `.css` is not a reason
 * for a scroller to be unheld.
 */
function everySheet(directory: string): string[] {
  const found: string[] = []
  for (const entry of readdirSync(directory, { withFileTypes: true })) {
    const path = join(directory, entry.name)
    if (entry.isDirectory()) found.push(...everySheet(path))
    else if (isStylesheet(entry.name)) found.push(path)
  }
  return found
}

const short = (path: string) => relative(SRC, path).split(sep).join('/')
const sheetPaths = everySheet(SRC).sort()

/**
 * One rule's selector list, split on the commas that separate selectors.
 *
 * Not `split(',')`: `:is(.a, .b)` and `:not(.x, .y)` carry commas of their
 * own, and a splitter that broke on those would compare half a selector
 * against a held name and match nothing.
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

/** Every `position` value declared *on this rule*, `!important` stripped. */
const positions = (rule: Rule) =>
  rule.declarations.filter((d) => d.property === 'position').map((d) => tokens(d.value)[0] ?? '')

/**
 * Every `position` value that reaches this rule's element: its own, plus those
 * of the rules it is nested in while those name the same element.
 *
 * `.desk-main { position: relative; @media (min-width: 600px) { overflow: auto } }`
 * and `.x { position: relative; &.dense { overflow: auto } }` are both
 * correctly positioned scrollers, and a reader that looked only at the inner
 * block's own declarations would fail them.
 */
function positionsFor(rule: Rule): string[] {
  const found: string[] = []
  let node: Rule | undefined = rule
  while (node !== undefined) {
    found.push(...positions(node))
    if (!isAtRule(node.prelude) && !isSameElement(node.prelude)) break
    node = node.parent
  }
  return found
}

/**
 * The rules the sweep is about: anything that scrolls, plus the frame, which
 * clips instead. `.desk` is `overflow: hidden` on purpose — the scrolling
 * belongs to the panes — and it is exactly as much a containing block for
 * that: an unpositioned frame does not clip a descendant it is not the
 * containing block of, which is how the skip link and the bubble selects
 * escaped it.
 */
function isContainer(rule: Rule): boolean {
  const overflow = rule.declarations.filter((d) => OVERFLOW.has(d.property))
  if (overflow.some((d) => scrolls(d.value))) return true
  return effectiveSelector(rule) === '.desk' && overflow.length > 0
}

/** The frame and its four panes, held by their own exact selector in `shell.css`. */
const PANES = ['.desk', '.desk-rail', '.desk-main', '.desk-inspector', '.desk-console']

/** A container rule that declares no position at all, or one that does not position. */
function unpositioned(containers: Rule[]): string[] {
  return containers.flatMap((rule) => {
    const declared = positionsFor(rule)
    if (declared.length === 0) return [`${rule.where}  ${describe_(rule)}  declares no position`]
    return declared
      .filter((value) => !POSITIONED.has(value))
      .map((value) => `${rule.where}  ${describe_(rule)}  position: ${value}`)
  })
}

/** The whole reader, over a set of sheets given as text — the real ones or a fixture's. */
function read(sheets: { where: string; text: string }[]) {
  const rules = sheets.flatMap((sheet) => rulesIn(sheet.text, sheet.where))
  const containers = rules.filter(isContainer)
  return { rules, containers, unpositioned: unpositioned(containers) }
}

/**
 * The same-selector reading, for one exact selector in one sheet.
 *
 * `rules` is every rule of that sheet whose selector list contains the
 * selector *as one whole selector of the list* — `.desk-main` and
 * `.desk-main, .desk-rail` both, `body .desk-main` and `.desk-main:not(.x)`
 * neither. An at-rule block counts under the selector it is nested in, so a
 * `position` inside `@media` reaches this reading either way round it is
 * written. Nothing here is a cascade: two rules naming the same selector are
 * both read, and whichever of them would win is not this file's question —
 * only whether one of them positions and none of them un-positions.
 */
function sameSelector(rules: Rule[], selector: string, where: string) {
  const named = rules.filter(
    (rule) => rule.where === where && selectorList(effectiveSelector(rule)).includes(selector)
  )
  return {
    named,
    positions: named.some((rule) => positions(rule).some((value) => POSITIONED.has(value))),
    takesBack: named.flatMap((rule) =>
      positions(rule)
        .filter((value) => !POSITIONED.has(value))
        .map((value) => `${rule.where}  ${describe_(rule)}  position: ${value}`)
    )
  }
}

const project = read(
  sheetPaths.map((path) => ({ where: short(path), text: readFileSync(path, 'utf8') }))
)

describe('every scroll container is a containing block', () => {
  it('found the sheets and the scrollers, so the sweep is not vacuous', () => {
    // A walker that returns nothing passes every rule under it. These numbers
    // are the ones that make the sweep mean something; they are floors and not
    // equalities, because adding a scroller is allowed and adding one that does
    // not position itself is what fails.
    expect(sheetPaths.length).toBeGreaterThanOrEqual(34)
    expect(sheetPaths.map(short)).toContain('shell.css')
    expect(sheetPaths.map(short)).toContain('styles.css')
    expect(project.containers.length).toBeGreaterThanOrEqual(18)
    // Eighteen is seventeen authored scrollers and the frame, and the frame is
    // the one the floor alone could not notice going: it reaches this set
    // through a clause of its own, so a floor of seventeen stayed green with
    // that clause deleted and `.desk` — the element the whole measurement was
    // taken on — silently unheld.
    expect(
      project.containers.some((rule) => effectiveSelector(rule) === '.desk'),
      'the frame is in the swept set: it clips on purpose, and clips nothing it is not the ' +
        'containing block of'
    ).toBe(true)
  })

  it.each(project.containers.map((rule) => [`${rule.where}  ${describe_(rule)}`, rule] as const))(
    '%s positions itself',
    (name, rule) => {
      expect(
        unpositioned([rule]),
        `${name} is in the swept set — it authors a scrolling overflow, or it is the ` +
          'frame, which clips on purpose — but declares no position that positions, on itself ' +
          'or on the rule it is nested in: an absolutely positioned descendant is then laid ' +
          'out against the initial containing block, is neither scrolled nor clipped by this ' +
          'rule, and extends the document'
      ).toEqual([])
    }
  )

  it('refuses an overflow value it cannot read', () => {
    // A sweep is only as good as its reading. `overflow: var(--x)` is a value
    // this file cannot resolve — the custom property may be `auto` on one
    // route and `hidden` on another — so a rule spelt that way would slip
    // through the filter above and be held by nothing. It is not a lint: it is
    // the one shape that makes the sweep quietly incomplete, so it fails here
    // and the author either writes the keyword or comes and changes this.
    const unreadable = project.rules.flatMap((rule) =>
      rule.declarations
        .filter((d) => OVERFLOW.has(d.property) && /var\(/i.test(d.value))
        .map((d) => `${rule.where}  ${describe_(rule)}  ${d.property}: ${d.value}`)
    )
    expect(
      unreadable,
      'an overflow written as a custom property cannot be read from the source, so this ' +
        'file cannot tell whether the rule scrolls: spell the keyword, or teach this test ' +
        'to resolve it'
    ).toEqual([])
  })

  it('refuses a selector with an escape in it, rather than decoding one', () => {
    // The by-name reading compares selector *text*, so `.desk\-main` and
    // `.desk-main` would be two strings for one selector. No sheet here writes
    // an escape, and the cheap guarantee that none starts to is to fail on the
    // character rather than to grow an unescaper nobody would review.
    const escaped = project.rules
      .filter((rule) => rule.prelude.includes('\\'))
      .map((rule) => `${rule.where}  ${rule.prelude}`)
    expect(
      escaped,
      'a backslash in a selector is an escape this file does not decode, and the frame ' +
        'and the panes are matched by exact selector text: write ' +
        'the selector without an escape, or teach this test to decode one'
    ).toEqual([])
  })

  it.each(PANES)('%s is positioned, and no rule of that exact selector takes it back', (selector) => {
    // The sweep does not reach `.desk-console` at all: it clips rather than
    // scrolls, so no clause of the sweep holds it and it is held here, by
    // name. (`.desk` clips too, and is in the swept set only because a clause
    // names it.) Every rule spelling that exact selector is read, not the
    // first one found. This is a *same-selector* check and nothing wider: an
    // override written as `body .desk-main` is a different selector, is not
    // read here, and is the browser script's job.
    const held = sameSelector(project.rules, selector, 'shell.css')
    expect(held.named.length, `${selector} is a rule of shell.css`).toBeGreaterThan(0)
    expect(
      held.positions,
      `${selector} is a containing block: some rule spelling exactly that selector declares ` +
        'a position that positions'
    ).toBe(true)
    expect(
      held.takesBack,
      `a rule spelling exactly ${selector} declares a position that does not position, so ` +
        'the pane stops containing its absolutely positioned descendants and the document ' +
        'grows again'
    ).toEqual([])
  })
})

/**
 * The reader, against every construction the review rounds threw at it.
 *
 * These are fixtures and not sheets on disk, so the list is the record: each
 * one is a shape that either did defeat an earlier draft, or is correct code an
 * earlier draft wrongly failed, or is an override this reader is *not* claiming
 * to catch and which is green here on purpose. The next person to change it
 * reads the list instead of re-deriving it from four rounds of review.
 */
describe('the rule reader itself', () => {
  const SHELL = 'shell.css'
  /** The pane as `shell.css` actually writes it. */
  const PANE = '.desk-main {\n  overflow: auto;\n  position: relative;\n}\n'
  /** The findings of the sweep over one sheet's worth of text. */
  const missing = (text: string, where = SHELL) => read([{ where, text }]).unpositioned
  /** The same-selector reading over one sheet's worth of text. */
  const named = (text: string, selector = '.desk-main', where = SHELL) =>
    sameSelector(read([{ where, text }]).rules, selector, where)

  it('keeps each rule’s own declarations apart from its neighbour’s', () => {
    // The defect this guards: a reader that pooled a sheet's declarations would
    // pass a sheet that scrolls in one rule and positions in another, which is
    // the exact shape of the bug.
    const parsed = rulesIn('.a { overflow: auto; }\n.b { position: relative; }')
    expect(parsed.map((r) => r.prelude)).toEqual(['.a', '.b'])
    expect(parsed[0]!.declarations).toEqual([{ property: 'overflow', value: 'auto' }])
    expect(parsed[1]!.declarations).toEqual([{ property: 'position', value: 'relative' }])
  })

  it('reads a rule nested inside an at-rule and inside another rule', () => {
    const parsed = rulesIn(
      '@layer shell {\n  .a { overflow: auto;\n' +
        '    &:hover { position: fixed; }\n    color: red }\n}'
    )
    const a = parsed.find((r) => r.prelude === '.a')!
    expect(a.declarations).toEqual([
      { property: 'overflow', value: 'auto' },
      { property: 'color', value: 'red' }
    ])
    expect(parsed.find((r) => r.prelude === '&:hover')!.declarations).toEqual([
      { property: 'position', value: 'fixed' }
    ])
    // And the link back up, which is what makes the nesting cases below work.
    expect(a.parent?.prelude).toBe('@layer shell')
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
    // `!important` changes which rule wins, not what the value means — with a
    // space or without one.
    expect(scrolls('auto !important')).toBe(true)
    expect(scrolls('auto!important')).toBe(true)
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

  it('splits a selector list on its own commas and not on a functional one', () => {
    expect(selectorList('.desk-main, .desk')).toEqual(['.desk-main', '.desk'])
    expect(selectorList(':is(.a, .b) .desk, .desk-rail')).toEqual([
      ':is(.a, .b) .desk',
      '.desk-rail'
    ])
  })

  it('tells a nested selector that is the same element from one that is a child', () => {
    // What decides whether a parent's `position` may be credited to a nested
    // rule at all.
    expect(isSameElement('&.dense')).toBe(true)
    expect(isSameElement('&:hover')).toBe(true)
    expect(isSameElement("&[data-state='open']")).toBe(true)
    expect(isSameElement('& .child')).toBe(false)
    expect(isSameElement('& > .child')).toBe(false)
    expect(isSameElement('.desk-main')).toBe(false)
  })

  // ---- The sweep: what a nested scroller has to have ------------------------

  it('reports a nested scroller whose element positions nothing, naming both', () => {
    // The at-rule is not the selector: what has to be reported is the element
    // that scrolls, and a reader that printed only the `@media` prelude would
    // name a viewport range at an author looking for a pane.
    expect(missing('.desk-main { @media (min-width: 600px) { overflow: auto; } }')).toEqual([
      'shell.css  .desk-main › @media (min-width: 600px)  declares no position'
    ])
    expect(missing('.x { &.dense { overflow: auto; } }')).toEqual([
      'shell.css  .x › &.dense  declares no position'
    ])
  })

  it('is quiet about a nested scroller whose element does position itself', () => {
    expect(
      missing('.desk-main { position: relative; @media (min-width: 600px) { overflow: auto; } }')
    ).toEqual([])
    expect(missing('.x { position: relative; &.dense { overflow: auto; } }')).toEqual([])
  })

  it('does not credit a nested child with the position of the rule above it', () => {
    // `& .child` is a descendant, not the same element, so the parent's
    // `position: relative` says nothing about the child's containing block.
    expect(missing('.x { position: relative; & .child { overflow: auto; } }')).toEqual([
      'shell.css  .x › & .child  declares no position'
    ])
  })

  it('reports a scroller in a plain sheet nobody thought to name', () => {
    expect(missing('.thing { overflow: auto; }', 'packs/extra.css')).toEqual([
      'packs/extra.css  .thing  declares no position'
    ])
    // And the walker finds such a sheet whatever case the extension is in.
    expect(isStylesheet('extra.CSS')).toBe(true)
    expect(isStylesheet('extra.Css')).toBe(true)
    expect(isStylesheet('extra.csv')).toBe(false)
  })

  it('reads a value the same however its `!important` and its case are spelt', () => {
    // The flag changes which rule wins, not what the value means; and a
    // property compared before lowercasing hid a real override once.
    expect(missing('.a { overflow: auto; position: relative !important; }')).toEqual([])
    expect(missing('.a { overflow: auto; position:relative!important; }')).toEqual([])
    expect(missing('.a { overflow: auto; position:static!important; }')).toEqual([
      'shell.css  .a  position: static'
    ])
    expect(missing('.a { OVERFLOW: AUTO; POSITION: STATIC; }')).toEqual([
      'shell.css  .a  position: static'
    ])
  })

  // ---- The same-selector reading, and exactly how far it reaches ------------

  it('reads every rule of the exact selector, not the first', () => {
    // The defect: a lookup that stopped at the first match let a later
    // `position: static` under the same selector through. Both rules are read.
    const twice = named(PANE + '.desk-main { position: static; }')
    expect(twice.named).toHaveLength(2)
    expect(twice.positions).toBe(true)
    expect(twice.takesBack).toEqual(['shell.css  .desk-main  position: static'])
  })

  it('reads the exact selector inside a list, and inside an at-rule either way round', () => {
    expect(named(PANE + '.desk-main, .desk-rail { position: static; }').takesBack).toEqual([
      'shell.css  .desk-main, .desk-rail  position: static'
    ])
    expect(
      named(PANE + '@media (max-width: 900px) { .desk-main { position: static } }').takesBack
    ).toEqual(['shell.css  @media (max-width: 900px) › .desk-main  position: static'])
    expect(
      named(PANE + '.desk-main { @media (max-width: 900px) { position: static } }').takesBack
    ).toEqual(['shell.css  .desk-main › @media (max-width: 900px)  position: static'])
  })

  it('needs some rule of the exact selector to position it', () => {
    expect(named('.desk-main { overflow: auto; }').positions).toBe(false)
    expect(named('.desk-main { overflow: auto; } .desk-main { position: sticky; }').positions).toBe(
      true
    )
  })

  it('does not let one sheet’s rule answer for another sheet’s selector', () => {
    // Module classes are hashed, so `.list` in two modules are two classes;
    // more generally the reading is per sheet, and a rule of `styles.css` is
    // not a rule of `shell.css`.
    const sheets = read([
      { where: 'shell.css', text: PANE },
      { where: 'styles.css', text: '.desk-main { position: static; }' }
    ])
    expect(sameSelector(sheets.rules, '.desk-main', 'shell.css').takesBack).toEqual([])
    expect(sameSelector(sheets.rules, '.desk-main', 'styles.css').takesBack).toEqual([
      'styles.css  .desk-main  position: static'
    ])
  })

  // ---- Outside the reader: green here, and the browser script's job --------

  it.each([
    ['an ancestor in front of it', 'body .desk-main { position: static; }'],
    ['a functional pseudo-class after it', '.desk-main:not(.x) { position: static; }'],
    ['a nested `&` inside the rule itself', '.desk-main { & { position: static; } }'],
    ['an id the pane also answers to', '#main { position: static; }'],
    ['an attribute selector', "main[id='main'] { position: static; }"]
  ])('is quiet about %s, which is not this selector', (_name, override) => {
    // Not a gap discovered later: the boundary of a same-selector reading,
    // chosen over emulating the cascade because three drafts that emulated it
    // were wrong. `.desk-main` is `<main id="main">`, so every one of these
    // does defeat the pane in a browser and none is reported here.
    // `scripts/containment-check.sh` measures the computed `position` of each
    // pane in Chrome, and fails on exactly these.
    const held = named(PANE + override)
    expect(held.positions).toBe(true)
    expect(held.takesBack).toEqual([])
  })

  it('is quiet about a `:global` override in a module, for the same reason', () => {
    // A module handing a global class back to the cascade. It is a different
    // sheet and a different selector, so it is outside twice over.
    const sheets = read([
      { where: 'shell.css', text: PANE },
      { where: 'ui/Thing.module.css', text: ':global(.desk-main) { position: static; }' }
    ])
    expect(sameSelector(sheets.rules, '.desk-main', 'shell.css').takesBack).toEqual([])
  })

  it('is quiet about rules that change something else on the pane’s own tree', () => {
    for (const addition of [
      '.desk-main button { all: unset; }',
      '.desk-main::after { position: absolute; }',
      '.desk-main { padding: 0; }'
    ])
      expect(named(PANE + addition).takesBack, addition).toEqual([])
  })
})
