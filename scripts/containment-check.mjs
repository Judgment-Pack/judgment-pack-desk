/**
 * The cascade half of "every scroll container is a containing block".
 *
 * `web/src/ui/containingBlock.test.ts` reads the stylesheets and holds the
 * declaring rules. It is a source reader and says so: a later rule that
 * changes a pane's *computed* position by any other selector — an ancestor in
 * front of it, an id, an attribute, a nested `&`, a `:global`, an inline
 * `style` — is outside anything that parses text. Three drafts of that file
 * tried to emulate the cascade and each was defeated by a construction nobody
 * had thought of, which is the argument for measuring instead of emulating.
 *
 * So this loads a **built chassis in a real browser** and reads what the
 * cascade actually produced. CI supplies no runtime binary and no project, so
 * there is nothing for the chassis to serve; the gate is run by hand. Run
 * before every merge that touches a stylesheet. This is a convention; nothing
 * automated enforces it.
 *
 * Six things per row, and a row is contained only if all six hold:
 *
 * 1. `document.scrollingElement.scrollHeight === innerHeight` — the document
 *    has no scrollable overflow at all.
 * 2. `.desk` is exactly `innerHeight` tall — the frame is one viewport.
 * 3. `scrollY` is 0 after `window.scrollTo(0, 5000)` — the thing the report
 *    was about: the whole shell could be scrolled up out of the window.
 * 4. The computed `position` of `.desk`, `.desk-rail`, `.desk-main`,
 *    `.desk-inspector` and `.desk-console`, wherever the route renders them,
 *    is `relative`. This is the one that discriminates without a long page to
 *    grow: a pane un-positioned from any selector fails here immediately.
 * 5. No absolutely positioned element resolves its `offsetParent` to `BODY`.
 *    That is the shape of the defect exactly — nothing between the element and
 *    the root is positioned, so no pane scrolls it and no frame clips it — and
 *    it includes the skip link, which hung from BODY on every route before this
 *    and resolves to `.desk` after it.
 * 6. No page error and no console error. The collector is reset before each
 *    row, so what it holds is that row's.
 *
 * **The widths.** The widths are derived from every breakpoint the sheets
 * author, so an override scoped to a width this gate never enters cannot exist
 * by construction; a breakpoint written in a form this parser does not read is
 * the one gap, and the parser prints what it found. Every `@media` prelude
 * under `web/src` is read for `max-width` and `min-width` in `px`, `em` or
 * `rem` (converted at 16px); the sample is `{1400, 640}`, plus `N − 1` for
 * each `max-width: N` and `N` for each `min-width: N`, deduplicated and
 * descending, each at height 800. No breakpoint found at all is a broken
 * parser rather than a shell without breakpoints, and exits 2.
 *
 * **The routes.** Every route pattern `web/src/App.tsx` declares, checked
 * against the file at run time: a pattern this script does not visit exits 2,
 * so a route added later fails the gate until it is sampled. The pack and the
 * graph are read off the rendered `/packs` and `/graphs` pages, because
 * neither id can be spelt without the project.
 *
 * **The configurations.** Four where the Inspector is a column and three where
 * it is a drawer, read from `INSPECTOR_DRAWER_BELOW` in
 * `web/src/shell/useMediaQuery.ts`: under a modal drawer the overlay owns the
 * pointer, so the console cannot be toggled while the Inspector is open. The
 * intended row count is that decision — routes × widths × configurations —
 * computed before any sampling and checked against the rows afterwards, so a
 * run that lost rows to a drifted locator fails on the count instead of
 * reporting the rows it managed. Each toggle is then *observed* on the page it
 * claims to configure, and a row whose configuration did not take effect fails.
 *
 *   node scripts/containment-check.mjs <port> <token> [label] [source-root]
 *
 * Normally run through `scripts/containment-check.sh`, which builds the
 * throwaway configuration and the copied project this needs, and passes the
 * source root this reads the sheets, `App.tsx` and `playwright-core` from.
 * Chrome is the system one: `PLAYWRIGHT_CHROME` names an executable, and
 * without it `channel: 'chrome'` asks playwright-core for the installed
 * browser. Nothing is downloaded.
 */
import { createRequire } from 'node:module'
import { readFileSync, readdirSync } from 'node:fs'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'

const [, , PORT, TOKEN, LABEL = 'build', ROOT_ARG] = process.argv
if (PORT === undefined || TOKEN === undefined) {
  console.error('usage: node scripts/containment-check.mjs <port> <token> [label] [source-root]')
  process.exit(2)
}
// The source root is passed rather than derived from this file's own path, so
// a copy of this script measures the repository it was given and not the
// directory it happens to sit in.
const ROOT = ROOT_ARG ?? join(dirname(fileURLToPath(import.meta.url)), '..')
const SRC = join(ROOT, 'web', 'src')
// `playwright-core` is a devDependency of `web/`, and this file is not
// necessarily under it, so the resolution is anchored at that package.
const { chromium } = createRequire(join(ROOT, 'web', 'package.json'))('playwright-core')

const at = (path) => `http://127.0.0.1:${PORT}${path}${path.includes('?') ? '&' : '?'}token=${TOKEN}`
const PANES = ['.desk', '.desk-rail', '.desk-main', '.desk-inspector', '.desk-console']
let problems = []
const rows = []

/** Every `.css` under `web/src`, by extension and not by name. */
function sheets(directory = SRC, found = []) {
  for (const entry of readdirSync(directory, { withFileTypes: true })) {
    const path = join(directory, entry.name)
    if (entry.isDirectory()) sheets(path, found)
    else if (/\.css$/i.test(entry.name)) found.push(path)
  }
  return found
}

/**
 * Every width breakpoint the sheets author, as `{ kind, px, where, prelude }`.
 *
 * `em` and `rem` are converted at 16px, which is the initial font size and the
 * one a media query resolves against whatever the page's own font size is. A
 * breakpoint written in any other form — a custom property, a container query,
 * a `width` range with `<=` — is not read, which is why the list is printed.
 */
function breakpoints() {
  const found = []
  for (const path of sheets()) {
    const where = path.slice(SRC.length + 1)
    const text = readFileSync(path, 'utf8')
    for (const rule of text.matchAll(/@media([^{]*)\{/g)) {
      const prelude = rule[1].replace(/\s+/g, ' ').trim()
      for (const one of prelude.matchAll(/(max|min)-width\s*:\s*([\d.]+)\s*(px|r?em)/gi)) {
        const value = Number(one[2])
        const unit = one[3].toLowerCase()
        found.push({
          kind: one[1].toLowerCase(),
          px: unit === 'px' ? Math.round(value) : Math.round(value * 16),
          where,
          prelude: `@media ${prelude}`
        })
      }
    }
  }
  return found
}

const BREAKPOINTS = breakpoints()
if (BREAKPOINTS.length === 0) {
  console.error(
    'no width breakpoint was found under web/src: this parser reads `max-width` and ' +
      '`min-width` in px, em or rem out of an `@media` prelude, and a shell with none is a ' +
      'broken parser rather than a shell without breakpoints'
  )
  process.exit(2)
}

const WIDTHS = [
  ...new Set([
    1400,
    640,
    ...BREAKPOINTS.map((one) => (one.kind === 'max' ? one.px - 1 : one.px))
  ])
].sort((a, b) => b - a)

/**
 * The width below which the Inspector is a drawer, read from the constant the
 * shell subscribes to rather than repeated here.
 */
function inspectorDrawerBelow() {
  const text = readFileSync(join(SRC, 'shell', 'useMediaQuery.ts'), 'utf8')
  const match = /INSPECTOR_DRAWER_BELOW\s*=\s*'\(max-width:\s*(\d+)px\)'/.exec(text)
  if (match === null) {
    console.error(
      'INSPECTOR_DRAWER_BELOW is not a `(max-width: Npx)` in web/src/shell/useMediaQuery.ts, ' +
        'so the configurations that exist at a width cannot be decided from source'
    )
    process.exit(2)
  }
  return Number(match[1])
}
const INSPECTOR_DRAWER_AT_OR_BELOW = inspectorDrawerBelow()

/** The configurations that exist at a width, decided from that breakpoint. */
const CONFIGS = (width) =>
  width > INSPECTOR_DRAWER_AT_OR_BELOW
    ? [
        { inspector: false, console: false },
        { inspector: false, console: true },
        { inspector: true, console: false },
        { inspector: true, console: true }
      ]
    : [
        { inspector: false, console: false },
        { inspector: false, console: true },
        { inspector: true, console: false }
      ]

const named = (config) =>
  `inspector ${config.inspector ? 'open' : 'closed'}, console ${config.console ? 'open' : 'closed'}`

/**
 * Every route pattern `App.tsx` declares, with a child's relative path
 * resolved against the route it is nested in.
 */
function declaredRoutes() {
  const text = readFileSync(join(SRC, 'App.tsx'), 'utf8')
  const patterns = []
  const stack = []
  for (const raw of text.split('\n')) {
    const line = raw.trim()
    if (line.startsWith('</Route>')) {
      stack.pop()
      continue
    }
    const match = /<Route\b[^>]*\bpath="([^"]*)"/.exec(line)
    if (match === null) continue
    const path = match[1]
    let full = path
    if (!path.startsWith('/') && path !== '*') {
      if (stack.length === 0) {
        console.error(`App.tsx declares a relative route path with no parent route: ${path}`)
        process.exit(2)
      }
      full = `${stack[stack.length - 1].replace(/\/$/, '')}/${path}`
    }
    patterns.push(full)
    // A tag that does not close itself opens a nesting its children resolve
    // against.
    if (!/\/>$/.test(line)) stack.push(full)
  }
  if (patterns.length === 0) {
    console.error('no <Route path="…"> was read out of web/src/App.tsx')
    process.exit(2)
  }
  return patterns
}

const browser = await chromium.launch({
  headless: true,
  args: ['--no-sandbox'],
  ...(process.env.PLAYWRIGHT_CHROME === undefined
    ? { channel: 'chrome' }
    : { executablePath: process.env.PLAYWRIGHT_CHROME })
})
const context = await browser.newContext({
  viewport: { width: WIDTHS[0], height: 800 },
  colorScheme: 'light'
})
const page = await context.newPage()
// Short enough that a locator which no longer matches ends the route it was
// sampling rather than the run's patience.
page.setDefaultTimeout(8000)
page.on('pageerror', (error) => problems.push(`pageerror: ${error.message}`))
page.on('console', (message) => {
  if (message.type() === 'error' && !/Failed to load resource/.test(message.text())) {
    problems.push(`console: ${message.text()}`)
  }
})

/** Everything read off one rendered page, in one round trip. */
const MEASURE = (panes) => {
  const scroller = document.scrollingElement
  const desk = document.querySelector('.desk')
  const positions = {}
  for (const selector of panes) {
    const element = document.querySelector(selector)
    positions[selector] = element === null ? 'absent' : getComputedStyle(element).position
  }
  // An absolutely positioned element whose containing block is the *initial*
  // one. `offsetParent === document.body` names exactly that: nothing between
  // it and the root is positioned.
  const hung = []
  for (const element of document.querySelectorAll('*')) {
    if (getComputedStyle(element).position !== 'absolute') continue
    if (element.offsetParent !== document.body) continue
    const box = element.getBoundingClientRect()
    hung.push(
      `${element.tagName.toLowerCase()}` +
        `${element.className?.toString() ? `.${element.className.toString().trim().split(/\s+/)[0]}` : ''}` +
        ` at ${Math.round(box.left)},${Math.round(box.top)}`
    )
  }
  return {
    innerWidth,
    innerHeight,
    scrollHeight: scroller.scrollHeight,
    deskHeight: desk === null ? null : Math.round(desk.getBoundingClientRect().height),
    positions,
    hung
  }
}

const settle = (ms = 900) => page.waitForTimeout(ms)

async function go(path) {
  await page.goto(at(path), { waitUntil: 'networkidle', timeout: 45000 })
  await page.waitForSelector('.desk', { timeout: 30000 })
  await settle(1100)
}

const inspectorOpen = async () =>
  (await page.locator('aside.desk-inspector:not([hidden])').count()) > 0 ||
  (await page.locator('.desk-drawer-right').count()) > 0
const consoleOpen = async () =>
  (await page.locator('section.desk-console:not([hidden])').count()) > 0

async function setInspector(want) {
  if ((await inspectorOpen()) === want) return
  if (!want && (await page.locator('.desk-drawer-right').count()) > 0) {
    // A drawer is dismissed the way a person dismisses it.
    await page.keyboard.press('Escape')
    await settle(600)
    return
  }
  await page.click('header button[aria-label="Inspector"]')
  await settle(600)
}
async function setConsole(want) {
  if ((await consoleOpen()) !== want) {
    await page.click('header button[aria-label="Console"]')
    await settle(500)
  }
}

/** One row: measure, then try to scroll the window the way the report did. */
async function sample(route, config) {
  // Observed, not assumed. A configuration that did not take effect is a row
  // measuring some other configuration under this one's name, so it fails
  // here rather than being skipped or counted as contained.
  const seenConfig = { inspector: await inspectorOpen(), console: await consoleOpen() }
  const seen = await page.evaluate(MEASURE, PANES)
  const scrolled = await page.evaluate(() => {
    window.scrollTo(0, 5000)
    return Math.round(window.scrollY)
  })
  await page.evaluate(() => window.scrollTo(0, 0))
  const notRelative = Object.entries(seen.positions)
    .filter(([, value]) => value !== 'relative' && value !== 'absent')
    .map(([selector, value]) => `${selector}: ${value}`)
  const failures = []
  for (const which of ['inspector', 'console']) {
    if (seenConfig[which] !== config[which]) {
      failures.push(`${which} ${config[which] ? 'open' : 'closed'} was not observed`)
    }
  }
  if (seen.scrollHeight !== seen.innerHeight)
    failures.push(`document ${seen.scrollHeight} > window ${seen.innerHeight}`)
  if (seen.deskHeight !== seen.innerHeight) failures.push(`.desk ${seen.deskHeight}`)
  if (scrolled !== 0) failures.push(`scrollY ${scrolled}`)
  if (notRelative.length > 0) failures.push(notRelative.join(', '))
  if (seen.hung.length > 0) failures.push(`${seen.hung.length} from BODY (${seen.hung.join('; ')})`)
  if (problems.length > 0) failures.push(problems.join('; '))
  rows.push({
    route,
    config: named(config),
    viewport: `${seen.innerWidth}x${seen.innerHeight}`,
    document: seen.scrollHeight,
    window: seen.innerHeight,
    desk: seen.deskHeight,
    scrollY: scrolled,
    panes: notRelative.length === 0 ? 'all relative' : notRelative.join(', '),
    body: seen.hung.length,
    failures
  })
}

/**
 * The two ids this gate cannot spell without the project: a pack and a graph.
 * Both reach the page over the relay rather than over an HTTP endpoint this
 * script could ask, so both are read off the rendered rail. A project with no
 * pack, or no graph, is a project this gate cannot measure, and that is said
 * out loud rather than quietly measured as fewer routes.
 */
async function firstLink(route, pattern) {
  await go(route)
  return page.evaluate(
    ([source]) =>
      [...document.querySelectorAll('a[href]')]
        .map((anchor) => new URL(anchor.href, location.origin).pathname)
        .find((path) => new RegExp(source).test(path)),
    [pattern.source]
  )
}

const pack = await firstLink('/packs', /^\/packs\/[^/]+$/)
if (pack === undefined) {
  console.error('the project listed no pack, and four of the routes are a pack')
  await browser.close()
  process.exit(2)
}
const graph = await firstLink('/graphs', /^\/graphs\/[^/]+$/)
if (graph === undefined) {
  console.error('the project listed no graph, and one of the routes is a graph')
  await browser.close()
  process.exit(2)
}

const ROUTES = [
  '/',
  '/packs',
  `${pack}?edit=1`,
  `${pack}/evaluate`,
  `${pack}/matrix`,
  '/admin',
  '/graphs',
  graph,
  '/matrix',
  '/author',
  '/help'
]

// Every pattern the router declares is visited, so a route added later fails
// this gate until somebody samples it. `*` is the only exception and is not a
// page: it renders `<Navigate to="/" replace />`, and `/` is in the list.
const DECLARED = declaredRoutes()
const visited = ROUTES.map((route) => route.split('?')[0])
const unvisited = DECLARED.filter((pattern) => pattern !== '*').filter((pattern) => {
  const source = pattern
    .split('/')
    .map((segment) =>
      segment.startsWith(':') ? '[^/]+' : segment.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
    )
    .join('/')
  const matcher = new RegExp(`^${source}$`)
  return !visited.some((one) => matcher.test(one))
})
if (unvisited.length > 0) {
  console.error(`App.tsx declares a route this gate does not visit: ${unvisited.join(', ')}`)
  await browser.close()
  process.exit(2)
}

const INTENDED = ROUTES.length * WIDTHS.reduce((total, width) => total + CONFIGS(width).length, 0)

console.log(`\ncontainment check — ${LABEL}\n`)
console.log('breakpoints read from the sheets:')
for (const one of BREAKPOINTS) console.log(`  ${one.where}  ${one.prelude}  →  ${one.kind}-width ${one.px}px`)
console.log(`\nwidths derived from them, each at height 800: ${WIDTHS.join(', ')}`)
console.log(
  `routes: ${ROUTES.length} — ${ROUTES.join(', ')}\n` +
    `configurations: 4 above ${INSPECTOR_DRAWER_AT_OR_BELOW}px, 3 at or below it\n` +
    `intended rows: ${INTENDED}\n`
)

for (const width of WIDTHS) {
  await page.setViewportSize({ width, height: 800 })
  for (const route of ROUTES) {
    try {
      problems = []
      await go(route)
      if (route.includes('edit=1')) {
        await page.waitForSelector('article', { timeout: 60000 }).catch(() => {})
        await settle(1200)
      }
      let first = true
      for (const config of CONFIGS(width)) {
        if (!first) problems = []
        first = false
        // The console is toggled while the Inspector is shut. Below the drawer
        // breakpoint the Inspector is modal and its overlay owns the pointer,
        // so a console toggle attempted under it reaches the overlay and not
        // the button — which is a timeout, not a configuration.
        await setInspector(false)
        await setConsole(config.console)
        await setInspector(config.inspector)
        await sample(route, config)
      }
      await setInspector(false)
    } catch (error) {
      // The rows this route did not produce are the count's business: a run
      // that lost them fails below on the count rather than reporting the
      // rows it managed as the whole measurement.
      console.log(`  ${route} at ${width}x800 ended early: ${error.message.split('\n')[0]}`)
    }
  }
}

await browser.close()

const cell = (value) => String(value)
const header = ['route', 'configuration', 'viewport', 'document/window', '.desk', 'scrollY', 'panes', 'BODY', '']
const table = rows.map((row) => [
  row.route,
  row.config,
  row.viewport,
  `${row.document}/${row.window}`,
  cell(row.desk),
  cell(row.scrollY),
  row.panes,
  cell(row.body),
  row.failures.length === 0 ? 'contained' : 'NOT CONTAINED'
])
const widths = header.map((_, column) =>
  Math.max(header[column].length, ...table.map((row) => row[column].length))
)
const line = (cells) => cells.map((value, column) => value.padEnd(widths[column])).join('  ')
console.log(line(header))
console.log(widths.map((width) => '-'.repeat(width)).join('  '))
for (const row of table) console.log(line(row))

const failed = rows.filter((row) => row.failures.length > 0)
console.log(
  `\n${LABEL}: ${rows.length} rows, ${rows.length - failed.length} contained, ${failed.length} not contained`
)
for (const row of failed) console.log(`  ${row.route} [${row.config}] ${row.viewport}: ${row.failures.join('; ')}`)

if (rows.length !== INTENDED) {
  console.log(`\n${LABEL}: sampled ${rows.length} rows where ${INTENDED} were intended`)
  process.exit(1)
}
process.exit(failed.length === 0 ? 0 : 1)
