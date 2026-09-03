/**
 * The styling convention, held by reading the source.
 *
 * It cannot be held by rendering: vitest runs with `css: false`, so no
 * stylesheet is processed and a component whose module was deleted renders
 * exactly as one whose module is intact. The four rules the README states are
 * therefore checked as text, in the `goldenIsolation.test.ts` idiom.
 *
 * **Why a module needs no `@layer` of its own.** Modules are unlayered author
 * rules, so they beat every `@layer shell` rule by construction; and they
 * cannot collide with `styles.css` because a module's class names are hashed
 * at build time. So there is no import-order rule in `main.tsx` to remember and
 * no layer to keep in sync — which is the whole reason this convention is
 * cheap enough to hold.
 */
import { readdirSync, readFileSync } from 'node:fs'
import { join } from 'node:path'
import { describe, expect, it } from 'vitest'
import { colourProblem, declarationsIn } from './declarations'

const UI = import.meta.dirname
const read = (name: string) => readFileSync(join(UI, name), 'utf8')

const components = readdirSync(UI).filter(
  (name) => name.endsWith('.tsx') && !name.includes('.test.')
)
const modules = readdirSync(UI).filter((name) => name.endsWith('.module.css'))

describe('the styling convention', () => {
  it('has a component and a module for each, and no orphan of either', () => {
    // A component with no module is one styling something through another
    // sheet; a module with no component is a rule nothing renders.
    expect(components.length).toBeGreaterThan(0)
    expect(modules.sort()).toEqual(
      components.map((name) => name.replace(/\.tsx$/, '.module.css')).sort()
    )
  })

  it.each(components)('%s carries no inline style', (name) => {
    // An inline style beats every sheet without `!important` and cannot be
    // themed, so it is the one way a component can quietly opt out of the
    // tokens. VisuallyHidden is inline for exactly this reason, and is exactly
    // why the skip link is not one.
    expect(read(name)).not.toContain('style={{')
  })

  it.each(components)('%s imports its own module and no other sheet', (name) => {
    const source = read(name)
    expect(source).toContain(`from './${name.replace(/\.tsx$/, '.module.css')}'`)
    const sheets = [...source.matchAll(/from '([^']*\.css)'/g)].map((match) => match[1])
    expect(sheets).toEqual([`./${name.replace(/\.tsx$/, '.module.css')}`])
  })

  it.each(modules)('%s spells no colour of its own', (name) => {
    // `styles.css` is the only source of colour. A literal here would be a
    // second palette that the theme attribute does not reach.
    const sheet = read(name)
    expect(sheet).not.toMatch(/#[0-9a-fA-F]{3,8}\b/)
    for (const fn of ['rgb(', 'rgba(', 'hsl(', 'hsla(', 'color(', 'oklch(']) {
      expect(sheet, `${name} carries ${fn}`).not.toContain(fn)
    }
  })

  it.each(modules)('%s selects only through its own classes', (name) => {
    // A bare element selector inside a module is global — it is not hashed —
    // so one `button { … }` here would restyle every button in the desk.
    const sheet = read(name).replace(/\/\*[\s\S]*?\*\//g, '')
    const selectors = [...sheet.matchAll(/(^|\})\s*([^{}@]+)\{/g)].map((match) => match[2]!.trim())
    expect(selectors.length).toBeGreaterThan(0)
    for (const selector of selectors) {
      for (const part of selector.split(',')) {
        expect(part.trim().startsWith('.'), `${name}: "${part.trim()}" is not a class`).toBe(true)
      }
    }
  })

  it.each(modules)('%s takes every radius from a token', (name) => {
    // The README says the tokens are the only source of colour *and radius*,
    // and before this rule existed one module spelled `border-radius: 4px`
    // while the colour rule below reported the file clean. A claim with no
    // holder is how that happens.
    const sheet = read(name)
    for (const [, value] of read(name).matchAll(/border-radius:\s*([^;]+);/g)) {
      const words = value!.trim()
      if (words === '0' || words === 'inherit') continue
      expect(words, `${name}: border-radius: ${words}`).toContain('var(--')
    }
    expect(sheet).not.toMatch(/border-radius:\s*\d/)
  })

  it.each(modules)('%s takes every colour from a token', (name) => {
    // **Parsed, not pattern-matched.** The rule this replaces matched
    // `border`/`outline` declarations and then skipped them, because it asked
    // whether the *property name* contained "background" or "color" — so
    // `border: 1px solid red`, `outline: 2px solid red` and a named colour in
    // a `box-shadow` all passed the rule that exists to catch them.
    for (const declaration of declarationsIn(read(name))) {
      const problem = colourProblem(declaration)
      expect(
        problem,
        `${name}: ${declaration.property}: ${declaration.value} — ${problem}`
      ).toBeUndefined()
    }
  })
})

describe('what the other sheets keep', () => {
  it('leaves layout of the five regions to shell.css, not to a component', () => {
    for (const name of modules) {
      const sheet = read(name)
      for (const region of ['--rail-current', '--inspector-current', '--console-current', 'grid-template-areas']) {
        expect(sheet, `${name} lays out a region`).not.toContain(region)
      }
    }
  })

  it('adds no cascade layer, because an unlayered module already wins', () => {
    for (const name of modules) {
      expect(read(name)).not.toContain('@layer')
    }
  })
})

/**
 * The convention's own instrument, checked against deliberate failures.
 *
 * A rule that passes over clean sheets proves nothing about the rule: the one
 * it replaces did exactly that for as long as it existed. So each shape it
 * used to miss is written out here as a fixture and asserted to be caught, and
 * the shapes that are legitimate are asserted to be left alone.
 */
describe('the colour rule catches what it used to miss', () => {
  const caught = (sheet: string) =>
    declarationsIn(sheet)
      .map((declaration) => colourProblem(declaration))
      .filter((problem): problem is string => problem !== undefined)

  it.each([
    ['a hex in a shorthand', '.a { border: 1px solid #ff0000; }'],
    ['a named colour in a border', '.a { border: 1px solid red; }'],
    ['a named colour in an outline', '.a { outline: 2px solid red; }'],
    ['a named colour in a shadow', '.a { box-shadow: 0 1px 2px rebeccapurple; }'],
    ['a colour function in a shadow', '.a { box-shadow: 0 1px 2px rgb(0 0 0 / 10%); }'],
    // A second layer added later, beside a token that is doing its job. The
    // value contains `var(--`, so the "took its colour from somewhere" clause
    // is satisfied and only the named-colour clause can catch this one.
    [
      'a named colour beside a token',
      '.a { box-shadow: 0 1px 2px var(--shadow), 0 2px 4px red; }'
    ],
    ['a named colour beside a token in a border', '.a { border: 1px solid var(--border) red; }'],
    ['a bare named colour', '.a { color: hotpink; }'],
    ['a longhand nobody enumerated', '.a { caret-color: red; }'],
    ['a fill on an icon', '.a { fill: black; }'],
    ['a colour inside a nested block', '@media (min-width: 1px) { .a { color: red; } }'],
    ['a border with no colour at all from a token', '.a { border-top: 1px solid; }']
  ])('catches %s', (_what, sheet) => {
    expect(caught(sheet).length).toBeGreaterThan(0)
  })

  it.each([
    ['a token in a shorthand', '.a { border: 1px solid var(--border); }'],
    ['a token in a shadow', '.a { box-shadow: 0 1px 2px var(--shadow); }'],
    ['transparent', '.a { background: transparent; }'],
    ['none', '.a { outline: none; }'],
    ['currentColor', '.a { fill: currentColor; }'],
    ['a property that carries no colour', '.a { padding: 4px 8px; margin: 0 auto; }'],
    ['a token whose name contains a colour word', '.a { color: var(--ink-red); }'],
    ['a commented-out literal', '.a { /* color: red; */ color: var(--ink); }'],
    ['a string that happens to say red', '.a::after { content: "red"; }']
  ])('leaves %s alone', (_what, sheet) => {
    expect(caught(sheet)).toEqual([])
  })

  it('reads a declaration at every nesting depth', () => {
    const sheet = `
      .a { color: var(--ink); }
      @media (min-width: 40rem) {
        .b { background: var(--surface); }
        @supports (display: grid) {
          .c { border: 1px solid var(--border); }
        }
      }
    `
    expect(declarationsIn(sheet).map((declaration) => declaration.property)).toEqual([
      'color',
      'background',
      'border'
    ])
  })
})
