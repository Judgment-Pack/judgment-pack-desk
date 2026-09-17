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
// scripts/edit-transition-check.mjs /path/jpack-desk /path/vendor.pack.json
// Uses localhost:8816 and a disposable project/configuration.
const root = resolve(dirname(fileURLToPath(import.meta.url)), '..')
const [binary, fixture] = process.argv.slice(2)
if (!binary || !fixture) throw new Error('Supply the built desk binary and a valid pack fixture with all seven map groups.')
const { chromium } = createRequire(`${root}/web/package.json`)('playwright-core')
const work = await mkdtemp(join(tmpdir(), 'jp-edit-transition-'))
await mkdir(`${work}/project/packs`, { recursive: true })
await mkdir(`${work}/config`, { recursive: true })
const doc = JSON.parse(await readFile(fixture, 'utf8'))
doc.description = 'Read this pack before editing. '.repeat(120)
doc.rules = Array.from({ length: 25 }, (_, index) => ({ ...doc.rules[0], id: `rule-${index}` }))
const original = JSON.stringify(doc, null, 2)
await writeFile(`${work}/project/packs/vendor.pack.json`, original)
await writeFile(`${work}/project/jpack.json`, JSON.stringify({ configVersion: '3', packs: { vendor: { path: 'packs/vendor.pack.json' } } }))
// Explicit, disposable test credential. No credentials are read from logs,
// process arguments, or the user's configuration/session.
const secret = randomBytes(24).toString('hex')
const server = spawn(binary, ['--dev-token', secret, '--port', '8816', '--jpack', process.env.JPACK_BIN ?? 'jpack', `${work}/project`], { env: { ...process.env, XDG_CONFIG_HOME: `${work}/config`, XDG_DATA_HOME: `${work}/data` }, stdio: 'ignore' })
let launchError
server.on('error', error => { launchError = error })
let browser
try {
  let ready = false
  for (let i = 0; i < 100; i++) {
    if (launchError) throw new Error('Could not start the disposable desk binary.', { cause: launchError })
    assert.equal(server.exitCode, null, 'The disposable server exited before it became ready.')
    try { if ((await fetch('http://127.0.0.1:8816/api/desk-config', { headers: { Authorization: `Bearer ${secret}` } })).ok) { ready = true; break } } catch {}
    await new Promise(resolve => setTimeout(resolve, 100))
  }
  assert(ready, 'The disposable server did not become ready on port 8816.')
  browser = await chromium.launch({ ...(process.env.PLAYWRIGHT_CHROME ? { executablePath: process.env.PLAYWRIGHT_CHROME } : { channel: 'chrome' }), args: ['--no-sandbox'] })
  const context = await browser.newContext({ viewport: { width: 1700, height: 1100 }, colorScheme: 'dark' })
  const page = await context.newPage()
  page.setDefaultTimeout(15000)
  const errors = []
  page.on('pageerror', e => errors.push(e.message))
  await page.goto(`http://127.0.0.1:8816/launch?secret=${secret}`, { waitUntil: 'domcontentloaded' })
  await page.locator('.desk').waitFor()
  await page.waitForFunction(() => sessionStorage.length > 0)
  const base = 'http://127.0.0.1:8816/packs/vendor'
  const main = page.locator('#main')
  const header = main.locator('[data-page-header]:visible')
  const body = main.locator('[data-page-scroll]:visible')
  const save = header.getByRole('button', { name: 'Save', exact: true })
  const back = header.getByRole('button', { name: 'Back to pack', exact: true })
  const configurations = []
  const output = resolve(root, 'docs/reviews/pack-edit-transition')
  await mkdir(output, { recursive: true })
  for (const [width, height, theme, density] of [
    [1700, 1000, 'dark', 'comfortable'], [1400, 800, 'light', 'compact'],
    [1024, 768, 'dark', 'comfortable'], [600, 700, 'light', 'compact'],
    [390, 844, 'light', 'comfortable'], [360, 640, 'dark', 'compact']
  ]) {
    await page.setViewportSize({ width, height })
    await page.emulateMedia({ colorScheme: theme })
    await page.goto(`${base}?view=document`)
    await header.getByRole('button', { name: 'Edit', exact: true }).waitFor()
    await page.evaluate(density => { document.documentElement.dataset.density = density }, density)
    await body.evaluate(element => { element.scrollTop = element.scrollHeight })
    await header.getByRole('button', { name: 'Edit', exact: true }).click()
    await back.waitFor()
    assert(await back.evaluate(element => element === document.activeElement), 'Entry must place keyboard focus on a visible control.')
    const before = await header.boundingBox()
    await body.evaluate(element => { element.scrollTop = element.scrollHeight })
    await page.evaluate(() => new Promise(requestAnimationFrame))
    assert.deepEqual(await header.boundingBox(), before, 'Scrolling must not move the editor header.')
    for (const control of [back, save, header.getByRole('button', { name: 'Test draft' })]) {
      const rect = await control.boundingBox()
      assert(rect && rect.x >= 0 && rect.y >= 0 && rect.x + rect.width <= width && rect.y + rect.height < height,
        `Editor actions must remain in the viewport at ${width}px.`)
    }
    const geometry = await page.evaluate(() => ({ width: innerWidth, height: innerHeight,
      documentWidth: document.scrollingElement.scrollWidth, documentHeight: document.scrollingElement.scrollHeight,
      bodyWidth: [...document.querySelectorAll('#main [data-page-scroll]')].find(element => element.clientHeight > 0).clientWidth,
      bodyScrollWidth: [...document.querySelectorAll('#main [data-page-scroll]')].find(element => element.clientHeight > 0).scrollWidth }))
    assert(geometry.documentWidth <= width && geometry.documentHeight === height, 'The shell must contain the page.')
    assert(geometry.bodyScrollWidth <= geometry.bodyWidth + 1, 'Form content must not force horizontal scrolling.')
    assert.equal(await header.getByRole('link', { name: 'Test pack' }).count(), 0)
    await back.click()
    await page.waitForURL(url => !url.searchParams.has('edit') && url.searchParams.get('view') === 'document')
    assert(await header.getByRole('button', { name: 'Edit', exact: true }).evaluate(element => element === document.activeElement))
    await header.getByRole('button', { name: 'Edit', exact: true }).click()
    await header.getByRole('radio', { name: 'JSON', exact: true }).click()
    const bytes = main.getByLabel("The document's bytes")
    const revised = original.replace(doc.title, 'Pack with a revised title')
    await bytes.fill(revised)
    await body.evaluate(element => { element.scrollTop = element.scrollHeight })
    await back.click()
    const dialog = page.getByRole('dialog', { name: 'Save changes before leaving?' })
    await dialog.waitFor()
    assert(await dialog.getByRole('button', { name: 'Keep editing' }).evaluate(element => element === document.activeElement))
    if (width === 360) await page.screenshot({ path: `${output}/exit-narrow.png` })
    await page.keyboard.press('Escape')
    await dialog.waitFor({ state: 'hidden' })
    assert(await back.evaluate(element => element === document.activeElement))
    assert.equal(await bytes.inputValue(), revised)
    await body.evaluate(element => { element.scrollTop = 0 })
    await page.screenshot({ path: `${output}/edit-${width}-${theme}.png` })
    await back.click()
    await dialog.getByRole('button', { name: 'Discard and return' }).click()
    await page.waitForURL(url => !url.searchParams.has('edit'))
    assert.equal(await readFile(`${work}/project/packs/vendor.pack.json`, 'utf8'), original)
    configurations.push({ width, height, theme, density })
  }
  // The same header must work when a large Inspector narrows a desktop pane.
  await page.setViewportSize({ width: 1400, height: 900 })
  await page.goto(`${base}?view=document`)
  await header.getByRole('button', { name: 'Edit', exact: true }).click()
  const inspectorToggle = page.getByRole('banner').getByRole('button', { name: 'Inspector', exact: true })
  if (await inspectorToggle.getAttribute('aria-pressed') !== 'true') await inspectorToggle.click()
  const divider = page.getByRole('separator', { name: 'Inspector', exact: true })
  await divider.waitFor()
  for (const key of ['Home', 'End']) {
    await divider.focus()
    await divider.press(key)
    await page.evaluate(() => new Promise(requestAnimationFrame))
    const bounds = await header.boundingBox()
    for (const control of [back, save, header.getByRole('button', { name: 'Test draft' })]) {
      const rect = await control.boundingBox()
      assert(rect.x >= bounds.x && rect.x + rect.width <= bounds.x + bounds.width + 1,
        'Editor actions must fit at both Inspector resize limits.')
    }
  }
  await header.getByRole('button', { name: 'Test draft' }).click()
  await page.getByRole('complementary', { name: 'Test draft' }).waitFor()
  await back.click()
  assert.equal(await page.getByRole('complementary', { name: 'Test draft' }).count(), 0,
    'A draft test pane must not remain open on the reading view.')
  await inspectorToggle.click()
  // One genuine write through the chassis, with return after read-back.
  await page.setViewportSize({ width: 1400, height: 900 })
  await page.goto(`${base}?view=logic`)
  await header.getByRole('button', { name: 'Edit', exact: true }).click()
  await header.getByRole('radio', { name: 'JSON', exact: true }).click()
  const revised = original.replace(doc.title, 'Saved transition proof')
  await main.getByLabel("The document's bytes").fill(revised)
  await back.click()
  await page.getByRole('dialog').getByRole('button', { name: 'Save and return' }).click()
  await page.waitForURL(url => !url.searchParams.has('edit') && url.searchParams.get('view') === 'logic')
  assert.equal(await readFile(`${work}/project/packs/vendor.pack.json`, 'utf8'), revised)
  assert.deepEqual(errors, [])
  const results = { passed: true, configurations, checks: ['scrolled entry and pinned controls', 'visible edit and return at every width', 'focus on entry and return', 'dialog cancellation and focus restoration', 'discard never writes', 'Inspector minimum/maximum resizing and draft-pane exit', 'one verified save and return through real chassis', 'no page or form overflow'], errors }
  await writeFile(`${output}/verification.json`, JSON.stringify(results, null, 2))
  console.log(JSON.stringify(results))
} finally {
  if (browser) await browser.close()
  if (server.pid && server.exitCode === null && server.signalCode === null) {
    const stopped = once(server, 'exit'); server.kill('SIGTERM'); await stopped
  }
  await rm(work, { recursive: true, force: true })
}
