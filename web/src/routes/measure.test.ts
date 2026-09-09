/**
 * Every route states the width kind of its page, and nothing else does.
 *
 * **Why a source test and not a rendered one.** vitest runs with `css: false`
 * and jsdom lays nothing out, so a page whose cap was wrong renders exactly
 * like one whose cap is right: `getBoundingClientRect` here reports zero for
 * everything. What can be held is the two halves of the mechanism as text —
 * `shell/shellSheet.test.ts` holds the rules on `.desk-measure`, and this
 * holds the attribute those rules read. The widths themselves are measured in
 * a browser, and the numbers are in the PR.
 *
 * **The set of routes is derived, never listed.** It is read out of `App.tsx`,
 * which is where a route becomes a route, so a route added next month fails
 * this until somebody gives it a kind. A list kept here would pass for ever the
 * day the eleventh route was written — the defect the packs pane's own sweep
 * was rewritten to avoid, one directory over.
 *
 * **Only the top-level routes.** `data-measure` is read by `:has()` on
 * `.desk-measure`, so it describes the element the shell mounts into the
 * measure — and for `/packs` that is `PacksLayout`, with `PacksIndex` and
 * `PackView` rendering inside it through its `<Outlet />`. A nested child
 * stating a second kind would be a second answer to a question its layout has
 * already answered, and `:has()` would take whichever the sheet ordered last.
 * So the rule has two directions and both are held below: every top-level
 * route carries exactly one kind, and no other module under `web/src` carries
 * the attribute at all.
 */
import { readdirSync, readFileSync } from 'node:fs'
import { join, relative } from 'node:path'
import { describe, expect, it } from 'vitest'

const ROUTES_DIR = import.meta.dirname
const SRC = join(ROUTES_DIR, '..')
const APP = readFileSync(join(SRC, 'App.tsx'), 'utf8')

/** The three kinds, which are the three rules `shell.css` writes. */
const KINDS = ['form', 'wide', 'full'] as const

/**
 * One `<Route>` tag, from `<Route` to the `>` that closes it.
 *
 * The `>` of a JSX expression does not close a tag, so brace depth is tracked
 * — `element={<X />}` contains two of them. This is the reader
 * `scripts/containment-check.mjs` uses on the same file, for the same reason:
 * a Route written across four lines is still one Route, and a line-oriented
 * regex passes over it in silence.
 */
function routeTags(text: string): { span: string; nested: boolean }[] {
  const found: { span: string; nested: boolean }[] = []
  const tag = /<\/?Route\b/g
  let depth = 0
  let match: RegExpExecArray | null
  while ((match = tag.exec(text)) !== null) {
    if (match[0] === '</Route') {
      depth -= 1
      continue
    }
    let braces = 0
    let end = match.index
    for (; end < text.length; end += 1) {
      const character = text[end]
      if (character === '{') braces += 1
      else if (character === '}') braces -= 1
      else if (character === '>' && braces === 0) break
    }
    expect(end, 'a <Route tag in App.tsx never closes').toBeLessThan(text.length)
    const span = text.slice(match.index, end + 1)
    tag.lastIndex = end + 1
    found.push({ span, nested: depth > 0 })
    if (!/\/>$/.test(span)) depth += 1
  }
  return found
}

/** The component name in `element={<Name … />}`, or undefined where there is none. */
function elementOf(span: string): string | undefined {
  return /\belement=\{\s*<([A-Za-z][A-Za-z0-9]*)/.exec(span)?.[1]
}

/** Where `App.tsx` imports one name from, exactly as written. */
function importedFrom(name: string): string | undefined {
  const from = new RegExp(`import \\{([^}]*)\\} from '([^']+)'`, 'g')
  for (const match of APP.matchAll(from)) {
    const names = match[1]!.split(',').map((each) => each.trim())
    if (names.includes(name)) return match[2]
  }
  return undefined
}

/**
 * The route modules the shell mounts directly into the measure, and the module
 * each one lives in.
 *
 * `Navigate` is the one element that is not a page — `path="*"` renders a
 * redirect — and it is admitted by name rather than by being quietly skipped:
 * any other element resolving outside `./routes/` fails, so a page added from
 * somewhere else cannot slip past this reader.
 */
function topLevelRoutes(): { name: string; file: string }[] {
  const mounted: { name: string; file: string }[] = []
  for (const { span, nested } of routeTags(APP)) {
    if (nested) continue
    const name = elementOf(span)
    if (name === undefined) continue
    if (name === 'Navigate') continue
    const from = importedFrom(name)
    expect(from, `App.tsx mounts <${name} /> at the top level and imports it from nowhere`).toMatch(
      /^\.\/routes\//
    )
    const file = `${from!.replace('./routes/', '')}.tsx`
    // `GraphView` and `MatrixView` are each mounted twice — `/graphs` and
    // `/graphs/:graphId`, `/matrix` and `/packs/:packId/matrix` — and a module
    // has one top-level element whichever pattern reached it.
    if (!mounted.some((each) => each.file === file)) mounted.push({ name, file })
  }
  return mounted
}

const ROUTES = topLevelRoutes()

/**
 * The component's principal return: the one at the function body's own
 * indentation, which is the one that is not inside a branch.
 *
 * A route may return a `Loading` or an `ErrorBox` early, and those states carry
 * no kind — they are a primitive, not a page, and the measure's declared
 * default covers them. What has to state a kind is the page, and there is
 * exactly one of those per route module; this asserts that rather than taking
 * the first or the last of several.
 */
function principalReturn(source: string, name: string): string {
  const opening = source.indexOf(`export function ${name}(`)
  expect(opening, `${name} is exported from its own module`).toBeGreaterThan(-1)
  const body = source.slice(opening)
  const ends = body.indexOf('\n}\n')
  const within = body.slice(0, ends === -1 ? body.length : ends)
  const returns = [...within.matchAll(/^ {2}return[ (]/gm)]
  expect(returns, `${name} has exactly one return at its body's own indentation`).toHaveLength(1)
  return within.slice(returns[0]!.index! + '  return'.length)
}

/**
 * The opening tag of the JSX that a return returns.
 *
 * Comments are skipped, because a `//` line between `return (` and the element
 * is exactly where the reason for the element tends to be written.
 */
function rootTag(after: string): string {
  let scan = after.replace(/^[\s(]+/, '')
  for (;;) {
    if (scan.startsWith('//')) {
      scan = scan.slice(scan.indexOf('\n') + 1).replace(/^\s+/, '')
      continue
    }
    if (scan.startsWith('/*')) {
      scan = scan.slice(scan.indexOf('*/') + 2).replace(/^\s+/, '')
      continue
    }
    break
  }
  expect(scan.startsWith('<'), 'the return returns JSX').toBe(true)
  let braces = 0
  for (let end = 0; end < scan.length; end += 1) {
    const character = scan[end]
    if (character === '{') braces += 1
    else if (character === '}') braces -= 1
    else if (character === '>' && braces === 0) return scan.slice(0, end + 1)
  }
  throw new Error('the root element never closes its opening tag')
}

/** Every `.ts`/`.tsx` under `web/src` that is not a test. */
function everySource(directory: string): string[] {
  const found: string[] = []
  for (const entry of readdirSync(directory, { withFileTypes: true })) {
    const path = join(directory, entry.name)
    if (entry.isDirectory()) found.push(...everySource(path))
    else if (/\.tsx?$/.test(entry.name) && !entry.name.includes('.test.')) found.push(path)
  }
  return found
}

describe('every route states the width kind of its page', () => {
  it('read App.tsx and found the routes, so the sweep is not vacuous', () => {
    // A reader that returns nothing passes every rule under it. This is a
    // floor and not an equality: adding a route is allowed, and adding one
    // with no kind is what fails.
    expect(ROUTES.length).toBeGreaterThanOrEqual(8)
    expect(ROUTES.map((route) => route.name)).toContain('AdminView')
    expect(ROUTES.map((route) => route.name)).toContain('PacksLayout')
  })

  it.each(ROUTES.map((route) => [route.file, route] as const))(
    '%s carries data-measure on its top-level element',
    (file, route) => {
      const source = readFileSync(join(ROUTES_DIR, file), 'utf8')
      const tag = rootTag(principalReturn(source, route.name))
      const stated = /\sdata-measure="([^"]*)"/.exec(tag)?.[1]
      expect(
        stated,
        `routes/${file}: <${route.name} /> is mounted by App.tsx and its top-level element ` +
          `carries no data-measure. Give it one of ${KINDS.join(', ')} — the measure declares a ` +
          'default so the page still renders, which is exactly why a route that says nothing ' +
          `has to fail here. The element is: ${tag.replace(/\s+/g, ' ').slice(0, 120)}`
      ).toBeDefined()
      expect(KINDS as readonly string[], `routes/${file}: data-measure="${stated}"`).toContain(
        stated
      )
      // A component is not an element: `data-measure` on `<Loading />` is a
      // prop nothing renders, and the sheet would never see it.
      expect(
        /^<[a-z]/.test(tag),
        `routes/${file}: the top-level element is a component, so the attribute reaches no DOM node`
      ).toBe(true)
    }
  )

  it('uses all three kinds, so no rule in the sheet is written for nobody', () => {
    const stated = ROUTES.map((route) => {
      const source = readFileSync(join(ROUTES_DIR, route.file), 'utf8')
      return /\sdata-measure="([^"]*)"/.exec(rootTag(principalReturn(source, route.name)))?.[1]
    })
    expect([...new Set(stated)].sort()).toEqual([...KINDS].sort())
  })

  it('is stated nowhere else under web/src, and once per route', () => {
    // `:has()` matches any descendant, so a `data-measure="form"` written on a
    // card deep inside a wide page would cap the whole page and nothing on
    // screen would say why. The attribute belongs to the route and to nothing
    // else, and this is the direction of the rule that says so.
    const carriers = new Map<string, number>()
    for (const path of everySource(SRC)) {
      const count = readFileSync(path, 'utf8').split('data-measure=').length - 1
      if (count > 0) carriers.set(relative(SRC, path).replaceAll('\\', '/'), count)
    }
    expect(Object.fromEntries([...carriers].sort())).toEqual(
      Object.fromEntries(ROUTES.map((route) => [`routes/${route.file}`, 1]).sort())
    )
  })
})
