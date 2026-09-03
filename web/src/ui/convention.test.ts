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
    const sheet = read(name)
    const declarations = [...sheet.matchAll(/(background|color|border[a-z-]*|outline[a-z-]*):\s*([^;]+);/g)]
    for (const [, property, value] of declarations) {
      if (!/\b(background|color)\b/.test(property!)) continue
      const words = value!.trim()
      if (words === 'none' || words === 'transparent' || words === 'inherit') continue
      expect(words, `${name}: ${property}: ${words}`).toContain('var(--')
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
