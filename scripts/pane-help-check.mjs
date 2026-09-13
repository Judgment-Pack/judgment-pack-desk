import { createRequire } from 'node:module'
import { randomBytes } from 'node:crypto'
import { mkdtemp, readFile, writeFile, mkdir, rm } from 'node:fs/promises'
import { spawn } from 'node:child_process'
import assert from 'node:assert/strict'
import { dirname, resolve, join } from 'node:path'
import { fileURLToPath } from 'node:url'
import { tmpdir } from 'node:os'
import { once } from 'node:events'
// Usage: JPACK_BIN=/path/jpack PLAYWRIGHT_CHROME=/path/chrome node
// scripts/pane-help-check.mjs /path/jpack-desk /path/vendor.pack.json
// Uses localhost:8804 and a disposable project/configuration.
const root = resolve(dirname(fileURLToPath(import.meta.url)), '..')
const [binary, fixture] = process.argv.slice(2)
if (!binary || !fixture) throw new Error('Supply the built desk binary and a valid pack fixture with all seven map groups.')
const { chromium } = createRequire(`${root}/web/package.json`)('playwright-core')
const work = await mkdtemp(join(tmpdir(), 'jp-pane-help-'))
await mkdir(`${work}/project/packs`, { recursive: true })
await mkdir(`${work}/config`, { recursive: true })
const doc = JSON.parse(await readFile(fixture, 'utf8'))
doc.decision.question = 'Does this case meet the conditions for approval, given its evidence and any special cases? '.repeat(60)
doc.description = 'An author description remains readable prose with its exact original content. '.repeat(800)
doc.rules = Array.from({ length: 80 }, (_, index) => ({ ...doc.rules[0], id: `example-rule-${index}`, description: `Decision rule ${index + 1}: ${doc.rules[0].description}` }))
await writeFile(`${work}/project/packs/vendor.pack.json`, JSON.stringify(doc))
const emptyDoc = JSON.parse(JSON.stringify(doc))
emptyDoc.title = doc.title.repeat(20)
emptyDoc.sources = []
function removeSourceRefs(value) { if (!value || typeof value !== 'object') return; delete value.sourceRefs; Object.values(value).forEach(removeSourceRefs) }
removeSourceRefs(emptyDoc)
await writeFile(`${work}/project/packs/empty.pack.json`, JSON.stringify(emptyDoc))
const config = { configVersion: '3', packs: { vendor: { path: 'packs/vendor.pack.json' } } }
config.packs.empty = { path: 'packs/empty.pack.json' }
await writeFile(`${work}/project/jpack.json`, JSON.stringify(config))
// Explicit, disposable test credential. No credentials are read from logs,
// process arguments, or the user's configuration/session.
const secret = randomBytes(24).toString('hex')
const server = spawn(binary, ['--dev-token', secret, '--port', '8804', '--jpack', process.env.JPACK_BIN ?? 'jpack', `${work}/project`], { env: { ...process.env, XDG_CONFIG_HOME: `${work}/config` }, stdio: 'ignore' })
let launchError
server.on('error', error => { launchError = error })
let browser
try {
  let ready = false
  for (let i = 0; i < 100; i++) {
    if (launchError) throw new Error('Could not start the disposable desk binary.', { cause: launchError })
    assert.equal(server.exitCode, null, 'The disposable server exited before it became ready.')
    try { if ((await fetch('http://127.0.0.1:8804/api/desk-config', { headers: { Authorization: `Bearer ${secret}` } })).ok) { ready = true; break } } catch {}
    await new Promise(resolve => setTimeout(resolve, 100))
  }
  assert(ready, 'The disposable server did not become ready on port 8804.')
  browser = await chromium.launch({ ...(process.env.PLAYWRIGHT_CHROME ? { executablePath: process.env.PLAYWRIGHT_CHROME } : { channel: 'chrome' }), args: ['--no-sandbox'] })
  const context = await browser.newContext({ viewport: { width: 1700, height: 1100 }, colorScheme: 'dark' })
  const page = await context.newPage()
  page.setDefaultTimeout(15000)
  const errors = []
  page.on('pageerror', e => errors.push(e.message))
  await page.goto(`http://127.0.0.1:8804/launch?secret=${secret}`, { waitUntil: 'domcontentloaded' })
  await page.locator('.desk').waitFor()
  await page.waitForFunction(() => sessionStorage.length > 0)
  const base = 'http://127.0.0.1:8804/packs/vendor'
  const header = page.locator('#main [data-page-header]:visible')
  const mainBody = page.locator('#main [data-page-scroll]:visible')
  const pane = page.locator('#desk-inspector')
  const paneBody = pane.locator('[data-pane-scroll][data-state="active"]')
  const box = locator => locator.evaluate(element => {
    const rect = element.getBoundingClientRect()
    return { x: rect.x, y: rect.y, width: rect.width, height: rect.height }
  })
  const closeEnough = (actual, expected, label) => assert(Math.abs(actual - expected) < 2, `${label}: ${actual} vs ${expected}`)
  const scroll = async locator => {
    const moved = await locator.evaluate(element => {
      element.scrollTop = element.scrollHeight
      return element.scrollTop
    })
    assert(moved > 100, 'The scenario did not exercise scrolling.')
    await page.evaluate(() => new Promise(requestAnimationFrame))
  }
  const assertContained = async () => {
    const geometry = await page.evaluate(() => ({ height: innerHeight, width: innerWidth,
      pageHeight: document.scrollingElement.scrollHeight, pageWidth: document.scrollingElement.scrollWidth,
      mainScroll: document.querySelector('.desk-main').scrollTop }))
    assert.equal(geometry.pageHeight, geometry.height)
    assert(geometry.pageWidth <= geometry.width)
    assert.equal(geometry.mainScroll, 0)
  }
  // A long document and long Inspector prose exercise both scroll owners.
  await page.goto(`${base}?view=document&at=/description`)
  await pane.getByText('An author description remains readable prose', { exact: false }).first().waitFor()
  const headingBefore = await box(header)
  const inspectorBefore = await box(pane.locator('.desk-pane-head'))
  const tabsBefore = await box(pane.getByRole('tablist').first())
  assert((await box(mainBody)).y >= headingBefore.y + headingBefore.height - 1)
  assert((await box(paneBody)).y >= tabsBefore.y + tabsBefore.height - 1)
  await scroll(mainBody)
  closeEnough((await box(header)).y, headingBefore.y, 'main header stays pinned')
  assert.equal(await paneBody.evaluate(element => element.scrollTop), 0)
  await scroll(paneBody)
  closeEnough((await box(pane.locator('.desk-pane-head'))).y, inspectorBefore.y, 'Inspector header stays pinned')
  closeEnough((await box(pane.getByRole('tablist').first())).y, tabsBefore.y, 'Inspector tabs stay pinned')
  await assertContained()

  // Short headers and full questions coexist; expanding prose changes only the body.
  await page.goto(base)
  await page.getByRole('button', { name: 'Read full question' }).waitFor()
  const height = (await box(header)).height
  await page.getByRole('button', { name: 'Read full question' }).click()
  closeEnough((await box(header)).height, height, 'expanding a question does not enlarge the header')
  assert.equal(await page.locator('[data-expanded="true"]').textContent(), doc.decision.question)
  await scroll(mainBody)
  closeEnough((await box(header)).y, headingBefore.y, 'overview header stays pinned')

  // Group details remain scrollable without scrolling the Inspector's navigation.
  await page.goto(`${base}?view=logic&layout=map&group=rules`)
  await pane.getByRole('heading', { name: 'Decision rules · 80' }).waitFor()
  await scroll(paneBody)
  await pane.locator('[data-outline-pointer="/rules/79"]').click()
  await page.waitForURL(url => url.searchParams.get('at') === '/rules/79')
  await page.waitForFunction(() => document.querySelector('#desk-inspector [data-pane-scroll][data-state="active"]').scrollTop === 0)
  await pane.getByRole('button', { name: 'About if this condition is unknown' }).click()
  const help = page.getByRole('dialog', { name: 'If this condition is unknown' })
  await help.waitFor()
  assert((await help.textContent()).includes('Handoff settings separately determine'))
  await page.keyboard.press('Escape')
  await page.waitForFunction(() => document.activeElement.getAttribute('aria-label') === 'About if this condition is unknown')
  assert.equal(await page.getByRole('dialog', { name: 'If this condition is unknown' }).count(), 0)
  assert.equal(await page.evaluate(() => document.activeElement.getAttribute('aria-label')), 'About if this condition is unknown')
  await pane.getByText('Technical details', { exact: true }).click()
  assert.equal(await pane.getByText('/rules/79', { exact: true }).count(), 1)

  // The same pinned-header contract survives light/dark and pane-to-drawer changes.
  const configurations = []
  for (const [width, height] of [[1400, 800], [1200, 600], [831, 800], [479, 800], [1400, 500]]) {
    await page.setViewportSize({ width, height })
    await page.emulateMedia({ colorScheme: width === 1200 || width === 479 ? 'light' : 'dark' })
    await page.goto(`${base}?view=document&at=/description`)
    await pane.getByText('An author description remains readable prose', { exact: false }).first().waitFor()
    const top = (await box(pane.locator('.desk-pane-head'))).y
    const tabTop = (await box(pane.getByRole('tablist').first())).y
    await scroll(paneBody)
    closeEnough((await box(pane.locator('.desk-pane-head'))).y, top, 'resized Inspector header')
    closeEnough((await box(pane.getByRole('tablist').first())).y, tabTop, 'resized Inspector tabs')
    await assertContained()
    if (width < 1100) await page.getByRole('button', { name: 'Close inspector', exact: true }).click()
    await mainBody.evaluate(element => { element.scrollTop = 0 })
    const pinned = (await box(header)).y
    await scroll(mainBody)
    closeEnough((await box(header)).y, pinned, 'resized main header')
    await assertContained()
    configurations.push({ width, height })
  }
  // Document links land below the pinned header, not behind it.
  await page.setViewportSize({ width: 1700, height: 1100 })
  await page.goto(`${base}?view=document#%2Frules%2F79`)
  const lastRule = page.locator('#main [data-pointer="/rules/79"]')
  await lastRule.waitFor()
  await page.waitForFunction(() => document.activeElement?.getAttribute('data-pointer') === '/rules/79')
  assert((await box(lastRule)).y >= (await box(mainBody)).y - 1)
  await assertContained()
  // Divider extremes and the bottom console keep the same independent scroll contract.
  const divider = page.getByRole('separator', { name: 'Inspector', exact: true })
  const originalWidth = Number(await divider.getAttribute('aria-valuenow'))
  for (const [key, bound] of [['Home', 'aria-valuemin'], ['End', 'aria-valuemax']]) {
    await divider.focus(); await page.keyboard.press(key)
    await page.waitForFunction(bound => {
      const element = document.querySelector('[role="separator"][aria-label="Inspector"]')
      return element.getAttribute('aria-valuenow') === element.getAttribute(bound)
    }, bound)
    await assertContained()
  }
  await divider.dblclick()
  await page.waitForFunction(width => Number(document.querySelector('[role="separator"][aria-label="Inspector"]').getAttribute('aria-valuenow')) === width, originalWidth)
  await page.getByRole('button', { name: 'Expand console', exact: true }).click()
  const consoleHeader = (await box(header)).y
  await scroll(mainBody)
  closeEnough((await box(header)).y, consoleHeader, 'header with bottom console open')
  await assertContained()
  await page.getByRole('button', { name: 'Collapse console', exact: true }).click()
  await page.screenshot({ path: '/tmp/jp-pane-help-desktop.png' })
  await page.goto('http://127.0.0.1:8804/packs/empty')
  await page.getByRole('button', { name: 'More pack actions' }).click()
  assert((await page.getByRole('dialog', { name: 'Pack details' }).textContent()).includes(emptyDoc.title))
  assert((await box(header)).height < 150, 'A long title must not consume the pane.')
  await page.keyboard.press('Escape')
  await assertContained()
  await page.goto('http://127.0.0.1:8804/help')
  await pane.locator('.desk-pane-empty').waitFor()
  assert((await box(pane.locator('.desk-pane-empty'))).y < (await box(pane)).y + 150, 'Empty-state help stays near the pane header.')
  assert.deepEqual(errors, [])
  console.log(JSON.stringify({ passed: true, configurations, checks: ['independent scroll', 'pinned main and Inspector headers', 'pinned Inspector tabs', 'long question expansion', 'group navigation and scroll reset', 'help popover and Escape focus', 'exact technical pointer', 'light/dark', 'short viewport', 'drawer', 'deep link visibility', 'divider minimum/maximum/reset', 'bottom console', 'long title', 'empty Inspector'], errors }))
} finally {
  if (browser) await browser.close()
  if (server.pid && server.exitCode === null && server.signalCode === null) {
    const stopped = once(server, 'exit'); server.kill('SIGTERM'); await stopped
  }
  await rm(work, { recursive: true, force: true })
}
