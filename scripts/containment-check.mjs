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
 * cascade actually produced. It is the gate for that half, and CI does not run
 * it: CI has no Chrome and no runtime binary. Every merge does — see the
 * README's Tests section, beside the needle check.
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
 * 6. No page error and no console error.
 *
 * Routes × configurations: seven routes, at 1400x800 and at 640x800. Wide,
 * four configurations — the Inspector closed and open, the console closed and
 * open. Narrow, three: below 1100px the Inspector is a modal drawer whose
 * overlay owns the pointer, so the console cannot be toggled while it is open.
 * 7 x (4 + 3) = 49 rows a build.
 *
 *   node scripts/containment-check.mjs <port> <token> [label]
 *
 * Normally run through `scripts/containment-check.sh`, which builds the
 * throwaway configuration and the copied project this needs. Chrome is the
 * system one: `PLAYWRIGHT_CHROME` names an executable, and without it
 * `channel: 'chrome'` asks playwright-core for the installed browser. Nothing
 * is downloaded.
 */
import { createRequire } from 'node:module'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'

const ROOT = join(dirname(fileURLToPath(import.meta.url)), '..')
// `playwright-core` is a devDependency of `web/`, and this file is not under
// it, so the resolution is anchored at that package rather than at this path.
const { chromium } = createRequire(join(ROOT, 'web', 'package.json'))('playwright-core')

const [, , PORT, TOKEN, LABEL = 'build'] = process.argv
if (PORT === undefined || TOKEN === undefined) {
  console.error('usage: node scripts/containment-check.mjs <port> <token> [label]')
  process.exit(2)
}

const at = (path) => `http://127.0.0.1:${PORT}${path}${path.includes('?') ? '&' : '?'}token=${TOKEN}`
const PANES = ['.desk', '.desk-rail', '.desk-main', '.desk-inspector', '.desk-console']
const problems = []
const rows = []

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

const browser = await chromium.launch({
  headless: true,
  args: ['--no-sandbox'],
  ...(process.env.PLAYWRIGHT_CHROME === undefined
    ? { channel: 'chrome' }
    : { executablePath: process.env.PLAYWRIGHT_CHROME })
})
const context = await browser.newContext({
  viewport: { width: 1400, height: 800 },
  colorScheme: 'light'
})
const page = await context.newPage()
page.on('pageerror', (error) => problems.push(`pageerror: ${error.message}`))
page.on('console', (message) => {
  if (message.type() === 'error' && !/Failed to load resource/.test(message.text())) {
    problems.push(`console: ${message.text()}`)
  }
})

const settle = (ms = 900) => page.waitForTimeout(ms)

async function go(path) {
  await page.goto(at(path), { waitUntil: 'networkidle' })
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
  if (seen.scrollHeight !== seen.innerHeight)
    failures.push(`document ${seen.scrollHeight} > window ${seen.innerHeight}`)
  if (seen.deskHeight !== seen.innerHeight) failures.push(`.desk ${seen.deskHeight}`)
  if (scrolled !== 0) failures.push(`scrollY ${scrolled}`)
  if (notRelative.length > 0) failures.push(notRelative.join(', '))
  if (seen.hung.length > 0) failures.push(`${seen.hung.length} from BODY (${seen.hung[0]})`)
  rows.push({
    route,
    config,
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
 * The pack edit route, which is one of the seven and cannot be spelt without
 * knowing a pack. Packs reach the page over the relay and not over an HTTP
 * endpoint this script could ask, so it is read off the rendered rail — the
 * first pack the project actually lists. A project with no packs is a project
 * this gate cannot measure, and that is said out loud rather than quietly
 * measured as six routes.
 */
await go('/packs')
const first = await page.evaluate(() =>
  [...document.querySelectorAll('a[href*="/packs/"]')]
    .map((anchor) => new URL(anchor.href, location.origin).pathname)
    .find((path) => /^\/packs\/[^/]+$/.test(path))
)
if (first === undefined) {
  console.error('the project listed no pack, and the edit route is one of the seven')
  await browser.close()
  process.exit(2)
}

const ROUTES = ['/packs', `${first}?edit=1`, '/admin', '/graphs', '/matrix', '/author', '/help']

for (const width of [1400, 640]) {
  await page.setViewportSize({ width, height: 800 })
  for (const route of ROUTES) {
    await go(route)
    if (route.includes('edit=1')) {
      await page.waitForSelector('article', { timeout: 60000 }).catch(() => {})
      await settle(1200)
    }
    await setInspector(false)
    await setConsole(false)
    await sample(route, 'inspector closed, console closed')
    await setConsole(true)
    await sample(route, 'inspector closed, console open')
    await setConsole(false)
    await setInspector(true)
    await sample(route, 'inspector open, console closed')
    const asPane = (await page.locator('aside.desk-inspector:not([hidden])').count()) > 0
    if (asPane) {
      await setConsole(true)
      await sample(route, 'inspector open, console open')
      await setConsole(false)
    }
    await setInspector(false)
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
console.log(`\ncontainment check — ${LABEL}\n`)
console.log(line(header))
console.log(widths.map((width) => '-'.repeat(width)).join('  '))
for (const row of table) console.log(line(row))

const failed = rows.filter((row) => row.failures.length > 0)
console.log(
  `\n${LABEL}: ${rows.length} rows, ${rows.length - failed.length} contained, ${failed.length} not contained`
)
for (const row of failed) console.log(`  ${row.route} [${row.config}] ${row.viewport}: ${row.failures.join('; ')}`)
console.log(`problems: ${problems.length === 0 ? 'none' : JSON.stringify(problems, null, 1)}`)

process.exit(failed.length === 0 && problems.length === 0 ? 0 : 1)
