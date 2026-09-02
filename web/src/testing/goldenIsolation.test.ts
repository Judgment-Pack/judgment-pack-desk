/**
 * Two cheap guards over files this line of work must not disturb.
 *
 * **The extraction goldens.** `extractionGoldens.ts` pins `innerHTML` strings,
 * and Radix's `useId` injects generated ids into anything it renders — the
 * spike that preceded this work produced `radix-_r_a_-trigger-files`. A Radix
 * import in one of the four files those goldens cover would put a generated id
 * inside a pinned string, and the failure would read as a rendering change
 * rather than as an import. Four files and not two, because `EvaluationView`
 * renders `TracePanel` and `MatrixRowList` renders `TargetPair`.
 *
 * **The one stylesheet cut.** `styles.css` lost exactly the `/* Layout *\/`
 * block. Its token block and its four badge rules are what the shell reads and
 * what `ConnectionBadge` still wears, and a second cut taken later — for
 * tidiness, or by a merge — should fail here rather than on screen.
 */
import { readFileSync } from 'node:fs'
import { join } from 'node:path'
import { describe, expect, it } from 'vitest'

const SRC = join(import.meta.dirname, '..')

const GOLDEN_RENDERERS = [
  'components/EvaluationView.tsx',
  'components/MatrixRowList.tsx',
  'components/TracePanel.tsx',
  'components/TargetPair.tsx'
]

describe('the golden-pinned renderers', () => {
  it.each(GOLDEN_RENDERERS)('%s imports nothing from radix-ui', (relative) => {
    const source = readFileSync(join(SRC, relative), 'utf8')
    expect(source).not.toContain('radix-ui')
    expect(source).not.toContain('@radix-ui/')
  })
})

describe('the one stylesheet cut', () => {
  const sheet = readFileSync(join(SRC, 'styles.css'), 'utf8')

  it('keeps the :root token block, including the three verdict colours', () => {
    expect(sheet).toContain(':root {')
    for (const token of ['--true:', '--false:', '--unknown:', '--accent:', '--radius:']) {
      expect(sheet).toContain(token)
    }
  })

  it('keeps the four badge rules ConnectionBadge still wears', () => {
    for (const rule of ['.badge {', '.badge-ready {', '.badge-failed {', '.badge-reconnecting {']) {
      expect(sheet).toContain(rule)
    }
  })

  it('no longer carries the Layout block, which moved to shell.css', () => {
    for (const gone of ['.app {', '.app-head {', '.brand {', '.app-body {', '.app-foot {']) {
      expect(sheet).not.toContain(gone)
    }
  })

  it('leaves the shell sheet entirely inside its own cascade layer', () => {
    // Unlayered author rules beat every layer, so `styles.css` staying
    // unlayered is what stops a `.desk-*` selector winning a collision by
    // accident. The shell's `!important` still wins, because important
    // declarations invert layer order.
    const shell = readFileSync(join(SRC, 'shell.css'), 'utf8')
    expect(shell.trimStart().startsWith('/*')).toBe(true)
    // Anchored at a line start, so the note about `@layer components` in the
    // sheet's own comment is not counted as a second layer.
    expect(shell).toContain('@layer shell {')
    expect(shell.match(/^@layer /gm)).toHaveLength(1)
    expect(sheet.match(/^@layer /gm)).toBeNull()
  })
})
