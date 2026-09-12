/**
 * The two palettes, and the one scale, read off the committed stylesheet.
 *
 * **Why the sheet and not a render.** vitest runs with `css: false`, so no
 * stylesheet is processed: a dark block that was deleted renders exactly like
 * one that is intact, and `getComputedStyle` in jsdom would report the token
 * name back rather than resolve it. What can be held here is the source the
 * browser measurement in the PR was taken against — so the numbers live in the
 * PR body and the rules live here, and a change that quietly reverts one fails.
 *
 * Four claims, and each was a defect before it was a rule:
 *
 * - **Every colour token has a dark value.** Both blocks used to carry the
 *   light ones, so choosing dark set an attribute and changed no colour.
 * - **The two blocks that select dark are identical.** They cannot be written
 *   once — one is inside a media query — so nothing but a test stops one being
 *   edited and the other forgotten.
 * - **Contrast is measured.** Every text/background and every semantic pair,
 *   in *both* palettes, at WCAG AA. It is the light palette this caught first:
 *   `--ink-faint` reached 3.4:1 on the page and is darker for it.
 * - **No sheet but this one spells a colour.** `convention.test.ts` holds the
 *   modules; the two global sheets were never covered, and `shell.css` was
 *   spelling three.
 */
import { readFileSync, readdirSync } from 'node:fs'
import { join } from 'node:path'
import { describe, expect, it } from 'vitest'
import { ROW_HEIGHT } from '../config/theme'
import {
  blockAt,
  contrastRatio,
  isColourValue,
  lengthOf,
  literalColoursIn,
  luminance,
  withoutBlocks,
  type Tokens
} from './palette'

const SRC = join(import.meta.dirname, '..')
const STYLES = readFileSync(join(SRC, 'styles.css'), 'utf8')
const SHELL = readFileSync(join(SRC, 'shell.css'), 'utf8')

/** The three blocks the palette lives in, by the selector that opens each. */
const LIGHT_AT = '\n:root {'
const MEDIA_DARK_AT = ':root:not([data-theme="light"]) {'
const ATTRIBUTE_DARK_AT = '\n:root[data-theme="dark"] {'
/** And the fourth, which is the density scale rather than a palette. */
const COMPACT_AT = ':root[data-density="compact"] {'

const light = blockAt(STYLES, LIGHT_AT)
const mediaDark = blockAt(STYLES, MEDIA_DARK_AT)
const attributeDark = blockAt(STYLES, ATTRIBUTE_DARK_AT)
const compact = blockAt(STYLES, COMPACT_AT)

/** The tokens on `:root` whose value is a colour, which is what a palette is. */
const colourTokens = [...light].filter(([, value]) => isColourValue(value)).map(([name]) => name)

/** One token's value in one palette, refusing a name the palette does not carry. */
function colour(tokens: Tokens, name: string): string {
  const value = tokens.get(name)
  expect(value, `${name} is defined`).toBeDefined()
  return value!
}

/** The dark palette as a whole map: `:root`'s tokens with the dark ones over them. */
const dark: Tokens = new Map([...light, ...attributeDark])

describe.each([['light', light], ['dark', dark]] as const)('%s uses only neutral, green and gold colors', (_name, palette) => {
  it('keeps every chromatic token within the brand families', () => {
    for (const [name, value] of palette) {
      if (!/^#[\da-f]{6}$/i.test(value)) continue
      const [r, g, b] = [1, 3, 5].map((offset) => parseInt(value.slice(offset, offset + 2), 16)) as [number, number, number]
      const max = Math.max(r, g, b), min = Math.min(r, g, b), delta = max - min
      // The existing neutral surfaces have a small cool bias. Preserve them.
      if (delta <= 16) continue
      const hue = ((max === r ? (g - b) / delta : max === g ? (b - r) / delta + 2 : (r - g) / delta + 4) * 60 + 360) % 360
      expect((hue >= 25 && hue <= 55) || (hue >= 100 && hue <= 190), `${name}: ${value} has hue ${hue.toFixed(1)}`).toBe(true)
    }
  })
})

describe('every colour token has a dark value', () => {
  it('defines the same colour names in both dark blocks as on :root', () => {
    // The defect this replaces: both blocks carried the light values, so
    // choosing dark set `data-theme` and changed nothing anybody could see.
    expect([...mediaDark.keys()].sort()).toEqual([...colourTokens].sort())
    expect([...attributeDark.keys()].sort()).toEqual([...colourTokens].sort())
  })

  it('gives no colour a home that only the dark palette reaches', () => {
    // The other direction, and it is not the same claim: a token defined only
    // inside a dark block is one the light palette cannot paint at all, and
    // every surface that used it would fall back to the property's initial
    // value in the light theme — silently.
    for (const name of [...mediaDark.keys(), ...attributeDark.keys()]) {
      expect(light.has(name), `${name} is defined on :root`).toBe(true)
    }
  })

  it('carries the same value in both blocks, token for token', () => {
    // They cannot be written once: one is inside `@media (prefers-color-scheme:
    // dark)` and the other is not, and CSS has no way to share a declaration
    // block across that boundary. So the only thing standing between them and
    // a value edited in one is this.
    expect(Object.fromEntries(mediaDark)).toEqual(Object.fromEntries(attributeDark))
  })

  it('changes theme surfaces and text while preserving the identity badge palette', () => {
    // Small identity badges keep the website colors; other accents adapt for contrast.
    const invariant = new Set(['--brand-fill', '--brand-label', '--ink-inverse'])
    for (const name of colourTokens) {
      if (invariant.has(name)) {
        expect(colour(attributeDark, name), `${name} is shared between themes`).toBe(colour(light, name))
      } else {
        expect(colour(attributeDark, name), `${name} adapts to the theme`).not.toBe(colour(light, name))
      }
    }
  })

  it('keeps the non-colour tokens out of both dark blocks', () => {
    // `--radius`, the type stacks and the density scale are tokens too, and a
    // palette block is not where any of them belongs.
    for (const name of ['--radius', '--radius-sm', '--mono', '--sans', '--density-row']) {
      expect(light.has(name), `${name} is on :root`).toBe(true)
      expect(attributeDark.has(name), `${name} is not in the dark block`).toBe(false)
    }
  })

  it('paints the body from a token rather than leaving it to the browser', () => {
    // A transparent body borrows whatever is behind it, which in a dark theme
    // is a white canvas under dark text.
    // Every rule the element `body` is selected by, together: it is styled
    // once beside `html` for its margins and once on its own for its paint,
    // and reading only the first of them is how this rule passed on a sheet
    // that had lost the second.
    const body = [...STYLES.matchAll(/\nbody \{([^}]*)\}/g)].map((rule) => rule[1]!).join('')
    expect(body).toMatch(/background:\s*var\(--bg\);/)
    expect(body).toMatch(/color:\s*var\(--ink\);/)
  })
})

/**
 * The pairs, and the ratio each has to clear.
 *
 * 4.5:1 is AA for body text. 3:1 is what AA asks of large text and of a user
 * interface component's own boundary — which is why the focus ring is here at
 * 3:1 and `--border` is **not** here at all: the borders in this desk separate
 * regions rather than identify controls, they are 1.3:1 and 1.6:1 in the light
 * palette as they have always been, and holding them to 3:1 would be this
 * chunk re-authoring the light desk under cover of shipping a dark one. Named
 * rather than omitted, so the gap is a decision and not an oversight.
 */
const PAIRS: { front: string; back: string; least: number; why: string }[] = [
  { front: '--ink', back: '--bg', least: 4.5, why: 'primary text on the page' },
  { front: '--ink', back: '--surface', least: 4.5, why: 'primary text on a card' },
  { front: '--ink', back: '--surface-raised', least: 4.5, why: 'primary text in a menu' },
  { front: '--ink', back: '--code-surface', least: 4.5, why: 'a JSON block' },
  { front: '--ink', back: '--selection', least: 4.5, why: 'selected text' },
  { front: '--ink-soft', back: '--bg', least: 4.5, why: 'muted text on the page' },
  { front: '--ink-soft', back: '--surface', least: 4.5, why: 'muted text on a card' },
  { front: '--ink-soft', back: '--surface-raised', least: 4.5, why: 'muted text in a menu' },
  { front: '--ink-faint', back: '--bg', least: 4.5, why: 'a label on the page' },
  { front: '--ink-faint', back: '--surface', least: 4.5, why: 'a label on a card' },
  { front: '--ink-faint', back: '--surface-raised', least: 4.5, why: 'a note in a menu' },
  { front: '--brand-label', back: '--brand-fill', least: 4.5, why: 'avatar and organization initials' },
  { front: '--accent', back: '--sidebar', least: 4.5, why: 'selected navigation text' },
  { front: '--ink-inverse', back: '--accent-fill', least: 4.5, why: 'a primary button' },
  { front: '--sidebar-ink', back: '--sidebar', least: 4.5, why: 'inactive navigation' },
  { front: '--ink-inverse', back: '--accent-hover', least: 4.5, why: 'a primary button, hovered' },
  { front: '--ink-inverse', back: '--accent-active', least: 4.5, why: 'a primary button, pressed' },
  { front: '--accent', back: '--surface', least: 4.5, why: 'a link on a card' },
  { front: '--accent', back: '--accent-soft', least: 4.5, why: 'a chosen nav item' },
  { front: '--danger', back: '--surface', least: 4.5, why: 'an error on a card' },
  { front: '--danger', back: '--danger-soft', least: 4.5, why: 'an error box' },
  { front: '--warn', back: '--surface', least: 4.5, why: 'a warning on a card' },
  { front: '--warn', back: '--warn-soft', least: 4.5, why: 'a warning banner' },
  { front: '--true', back: '--true-soft', least: 4.5, why: 'a true verdict' },
  { front: '--false', back: '--false-soft', least: 4.5, why: 'a false verdict' },
  { front: '--unknown', back: '--unknown-soft', least: 4.5, why: 'an unknown verdict' },
  { front: '--accent', back: '--bg', least: 3, why: 'the focus ring over the page' },
  { front: '--accent', back: '--surface', least: 3, why: 'the focus ring over a card' }
]

describe.each([
  ['the light palette', light],
  ['the dark palette', dark]
])('%s meets WCAG AA where it is read', (_name, palette) => {
  it.each(PAIRS.map((pair) => [`${pair.front} on ${pair.back} (${pair.why})`, pair] as const))(
    '%s',
    (_what, pair) => {
      const ratio = contrastRatio(colour(palette, pair.front), colour(palette, pair.back))
      expect(
        Number(ratio.toFixed(2)),
        `${pair.front} on ${pair.back} is ${ratio.toFixed(2)}:1, under ${pair.least}:1`
      ).toBeGreaterThanOrEqual(pair.least)
    }
  )
})

describe('the density scale tightens, and every token in it does', () => {
  it('gives every --density- token on :root a compact value', () => {
    // The prefix is what makes "every spacing token" a question the sheet can
    // answer about itself: a rule that went by a list kept in the test would
    // pass for ever the day a seventh token was added and not listed.
    const scale = [...light.keys()].filter((name) => name.startsWith('--density-'))
    expect(scale.length).toBeGreaterThan(0)
    expect([...compact.keys()].sort()).toEqual([...scale].sort())
  })

  it('makes each compact value strictly smaller, in the same unit', () => {
    // Strictly. A compact value *equal* to its comfortable one is a density
    // that is offered, stored, applied to the root element — and changes
    // nothing, which is the shape of the defect this scale exists to close.
    for (const [name, tight] of compact) {
      const roomy = lengthOf(colour(light, name))
      const dense = lengthOf(tight)
      expect(roomy, `${name} is a plain length`).toBeDefined()
      expect(dense, `${name}'s compact value is a plain length`).toBeDefined()
      expect(dense!.unit, `${name} keeps its unit`).toBe(roomy!.unit)
      expect(dense!.amount, `${name}: ${tight} is not smaller than ${light.get(name)}`).toBeLessThan(
        roomy!.amount
      )
    }
  })

  it('is the same row height the windowed list computes with', () => {
    // The one number the sheet cannot keep to itself: the packs list reserves
    // two spacers for the rows it is not rendering, and that arithmetic is
    // done in JavaScript. A `--density-row` tightened here while `ROW_HEIGHT`
    // stayed at 40 would scroll to the wrong place and focus the wrong row.
    expect(lengthOf(colour(light, '--density-row'))).toEqual({
      amount: ROW_HEIGHT.comfortable,
      unit: 'px'
    })
    expect(lengthOf(colour(compact, '--density-row'))).toEqual({
      amount: ROW_HEIGHT.compact,
      unit: 'px'
    })
  })
})

describe('no sheet but styles.css spells a colour', () => {
  it('finds no literal in shell.css', () => {
    // It used to spell three: `#fff` on the Create button, the drawer scrim,
    // and the menu's shadow. Each was a colour the theme attribute could not
    // reach, which a light-only desk had no way of showing.
    const found = literalColoursIn(SHELL)
    expect(found.map((entry) => `${entry.where}: ${entry.problem}`)).toEqual([])
  })

  it('finds no literal in styles.css outside the palette blocks', () => {
    // Inside them a literal *is* the palette. Everywhere else in this file it
    // is a fourth palette that no `data-theme` selector reaches — which is
    // what `#fbfbf9` was, in two rules, for as long as this sheet has existed.
    const rules = withoutBlocks(STYLES, [
      LIGHT_AT,
      MEDIA_DARK_AT,
      ATTRIBUTE_DARK_AT,
      COMPACT_AT
    ])
    const found = literalColoursIn(rules)
    expect(found.map((entry) => `${entry.where}: ${entry.problem}`)).toEqual([])
  })
})

/**
 * The instrument, against answers that are known independently of this desk.
 *
 * A contrast function that returned 21 for everything would pass every
 * assertion above, and a token reader that found nothing would pass three of
 * them. Both were written for this piece of work, so both are checked before
 * they are trusted — the same reason `declarations.ts` is proven on
 * deliberately broken fixtures rather than only on sheets that are clean.
 */
describe('the instrument', () => {
  it('computes the ratios WCAG defines', () => {
    expect(contrastRatio('#000000', '#ffffff')).toBeCloseTo(21, 5)
    expect(contrastRatio('#ffffff', '#000000')).toBeCloseTo(21, 5)
    expect(contrastRatio('#123456', '#123456')).toBeCloseTo(1, 5)
    // The mid grey WCAG's own examples use: #767676 on white is 4.54:1.
    expect(contrastRatio('#767676', '#ffffff')).toBeCloseTo(4.54, 2)
    // And the short form is the long one.
    expect(contrastRatio('#fff', '#000')).toBeCloseTo(21, 5)
  })

  it('refuses a colour it cannot measure rather than guessing at one', () => {
    expect(() => luminance('rgb(0 0 0 / 25%)')).toThrow(/not a hex colour/)
    expect(() => luminance('var(--ink)')).toThrow(/not a hex colour/)
  })

  it('tells a colour token from every other kind', () => {
    expect(isColourValue('#1b1b19')).toBe(true)
    expect(isColourValue('rgb(0 0 0 / 25%)')).toBe(true)
    expect(isColourValue('8px')).toBe(false)
    expect(isColourValue('ui-monospace, Menlo, Consolas, monospace')).toBe(false)
  })

  it('reads a block by brace matching and refuses an ambiguous one', () => {
    expect([...blockAt('a { --x: 1px; --y: red; }', 'a {')]).toEqual([
      ['--x', '1px'],
      ['--y', 'red']
    ])
    // A nested block belongs to the block that contains it.
    expect([...blockAt('@media x { a { --x: 1px; } }', '@media x {').keys()]).toEqual(['--x'])
    expect(() => blockAt('a { --x: 1px; }', 'b {')).toThrow(/no block/)
    expect(() => blockAt('a { }\na { }', 'a {')).toThrow(/two blocks/)
  })

  it('catches every spelling of a colour, and leaves the keywords alone', () => {
    const caught = (sheet: string) => literalColoursIn(sheet).map((entry) => entry.problem)
    expect(caught('.a { color: #ff0000; }')).toHaveLength(1)
    expect(caught('.a { border: 1px solid red; }')).toHaveLength(1)
    expect(caught('.a { box-shadow: 0 1px 2px rgb(0 0 0 / 10%); }')).toHaveLength(1)
    expect(caught('.a { --mine: #ff0000; }')).toHaveLength(1)
    expect(caught('.a { color: var(--ink, rebeccapurple); }')).toHaveLength(1)
    expect(caught('@media (min-width: 1px) { .a { fill: black; } }')).toHaveLength(1)
    // And the shapes a global sheet is allowed: a token, the keywords, a
    // transparent placeholder border, and a word that is not a colour.
    expect(caught('.a { color: var(--ink); }')).toEqual([])
    expect(caught('.a { border: 1px solid transparent; }')).toEqual([])
    expect(caught('.a { outline: none; fill: currentColor; }')).toEqual([])
    expect(caught('.a::after { content: "red"; }')).toEqual([])
    expect(caught('.a { /* color: red; */ color: var(--ink); }')).toEqual([])
  })

  it('reads a length, and nothing that is not one', () => {
    expect(lengthOf('40px')).toEqual({ amount: 40, unit: 'px' })
    expect(lengthOf('0.45rem')).toEqual({ amount: 0.45, unit: 'rem' })
    expect(lengthOf('calc(100vh - 4px)')).toBeUndefined()
    expect(lengthOf('var(--x)')).toBeUndefined()
    expect(lengthOf('40')).toBeUndefined()
  })

  it('cuts a block out of a sheet and leaves the rest whole', () => {
    expect(withoutBlocks('a { --x: red; }\n.b { color: var(--x); }', ['a {'])).toBe(
      'a {}\n.b { color: var(--x); }'
    )
  })
})


describe('all stylesheet references have a source', () => {
  it('resolves custom properties across CSS and the shell, apart from Radix-owned values', () => {
    const files = (dir: string): string[] => readdirSync(dir, { withFileTypes: true }).flatMap((entry) => {
      const path = join(dir, entry.name)
      return entry.isDirectory() ? files(path) : /\.(css|tsx?)$/.test(path) && !path.includes('.test.') ? [path] : []
    })
    const sources = files(SRC).map((path) => ({ path, text: readFileSync(path, 'utf8').replace(/\/\*[\s\S]*?\*\//g, '') }))
    const defined = new Set(sources.flatMap(({ text }) => [
      ...[...text.matchAll(/(--[\w-]+)\s*:/g)].map((match) => match[1]),
      ...[...text.matchAll(/['"](--[\w-]+)['"]\s*[:,]/g)].map((match) => match[1])
    ]))
    const unresolved = sources.filter(({ path }) => path.endsWith('.css')).flatMap(({ path, text }) =>
      [...text.matchAll(/var\((--[\w-]+)/g)].filter((match) => !defined.has(match[1]) && !match[1]!.startsWith('--radix-'))
        .map((match) => `${path}: ${match[1]}`))
    expect(unresolved).toEqual([])
  })
})
