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
// scripts/inspection-controls-check.mjs /path/jpack-desk /path/vendor.pack.json
// Uses localhost:8805 and a disposable project/configuration.
const root = resolve(dirname(fileURLToPath(import.meta.url)), '..')
const [binary, fixture] = process.argv.slice(2)
if (!binary || !fixture) throw new Error('Supply the built desk binary and a valid pack fixture with all seven map groups.')
const { chromium } = createRequire(`${root}/web/package.json`)('playwright-core')
const work = await mkdtemp(join(tmpdir(), 'jp-inspection-controls-'))
await mkdir(`${work}/project/packs`, { recursive: true })
await mkdir(`${work}/config`, { recursive: true })
const doc = JSON.parse(await readFile(fixture, 'utf8'))
doc.description = 'An author description remains readable prose with its exact original content. '.repeat(800)
doc.rules = Array.from({ length: 80 }, (_, index) => ({ ...doc.rules[0], id: `example-rule-${index}`, description: `Decision rule ${index + 1}: ${doc.rules[0].description}` }))
await writeFile(`${work}/project/packs/vendor.pack.json`, JSON.stringify(doc))
const emptyDoc = JSON.parse(JSON.stringify(doc))
emptyDoc.title = doc.title.repeat(20)
emptyDoc.sources = []
emptyDoc.outcomes = [...emptyDoc.outcomes, ...Array.from({ length: 4 }, (_, index) => ({ id: `additional-${index}`, label: `Additional outcome ${index}` }))]
emptyDoc.outcomes[0].label = 'A long outcome label with an-unbroken-reference-that-must-wrap-within-the-content-pane '.repeat(5).trim()
function removeSourceRefs(value) { if (!value || typeof value !== 'object') return; delete value.sourceRefs; Object.values(value).forEach(removeSourceRefs) }
removeSourceRefs(emptyDoc)
await writeFile(`${work}/project/packs/empty.pack.json`, JSON.stringify(emptyDoc))
const config = { configVersion: '3', packs: { vendor: { path: 'packs/vendor.pack.json' } } }
config.packs.empty = { path: 'packs/empty.pack.json' }
await writeFile(`${work}/project/jpack.json`, JSON.stringify(config))
// Explicit, disposable test credential. No credentials are read from logs,
// process arguments, or the user's configuration/session.
const secret = randomBytes(24).toString('hex')
const server = spawn(binary, ['--dev-token', secret, '--port', '8805', '--jpack', process.env.JPACK_BIN ?? 'jpack', `${work}/project`], { env: { ...process.env, XDG_CONFIG_HOME: `${work}/config` }, stdio: 'ignore' })
let launchError
server.on('error', error => { launchError = error })
let browser
try {
  let ready = false
  for (let i = 0; i < 100; i++) {
    if (launchError) throw new Error('Could not start the disposable desk binary.', { cause: launchError })
    assert.equal(server.exitCode, null, 'The disposable server exited before it became ready.')
    try { if ((await fetch('http://127.0.0.1:8805/api/desk-config', { headers: { Authorization: `Bearer ${secret}` } })).ok) { ready = true; break } } catch {}
    await new Promise(resolve => setTimeout(resolve, 100))
  }
  assert(ready, 'The disposable server did not become ready on port 8805.')
  browser = await chromium.launch({ ...(process.env.PLAYWRIGHT_CHROME ? { executablePath: process.env.PLAYWRIGHT_CHROME } : { channel: 'chrome' }), args: ['--no-sandbox'] })
  const context = await browser.newContext({ viewport: { width: 1700, height: 1100 }, colorScheme: 'dark' })
  const page = await context.newPage()
  page.setDefaultTimeout(15000)
  const errors = []
  page.on('pageerror', e => errors.push(e.message))
  await page.goto(`http://127.0.0.1:8805/launch?secret=${secret}`, { waitUntil: 'domcontentloaded' })
  await page.locator('.desk').waitFor()
  await page.waitForFunction(() => sessionStorage.length > 0)
  const base = 'http://127.0.0.1:8805/packs/vendor'
  const mainBody = page.locator('#main [data-page-scroll]:visible')
  const pane = page.locator('#desk-inspector')
  const paneBody = pane.locator('[data-pane-scroll][data-state="active"]')
  const box = locator => locator.evaluate(element => {
    const rect = element.getBoundingClientRect()
    return { x: rect.x, y: rect.y, width: rect.width, height: rect.height }
  })
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
  const overview = page.getByRole('region', { name: 'Pack overview' })
  const seenTools = []
  page.on('request', request => {
    if (request.method() !== 'POST') return
    try { const body = request.postDataJSON(); if (body?.method === 'tools/call') seenTools.push(body.params.name) } catch {}
  })
  await page.goto(base)
  await overview.waitFor()
  const scenarios = [
    ['View conditions', '/applicability'],
    ['View evidence needed', '/evidenceRequirements'],
    [`View outcome: ${doc.outcomes[0].label}`, '/outcomes/0'],
    [`View outcome: ${doc.outcomes[1].label}`, '/outcomes/1'],
    ['View source references', '/sources']
  ]
  for (const [index, [name, pointer]] of scenarios.entries()) {
    const control = overview.getByRole('button', { name, exact: true })
    const before = await box(control)
    const color = await control.evaluate(element => getComputedStyle(element).color)
    await control.hover()
    const after = await box(control)
    assert.deepEqual(after, before, 'Hover must not move or resize an inspection row.')
    assert.equal(await control.evaluate(element => getComputedStyle(element).textDecorationLine), 'none')
    assert.equal(await control.evaluate(element => getComputedStyle(element).color), color)
    if (index % 2) { await control.focus(); await page.keyboard.press('Enter') } else await control.click()
    await page.waitForURL(url => url.searchParams.get('at') === pointer)
    await control.and(overview.locator('[aria-current="true"]')).waitFor()
    assert.equal(await overview.locator('[aria-current="true"]').count(), 1)
    assert.equal(await control.getAttribute('aria-current'), 'true')
    assert.equal(await control.evaluate(element => getComputedStyle(element).color), color)
    assert.deepEqual(await box(control), before, 'Selection must not change the row geometry.')
  }
  assert.equal(await overview.getByRole('link', { name: 'View logic', exact: true }).getAttribute('href'), '/packs/vendor?view=logic')
  const description = overview.locator('summary').filter({ hasText: 'Author description' })
  await description.focus(); await page.keyboard.press('Space')
  assert.equal(await description.evaluate(element => element.parentElement.open), true)
  await description.press('Space')
  assert.equal(await description.evaluate(element => element.parentElement.open), false)
  await assertContained()
  await mainBody.evaluate(element => { element.scrollTop = 0 })
  await overview.getByRole('heading', { name: 'What this pack needs' }).click()
  await page.mouse.move(0, 0)
  await page.screenshot({ path: '/tmp/jp-controls-overview-dark.png' })

  // Every Inspector list uses the same control, including deep-linked groups.
  await page.goto(`${base}?view=logic&layout=map&group=rules`)
  await pane.getByRole('heading', { name: 'Decision rules · 80' }).waitFor()
  await scroll(paneBody)
  const last = pane.locator('[data-outline-pointer="/rules/79"]')
  assert.equal(await last.getAttribute('data-inspection-row'), 'true')
  await last.focus(); await page.keyboard.press('Space')
  await page.waitForURL(url => url.searchParams.get('at') === '/rules/79')
  await page.waitForFunction(() => document.querySelector('#desk-inspector [data-pane-scroll][data-state="active"]').scrollTop === 0)
  await pane.locator('summary').filter({ hasText: 'Technical details' }).click()
  assert.equal(await pane.getByText('/rules/79', { exact: true }).count(), 1)
  await assertContained()

  const configurations = []
  for (const [width, height, theme, density] of [[1400, 800, 'dark', 'comfortable'], [1200, 600, 'light', 'compact'], [831, 800, 'dark', 'comfortable'], [479, 800, 'light', 'compact'], [360, 640, 'dark', 'comfortable']]) {
    await page.setViewportSize({ width, height })
    await page.emulateMedia({ colorScheme: theme })
    await page.goto('http://127.0.0.1:8805/packs/empty')
    await overview.waitFor()
    await page.evaluate(density => { document.documentElement.dataset.density = density }, density)
    if (width < 1100 && await page.getByRole('button', { name: 'Close inspector', exact: true }).isVisible()) await page.getByRole('button', { name: 'Close inspector', exact: true }).click()
    const long = overview.getByRole('button', { name: `View outcome: ${emptyDoc.outcomes[0].label}`, exact: true })
    await long.scrollIntoViewIfNeeded()
    const fits = await long.evaluate(element => {
      const rect = element.getBoundingClientRect()
      return [...element.querySelectorAll('span')].every(child => { const bounds = child.getBoundingClientRect(); return bounds.left >= rect.left - 1 && bounds.right <= rect.right + 1 }) && rect.right <= innerWidth && rect.left >= 0
    })
    assert(fits, `Long labels must wrap within ${width}px.`)
    await long.click()
    await page.waitForURL(url => url.searchParams.get('at') === '/outcomes/0')
    await pane.getByRole('heading', { name: emptyDoc.outcomes[0].label, exact: true }).waitFor()
    if (width < 1100) await page.getByRole('button', { name: 'Close inspector', exact: true }).click()
    await overview.getByRole('button', { name: `View all ${emptyDoc.outcomes.length} outcomes`, exact: true }).click()
    await pane.getByText('Additional outcome 3', { exact: true }).waitFor()
    if (width < 1100) await page.getByRole('button', { name: 'Close inspector', exact: true }).click()
    await overview.getByRole('button', { name: 'View source references', exact: true }).click()
    await pane.getByText('None declared.', { exact: true }).waitFor()
    assert.equal(await pane.getByText('None declared.', { exact: true }).count(), 1)
    if (width < 1100) await page.getByRole('button', { name: 'Close inspector', exact: true }).click()
    await assertContained()
    configurations.push({ width, height, theme, density })
    if (width === 479) await page.screenshot({ path: '/tmp/jp-controls-overview-narrow.png' })
  }
  await page.setViewportSize({ width: 1400, height: 800 })
  await page.emulateMedia({ colorScheme: 'light' })
  await page.goto(base)
  await overview.waitFor()
  await page.screenshot({ path: '/tmp/jp-controls-overview-light.png' })
  assert(seenTools.every(name => !/evaluate|write|save|author/.test(name)), 'Reading controls must not execute a pack or write content.')
  assert.deepEqual(JSON.parse(await readFile(`${work}/project/packs/vendor.pack.json`, 'utf8')), doc)
  assert.deepEqual(JSON.parse(await readFile(`${work}/project/packs/empty.pack.json`, 'utf8')), emptyDoc)
  assert.deepEqual(errors, [])
  console.log(JSON.stringify({ passed: true, configurations, checks: ['mouse and keyboard inspection', 'exact pointers', 'stable hover and selected geometry', 'neutral text and no link underline', 'native disclosure keyboard behavior', 'Inspector group rows and scroll reset', 'long labels', 'many outcomes', 'empty groups', 'light/dark and both densities', 'drawer', 'no evaluations or document writes'], errors }))
} finally {
  if (browser) await browser.close()
  if (server.pid && server.exitCode === null && server.signalCode === null) {
    const stopped = once(server, 'exit'); server.kill('SIGTERM'); await stopped
  }
  await rm(work, { recursive: true, force: true })
}
