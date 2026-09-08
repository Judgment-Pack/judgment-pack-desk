/**
 * Admin's shape, read off the committed stylesheets.
 *
 * **Why the sheets and not a render.** vitest runs with `css: false`, so no
 * stylesheet is processed: a `border` put back on `.member` renders exactly
 * like one that was never there, and `getComputedStyle` in jsdom would report
 * a token name back rather than resolve it. What is held here is the source
 * the browser measurement in the PR was taken against — the numbers live in
 * the PR body and the rules live here, so a change that quietly reverts one
 * fails rather than waiting for somebody to look at the page again.
 *
 * Three claims, and each was the page before this chunk:
 *
 * - **No container on Admin draws a box.** A group was a frame around cards
 *   that were frames; hierarchy is type, spacing and hairlines now. A
 *   `border-top`/`border-bottom` is allowed and a four-sided `border` is not,
 *   because a hairline is a separator and a frame is an object's boundary.
 * - **No form frames its own fields.** The `<fieldset>` every form on this
 *   desk uses to group what a `disabled` applies to was drawing the browser's
 *   own groove, which is why there was no rule in any sheet to delete.
 * - **Nothing on Admin is tracked capitals.** Two label styles on one page —
 *   `Location` in bold small, `ORGANIZATION` in tracked caps — read as two
 *   pages joined at a heading.
 */
import { readFileSync } from 'node:fs'
import { join } from 'node:path'
import { describe, expect, it } from 'vitest'
import { declarationsIn } from '../ui/declarations'

const SRC = join(import.meta.dirname, '..')
const read = (name: string) => readFileSync(join(SRC, name), 'utf8')

/** The three sheets that dress Admin, and the only ones this file speaks for. */
const ADMIN_SHEETS = [
  'admin/SourceCard.module.css',
  'admin/AdminStatusLine.module.css',
  'routes/AdminView.module.css'
] as const

const STYLES = read('styles.css')

/**
 * One rule's declarations, by the exact selector that opens it.
 *
 * Brace-matched rather than `[^}]*`, so a rule containing a nested block is
 * read whole rather than truncated at the first `}` inside it — and the
 * selector is matched on its own line, so `.member` does not also return
 * `.members`.
 */
function ruleBody(sheet: string, selector: string): string | undefined {
  const text = sheet.replace(/\/\*[\s\S]*?\*\//g, '')
  const at = new RegExp(`(^|\\})\\s*${selector.replace(/[.]/g, '\\.')}\\s*\\{`, 'm').exec(text)
  if (at === null) return undefined
  const open = at.index + at[0].length - 1
  let depth = 0
  for (let scan = open; scan < text.length; scan += 1) {
    if (text[scan] === '{') depth += 1
    if (text[scan] === '}') {
      depth -= 1
      if (depth === 0) return text.slice(open + 1, scan)
    }
  }
  return undefined
}

/** The containers this page draws, and the sheet each is written in. */
const CONTAINERS = [
  ['admin/SourceCard.module.css', '.card'],
  ['admin/SourceCard.module.css', '.group'],
  ['admin/SourceCard.module.css', '.member'],
  ['admin/AdminStatusLine.module.css', '.line']
] as const

describe('no container on Admin draws a box', () => {
  it.each(CONTAINERS)('%s %s sets no background and no four-sided border', (sheet, selector) => {
    const body = ruleBody(read(sheet), selector)
    expect(body, `${sheet} has no ${selector} rule`).toBeDefined()
    for (const declaration of declarationsIn(`${selector} {${body}}`)) {
      // A hairline on one edge is a separator and is the whole point of the
      // shape. `border` and `border-radius` are the frame.
      expect(
        declaration.property,
        `${sheet} ${selector}: ${declaration.property}: ${declaration.value}`
      ).not.toBe('border')
      expect(
        declaration.property,
        `${sheet} ${selector}: ${declaration.property}: ${declaration.value}`
      ).not.toBe('background')
      expect(
        declaration.property,
        `${sheet} ${selector}: ${declaration.property}: ${declaration.value}`
      ).not.toBe('background-color')
    }
  })

  it('leaves one frame standing, and it is the code block', () => {
    // The rule the shape is *for*: a frame is drawn around an object, and a
    // `pre` full of somebody's file is one. A test that only ever said "no
    // borders" would be satisfied by a page that had lost this one too.
    const body = ruleBody(read('admin/SourceCard.module.css'), '.json')
    expect(body).toBeDefined()
    expect(body).toContain('border: 1px solid var(--border)')
    expect(body).toContain('border-radius: var(--radius-sm)')
  })

  it('reads a rule by brace matching, and tells .member from .members', () => {
    // The instrument, on fixtures rather than only on sheets that are clean.
    const sheet = '.members { gap: 0; }\n.member { padding-block: 1rem; }'
    expect(ruleBody(sheet, '.member')?.trim()).toBe('padding-block: 1rem;')
    expect(ruleBody(sheet, '.members')?.trim()).toBe('gap: 0;')
    expect(ruleBody('.a { color: red; }', '.b')).toBeUndefined()
    expect(ruleBody('.a { b: 1; @media (min-width: 1px) { .c { d: 2; } } e: 3; }', '.a')).toContain(
      'e: 3'
    )
  })

  it('catches a background or a border put back on a container', () => {
    // A rule that passes over a clean sheet proves nothing about the rule.
    const boxed = '.member { border: 1px solid var(--border); background: var(--bg); }'
    const properties = declarationsIn(boxed).map((declaration) => declaration.property)
    expect(properties).toContain('border')
    expect(properties).toContain('background')
  })
})

describe('no form frames its own fields', () => {
  it('resets the fieldset globally, border included', () => {
    const body = ruleBody(STYLES, 'fieldset')
    expect(body, 'styles.css carries no fieldset rule').toBeDefined()
    const declarations = new Map(
      declarationsIn(`fieldset {${body}}`).map((each) => [each.property, each.value])
    )
    expect(declarations.get('border')).toBe('0')
    expect(declarations.get('padding')).toBe('0')
    expect(declarations.get('margin')).toBe('0')
    // The other half: a fieldset's default `min-content` sizing is what stops
    // a grid or flex child inside one from ever shrinking.
    expect(declarations.get('min-width')).toBe('0')
  })
})

describe('nothing on Admin is tracked capitals', () => {
  it.each(ADMIN_SHEETS)('%s spells no uppercase and no tracking', (name) => {
    const sheet = read(name).replace(/\/\*[\s\S]*?\*\//g, '')
    expect(sheet).not.toMatch(/text-transform:\s*uppercase/)
    expect(sheet).not.toMatch(/letter-spacing:/)
  })
})

describe('one label style, written in both places', () => {
  it('gives SourceCard’s key and Field’s label the same three values', () => {
    // `Location` and `Name` are the same kind of label and were 0.78rem/600
    // and 0.82rem/600 in two files. The values are picked once; this is what
    // stops one of them being edited alone.
    const key = ruleBody(read('admin/SourceCard.module.css'), '.key')
    const label = ruleBody(read('ui/Field.module.css'), '.label')
    expect(key).toBeDefined()
    expect(label).toBeDefined()
    const values = (body: string) =>
      new Map(
        declarationsIn(`.a {${body}}`)
          .filter((each) => ['font-size', 'font-weight', 'color'].includes(each.property))
          .map((each) => [each.property, each.value])
      )
    expect([...values(label!)].sort()).toEqual([...values(key!)].sort())
    expect(values(key!).get('font-size')).toBe('0.8rem')
  })
})
