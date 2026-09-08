/**
 * A scroll container is a containing block — read off every committed sheet.
 *
 * **What this file is, and what it is not.** It is a *source reader*. It parses
 * the `.css` files under `web/src` and states a claim about what they say. It
 * does not compute the cascade, and two earlier drafts of it were wrong
 * precisely because they tried to: an emulated cascade that is nearly right is
 * a guard that reports green on a broken pane. So the division of labour is
 * written down here and kept to:
 *
 * - **The test reads source and says so.** It holds two things exactly, and
 *   names its own edge below.
 * - **The browser measures the cascade, and the drive says so.** The live
 *   drive in the PR body loads a real build in real Chrome and reads
 *   `document.scrollingElement.scrollHeight` against `innerHeight` on 49
 *   configurations — every route, both Inspector states, the console open, two
 *   widths. That is the measurement of the computed cascade. It is not run in
 *   CI, and this file cannot stand in for it.
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
 * 1. **The declaring rule.** Every rule that authors a scrolling overflow —
 *    `overflow`, `overflow-x`, `overflow-y`, `overflow-block` or
 *    `overflow-inline` with `auto`, `scroll` or `overlay` among its tokens —
 *    declares a `position` that positions in the same rule. So do the frame
 *    `.desk` and its four panes, by name, and the one user-agent scroller the
 *    sweep is structurally blind to (below). That is `relative` on every one of
 *    them but two — `Dialog .content` and `.desk-drawer` — which are `fixed`,
 *    and out of flow already.
 * 2. **Every rule that takes that position back.** In the same sheet family,
 *    any rule whose effective selector *names a held class as a whole class
 *    token* — `body .desk-main`, `main.desk-main`, `.desk > .desk-main`,
 *    `:where(.desk) .desk-main`, `.desk[data-console='open'] .desk-main` — and
 *    which declares `position` with a value that is not one of the four
 *    positioning keywords, or `all` with any value at all, fails and is named.
 *    Later in the file or not: specificity does not care about source order,
 *    so neither does this.
 *
 * **What it cannot do, stated so nobody has to rediscover it.** An override
 * that reaches a held element *without naming its class* is outside this
 * reader: `#main { position: static }`, `main[id='main'] { position: static }`,
 * an inline `style` attribute, a `position` set from script. Those are green
 * here and would be caught, if ever written, by the drive. The reader is a
 * class-token matcher, not a selector engine, and this is the boundary of that
 * choice.
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
 * **And exactly this much.** Two kinds of thing are outside the sweep and
 * named here rather than left to be discovered:
 *
 * - **A rule that merely clips is not held.** `overflow: hidden` on an
 *   ellipsis label, a segmented control, a popup, a `.json` block or a code
 *   frame clips text, not positioned boxes, and those rules ship without a
 *   `position` on purpose. The frame is the exception because a positioned
 *   descendant *is* what escaped it.
 * - **A scroll container the user agent makes is outside it.** A `textarea`
 *   computes `overflow: auto` with no sheet saying so, and a `select`'s
 *   listbox is drawn by the platform; a reader of sources can see neither. The
 *   one the desk renders is listed by hand in `UA_SCROLLERS` below and held by
 *   the same reader — a list, because no sweep over authored declarations can
 *   ever find it.
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
 * *inside* `.desk-main` is a rule whose prelude is not a selector at all; the
 * element it styles is named by the nearest enclosing rule that is one. Without
 * the link, a reader compares `@media (min-width: 600px)` against a set of
 * class names, matches nothing, and reports a correct sheet as broken — which
 * is what the previous draft did.
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
 * The rule whose prelude actually names the element these declarations style.
 *
 * For an ordinary rule that is itself. For `@media`, `@supports`, `@layer` or
 * `@container` nested inside one, it is the enclosing selector rule: an
 * `overflow` written in such a block is an overflow *on that element*, and a
 * `position` written on the element counts for it.
 */
function owner(rule: Rule): Rule | undefined {
  let node: Rule | undefined = rule
  while (node !== undefined && isAtRule(node.prelude)) node = node.parent
  return node
}

/** The selector that governs a rule — `''` for an at-rule with no selector above it. */
const effectiveSelector = (rule: Rule) => owner(rule)?.prelude ?? ''

/** How a rule is written in a message: its selector, and the at-rule it sits in. */
const describe_ = (rule: Rule) =>
  isAtRule(rule.prelude)
    ? `${effectiveSelector(rule) || '(no selector)'} › ${rule.prelude}`
    : rule.prelude

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
 * against the held set and match nothing.
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

/**
 * Every class named anywhere in a selector, as whole class tokens.
 *
 * This is the unit of matching, and choosing it is the whole repair. Comparing
 * *selector text* — the previous draft — made `.desk-main` and
 * `body .desk-main` two unrelated strings, so an override with one extra
 * ancestor in front of it was invisible while measuring green in Chrome.
 * A class token is what both selectors have in common and what the cascade
 * actually keys on. `.desk > .desk-main` names two; `.desk-main-x` names one,
 * and it is not `.desk-main`, because the match is on the whole token and not
 * on a prefix. Quoted strings are blanked first so an attribute value like
 * `[data-file='a.css']` contributes no class.
 */
function classesIn(selector: string): string[] {
  const bare = selector.replace(/"[^"]*"|'[^']*'/g, "''")
  return [...bare.matchAll(/\.(-?[_a-zA-Z][\w-]*)/g)].map((match) => match[1]!)
}

/** Every class a rule's effective selector list names. */
const classesNamedBy = (rule: Rule) => [
  ...new Set(selectorList(effectiveSelector(rule)).flatMap(classesIn))
]

/**
 * Which sheets can reach each other's classes.
 *
 * Module classes are hashed at build time, so `.list` in `PacksPane.module.css`
 * and `.list` in `Tabs.module.css` are different classes that cannot override
 * one another; a reader that compared bare tokens across every sheet would call
 * the second an override of the first and fail a correct tree. A class authored
 * in a global sheet can be taken back only from a global sheet, and one
 * authored in a module only from that same module. (A module class handed to a
 * global sheet through `:global` would escape this; the project uses none, and
 * a search for `:global` is the check if that changes.)
 */
const familyOf = (where: string) =>
  where.toLowerCase().endsWith('.module.css') ? where : 'global'

/** Every `position` value declared *on this rule*, `!important` stripped. */
const positions = (rule: Rule) =>
  rule.declarations.filter((d) => d.property === 'position').map((d) => tokens(d.value)[0] ?? '')

/**
 * Every `position` value that reaches this rule's element: its own, plus those
 * of the selector rule it is nested in, if it is an at-rule block.
 *
 * `.desk-main { position: relative; @media (min-width: 600px) { overflow: auto } }`
 * is a correctly positioned scroller, and a reader that looked only at the
 * `@media` block's own declarations would fail it.
 */
function positionsFor(rule: Rule): string[] {
  const found: string[] = []
  let node: Rule | undefined = rule
  while (node !== undefined) {
    found.push(...positions(node))
    if (!isAtRule(node.prelude)) break
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

/** The frame and its four panes, held by name as well as by shape. */
const PANES = ['.desk', '.desk-rail', '.desk-main', '.desk-inspector', '.desk-console']

/**
 * Scroll containers the user agent makes, which no sweep over authored
 * declarations can find — so they are a list, and the list says why.
 *
 * A `<textarea>` computes `overflow: auto` with nothing in any sheet saying
 * so. The desk renders twenty of them on the editor route, and each one is a
 * scroll container that would lay an absolutely positioned descendant out
 * against the initial containing block exactly as the panes did. Its sibling
 * primitive `CodeArea .area` is caught by the sweep only because that rule
 * happens to spell `overflow: auto` out. Same primitive, same job, so the
 * declaration is written by hand and asserted here by the same reader that
 * holds the sweep's rules — including the second pass, so a later
 * `.textarea { position: static }` in that module fails too.
 */
const UA_SCROLLERS = [
  { where: 'ui/TextArea.module.css', selector: '.textarea' }
] as const

/**
 * Every class this reader holds, per sheet family: the sweep's containers, the
 * five panes by name, and the user-agent scrollers.
 */
function heldClasses(containers: Rule[]): Map<string, Set<string>> {
  const held = new Map<string, Set<string>>()
  const add = (where: string, token: string) => {
    const key = familyOf(where)
    const set = held.get(key) ?? new Set<string>()
    set.add(token)
    held.set(key, set)
  }
  for (const rule of containers)
    for (const token of classesNamedBy(rule)) add(rule.where, token)
  for (const selector of PANES) for (const token of classesIn(selector)) add('shell.css', token)
  for (const one of UA_SCROLLERS) for (const token of classesIn(one.selector)) add(one.where, token)
  return held
}

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

/**
 * Every rule that takes a held class's position back again.
 *
 * **Why a second pass at all.** The sweep is *per rule*, and the cascade is
 * not. `.desk-main { overflow: auto; position: relative }` satisfies it and a
 * single later line undoes the whole change while every assertion stays green.
 * Two such lines were measured against the first draft of this pass — `body
 * .desk-main { position: static }` and `.desk-console { position: static }` —
 * and both left the file green with the pane no longer a containing block,
 * because that draft compared selector *text* and looked at only the first
 * matching rule. This one matches class tokens and reads every rule.
 *
 * **What counts as taking it back.** A `position` whose value is not one of
 * the four positioning keywords: `static` is the one that was measured;
 * `initial`, `unset`, `revert` and `revert-layer` all compute to it, and
 * `inherit` computes to whatever the parent has, which is not a promise. So
 * this reads a whitelist and not a blacklist. And `all` with *any* value, because
 * `all: revert` resets `position` while containing the word `position`
 * nowhere — the shorthand that made a previous draft report green on a pane
 * with no `position` declaration left standing at all.
 */
function takenBack(all: Rule[], held: Map<string, Set<string>>): string[] {
  const out: string[] = []
  for (const rule of all) {
    const family = held.get(familyOf(rule.where))
    if (family === undefined) continue
    const named = classesNamedBy(rule).filter((token) => family.has(token))
    if (named.length === 0) continue
    for (const declaration of rule.declarations) {
      const value = tokens(declaration.value)[0] ?? ''
      const takes =
        declaration.property === 'all' ||
        (declaration.property === 'position' && !POSITIONED.has(value))
      if (!takes) continue
      const { property, value: written } = declaration
      for (const token of named)
        out.push(`${rule.where}  ${describe_(rule)}  .${token} → ${property}: ${written}`)
    }
  }
  return out
}

/** The whole reader, over a set of sheets given as text — the real ones or a fixture's. */
function read(sheets: { where: string; text: string }[]) {
  const rules = sheets.flatMap((sheet) => rulesIn(sheet.text, sheet.where))
  const containers = rules.filter(isContainer)
  const held = heldClasses(containers)
  return {
    rules,
    containers,
    held,
    unpositioned: unpositioned(containers),
    takenBack: takenBack(rules, held)
  }
}

const project = read(
  sheetPaths.map((path) => ({ where: short(path), text: readFileSync(path, 'utf8') }))
)

/** The rules of one sheet family that name a class — every one, not the first. */
const rulesNaming = (token: string, family: string) =>
  project.rules.filter(
    (rule) => familyOf(rule.where) === family && classesNamedBy(rule).includes(token)
  )

describe('every scroll container is a containing block', () => {
  it('found the sheets, the scrollers and the held classes, so the sweep is not vacuous', () => {
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
    // And the second pass is only as wide as the set it holds.
    const global = project.held.get('global') ?? new Set<string>()
    for (const selector of PANES)
      expect(global.has(classesIn(selector)[0]!), `${selector} is held`).toBe(true)
    expect(project.held.get('ui/TextArea.module.css')?.has('textarea')).toBe(true)
  })

  it.each(project.containers.map((rule) => [`${rule.where}  ${describe_(rule)}`, rule] as const))(
    '%s positions itself',
    (name, rule) => {
      expect(
        unpositioned([rule]),
        `${name} is in the swept set — it authors a scrolling overflow, or it is the ` +
          'frame, which clips on purpose — but declares no position that positions: an ' +
          'absolutely positioned descendant is then laid out against the initial containing ' +
          'block, is neither scrolled nor clipped by this rule, and extends the document'
      ).toEqual([])
    }
  )

  it('and no rule that names one of those classes takes its position back', () => {
    expect(
      project.takenBack,
      'a scroll container is a containing block only while nothing takes its position back: ' +
        'each line above is a rule whose selector names a held class and which declares a ' +
        'position that does not position, or an `all` that resets one, so the pane stops ' +
        'containing its absolutely positioned descendants and the document grows again — at ' +
        'the width the media query names, if it is inside one'
    ).toEqual([])
  })

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

  it.each(PANES)('%s is positioned by some rule of the shell sheet', (selector) => {
    // The sweep reaches `.desk-console` only through the frame clause, and it
    // clips rather than scrolls — so the four panes and the frame are named
    // here too. Every rule that names the class is read, not the first one
    // found: the position may be declared on `.desk-main` and the class may be
    // named again three hundred lines down, and it was reading only the first
    // that let `.desk-console { position: static }` pass.
    const token = classesIn(selector)[0]!
    const named = rulesNaming(token, 'global')
    expect(named.length, `${selector} is declared in a global sheet`).toBeGreaterThan(0)
    expect(
      named.some((rule) => positionsFor(rule).some((value) => POSITIONED.has(value))),
      `${selector} is a containing block: some rule naming it declares a position that positions`
    ).toBe(true)
    // Nothing takes it back: that is the assertion above, over the whole tree.
  })

  it.each(UA_SCROLLERS.map((one) => [`${one.where} ${one.selector}`, one] as const))(
    '%s is positioned by hand, because no sweep can see it',
    (_name, one) => {
      const token = classesIn(one.selector)[0]!
      const named = rulesNaming(token, familyOf(one.where))
      expect(named.length, `${one.selector} is declared in ${one.where}`).toBeGreaterThan(0)
      expect(
        named.some((rule) => positionsFor(rule).some((value) => POSITIONED.has(value))),
        `${one.selector} is a user-agent scroll container — a textarea computes ` +
          '`overflow: auto` with no sheet saying so — so the sweep is structurally blind to ' +
          'it and the declaration is held by this list instead'
      ).toBe(true)
    }
  )
})

/**
 * The reader, against every construction the review rounds threw at it.
 *
 * These are fixtures and not sheets on disk, so the list is the record: each
 * one is a shape that either did defeat an earlier draft or is the correct
 * code an earlier draft wrongly failed. The next person to change the matcher
 * reads the list instead of re-deriving it from two rounds of review.
 */
describe('the rule reader itself', () => {
  const SHELL = 'shell.css'
  /** The pane as `shell.css` actually writes it. */
  const PANE = '.desk-main {\n  overflow: auto;\n  position: relative;\n}\n'
  /** The findings of the second pass over one sheet's worth of text. */
  const overrides = (text: string, where = SHELL) => read([{ where, text }]).takenBack
  /** The findings of the sweep over one sheet's worth of text. */
  const missing = (text: string, where = SHELL) => read([{ where, text }]).unpositioned

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
    // And the link back up, which is what makes the at-rule cases below work.
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

  it('takes a class token whole, out of any shape of selector', () => {
    expect(classesIn('body .desk-main')).toEqual(['desk-main'])
    expect(classesIn('main.desk-main')).toEqual(['desk-main'])
    expect(classesIn('.desk > .desk-main')).toEqual(['desk', 'desk-main'])
    expect(classesIn(':where(.desk) .desk-main')).toEqual(['desk', 'desk-main'])
    expect(classesIn(".desk[data-console='open'] .desk-main")).toEqual(['desk', 'desk-main'])
    // A prefix is not a token, and an id or an attribute names no class at all.
    expect(classesIn('.desk-main-x')).toEqual(['desk-main-x'])
    expect(classesIn('#main')).toEqual([])
    expect(classesIn("main[id='main']")).toEqual([])
  })

  it('splits a selector list on its own commas and not on a functional one', () => {
    expect(selectorList('.desk-main, .desk')).toEqual(['.desk-main', '.desk'])
    expect(selectorList(':is(.a, .b) .desk, .desk-rail')).toEqual([
      ':is(.a, .b) .desk',
      '.desk-rail'
    ])
  })

  // ---- The constructions that must FAIL ------------------------------------

  it.each([
    ['a descendant selector', 'body .desk-main { position: static; }'],
    ['a type-qualified selector', 'main.desk-main { position: static; }'],
    ['a child combinator', '.desk > .desk-main { position: static; }'],
    ['a :where() ancestor', ':where(.desk) .desk-main { position: static; }'],
    [
      'an attribute-qualified ancestor',
      ".desk[data-console='open'] .desk-main { position: static; }"
    ],
    ['the shorthand that names no property', '.desk-main { all: revert; }'],
    ['a value that is a promise about a parent', '.desk-main { position: inherit; }'],
    ['`!important` with no space before it', '.desk-main { position:static!important; }'],
    ['a shouted property and value', '.desk-main { POSITION: STATIC; }'],
    [
      'a media block three hundred lines down',
      '@media (max-width: 900px) { .desk-main { position: static } }'
    ],
    [
      'an at-rule nested inside the rule itself',
      '.desk-main { @media (max-width: 900px) { position: static } }'
    ]
  ])('reports %s that takes the pane’s position back', (_name, override) => {
    // Every one of these leaves `.desk-main`'s own rule exactly as shipped and
    // still stops the pane containing anything. The first five were invisible
    // to a reader that compared selector text; `all: revert` to one that looked
    // for the word `position`; the `!important` and shouted spellings to one
    // that tokenised before stripping and compared before lowercasing.
    expect(overrides(PANE + override).length, override).toBeGreaterThan(0)
  })

  it('reports the two the round-2 measurement caught, with the sheet and the class named', () => {
    // These are the two that measured green in Chrome against the previous
    // draft — the finding that sent this round back. The message shape is
    // asserted, not just the count, because a finding nobody can act on is
    // most of the way to no finding.
    expect(overrides(PANE + 'body .desk-main { position: static; }')).toEqual([
      'shell.css  body .desk-main  .desk-main → position: static'
    ])
    expect(overrides(PANE + '.desk-console { position: static; }')).toEqual([
      'shell.css  .desk-console  .desk-console → position: static'
    ])
  })

  it('reports a nested scroller whose element positions nothing, naming the element', () => {
    // The at-rule is not the selector: what has to be reported is `.desk-main`,
    // the element that scrolls, and a reader that printed the `@media` prelude
    // would name a viewport range at an author looking for a pane.
    expect(missing('.desk-main { @media (min-width: 600px) { overflow: auto; } }')).toEqual([
      'shell.css  .desk-main › @media (min-width: 600px)  declares no position'
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

  it('reports the textarea losing the position the sweep cannot ask it for', () => {
    const module = 'ui/TextArea.module.css'
    expect(
      overrides('.textarea { position: relative; }\n.textarea { position: static; }', module)
    ).toEqual(['ui/TextArea.module.css  .textarea  .textarea → position: static'])
  })

  it('reports the four spellings of static, and the frame unpositioned at one width', () => {
    const frame = '.desk { overflow: hidden; position: relative }\n'
    for (const value of ['static', 'initial', 'unset', 'revert', 'revert-layer', 'inherit'])
      expect(overrides(`${frame}.desk { position: ${value} }`), value).toHaveLength(1)
    expect(
      overrides(
        frame +
          '@media (max-width: 900px) {\n' +
          '  @supports (height: 100dvh) { .desk-main, .desk { position: static } }\n' +
          '}'
      )
    ).toEqual([
      'shell.css  .desk-main, .desk  .desk-main → position: static',
      'shell.css  .desk-main, .desk  .desk → position: static'
    ])
  })

  // ---- The constructions that must PASS ------------------------------------

  it.each([
    ['`!important` on a value that positions', '.desk-main { position: relative !important; }'],
    [
      '`!important` with no space, on one that positions',
      '.desk-main { position:relative!important; }'
    ],
    ['a class whose name merely starts the same', '.desk-main-x { position: static; }'],
    ['a rule that names the class and changes something else', '.desk > .desk-main { padding: 0; }']
  ])('is quiet about %s', (_name, addition) => {
    expect(overrides(PANE + addition)).toEqual([])
  })

  it('is quiet about a nested scroller whose element does position itself', () => {
    const nested =
      '.desk-main { position: relative; @media (min-width: 600px) { overflow: auto; } }'
    expect(missing(nested)).toEqual([])
    expect(overrides(nested)).toEqual([])
  })

  it('does not let one module’s class reach another module’s', () => {
    // Module classes are hashed, so `.list` in two modules are two classes.
    // A reader that compared bare tokens across sheets would fail this correct
    // tree; one that compared them within a module has to still catch the
    // second case.
    expect(
      read([
        {
          where: 'packs/PacksPane.module.css',
          text: '.list { overflow: hidden auto; position: relative }'
        },
        { where: 'ui/Tabs.module.css', text: '.list { position: static }' }
      ]).takenBack
    ).toEqual([])
    expect(
      read([
        {
          where: 'packs/PacksPane.module.css',
          text: '.list { overflow: hidden auto; position: relative }\n.list { position: static }'
        }
      ]).takenBack
    ).toEqual(['packs/PacksPane.module.css  .list  .list → position: static'])
    // And a global sheet cannot take a module class back either way round.
    expect(
      read([
        {
          where: 'packs/PacksPane.module.css',
          text: '.list { overflow: hidden auto; position: relative }'
        },
        { where: 'styles.css', text: '.list { position: static }' }
      ]).takenBack
    ).toEqual([])
  })

  // ---- Named as outside reach ---------------------------------------------

  it('is quiet about an override that reaches the pane without naming its class', () => {
    // Not a gap discovered later: the boundary of a class-token matcher, chosen
    // over emulating the cascade because two drafts that emulated it were
    // wrong. `.desk-main` is `<main id="main">`, so both of these do defeat the
    // pane in a browser and neither is reported here. The drive is what
    // measures the computed cascade — 49 configurations a build — and the
    // docstring at the top of this file says so.
    expect(overrides(PANE + '#main { position: static; }')).toEqual([])
    expect(overrides(PANE + "main[id='main'] { position: static; }")).toEqual([])
  })
})
