import { createRequire } from 'node:module'
import { randomBytes } from 'node:crypto'
import { cp, mkdtemp, readFile, writeFile, mkdir, rm } from 'node:fs/promises'
import { spawn } from 'node:child_process'
import assert from 'node:assert/strict'
import { dirname, resolve, join } from 'node:path'
import { fileURLToPath } from 'node:url'
import { tmpdir } from 'node:os'
import { once } from 'node:events'

// JPACK_BIN=/path/to/latest/jpack node scripts/packs-preview-check.mjs
// /path/to/built/desk /path/to/runtime/internal/graph/testdata/project
const root = resolve(dirname(fileURLToPath(import.meta.url)), '..')
const [binary, fixture] = process.argv.slice(2)
if (!binary || !fixture || !process.env.JPACK_BIN) throw new Error('Supply Desk, a graph project fixture and JPACK_BIN.')
const { chromium } = createRequire(`${root}/web/package.json`)('playwright-core')
const work = await mkdtemp(join(tmpdir(), 'jp-preview-browser-'))
await cp(fixture, `${work}/project`, { recursive: true })
await mkdir(`${work}/config`)
const config = JSON.parse(await readFile(`${work}/project/jpack.json`, 'utf8'))
const originalFlow = JSON.parse(await readFile(`${work}/project/onboarding.graph.json`, 'utf8'))
const question = 'May this investigation proceed when records are complete, evidence is available, and all declared requirements have been checked?'
const longDescription = (question + ' ').repeat(7)
config.packs['sanctions-screening'].description = question
await writeFile(`${work}/project/jpack.json`, JSON.stringify(config))
const secret = randomBytes(24).toString('hex')
const server = spawn(binary, ['--dev-token', secret, '--port', '8821', '--jpack', process.env.JPACK_BIN, `${work}/project`], {
  env: { ...process.env, XDG_CONFIG_HOME: `${work}/config`, XDG_DATA_HOME: `${work}/data` }, stdio: 'ignore'
})
let launchError
server.on('error', error => { launchError = error })
const output = resolve(root, 'docs/reviews/packs-preview')
await mkdir(output, { recursive: true })
let browser, page
try {
  let ready = false
  for (let i = 0; i < 100; i++) {
    if (launchError) throw launchError
    assert.equal(server.exitCode, null)
    try { if ((await fetch('http://127.0.0.1:8821/api/desk-config', { headers: { Authorization: `Bearer ${secret}` } })).ok) { ready = true; break } } catch {}
    await new Promise(resolve => setTimeout(resolve, 100))
  }
  assert(ready, 'Disposable Desk did not start.')
  browser = await chromium.launch({ ...(process.env.PLAYWRIGHT_CHROME ? { executablePath: process.env.PLAYWRIGHT_CHROME } : { channel: 'chrome' }), args: ['--no-sandbox'] })
  const context = await browser.newContext({ viewport: { width: 1700, height: 1000 }, colorScheme: 'dark' })
  page = await context.newPage()
  page.setDefaultTimeout(15000)
  const errors = [], commands = [], checks = []
  page.on('pageerror', error => errors.push(error.message))
  page.on('websocket', socket => socket.on('framesent', ({ payload }) => {
    try {
      const message = JSON.parse(String(payload))
      if (message.method === 'tools/call' && message.params?.name?.startsWith('experimental_test_')) commands.push(message.params.name)
    } catch {}
  }))
  await page.goto(`http://127.0.0.1:8821/launch?secret=${secret}`, { waitUntil: 'domcontentloaded' })
  const main = page.locator('#main')
  const header = main.locator('[data-page-header]:visible')
  const pane = page.locator('#desk-inspector:visible')
  const list = main.locator('[data-pack-list]')
  const link = id => list.locator(`a[href="/packs/${id}"]`)
  const preview = id => main.getByRole('button', { name: `Preview ${id}`, exact: true })
  await link('sanctions-screening').waitFor()
  assert.equal(await pane.count(), 0, 'Collection starts without an empty Inspector.')
  const nav = main.getByRole('navigation', { name: 'Packs workspace' })
  const create = header.getByRole('link', { name: 'Create pack', exact: true })
  const navBox = await nav.boundingBox(), createBox = await create.boundingBox()
  assert(Math.abs(navBox.y + navBox.height / 2 - createBox.y - createBox.height / 2) < 2)
  assert.equal(Math.round((await header.boundingBox()).height), 49)
  await page.screenshot({ path: `${output}/collection-dark.png` })
  await preview('sanctions-screening').click()
  await pane.getByRole('heading', { name: 'sanctions-screening', exact: true }).waitFor()
  assert.equal(Math.round((await pane.boundingBox()).width), 360)
  assert(Math.abs((await header.boundingBox()).height - (await pane.locator('.desk-pane-head').boundingBox()).height) <= 1)
  assert.equal(await pane.getByText(question, { exact: true }).count(), 1)
  assert.equal(await pane.locator('details').getAttribute('open'), null)
  await pane.getByText('Technical details', { exact: true }).click()
  assert.equal(await pane.locator('details').getAttribute('open'), '')
  await pane.getByText('Technical details', { exact: true }).click()
  await page.screenshot({ path: `${output}/preview-dark.png` })
  await link('sanctions-screening').focus()
  await page.keyboard.press('ArrowDown')
  await pane.getByRole('heading', { name: 'vendor-onboarding', exact: true }).waitFor()
  await page.keyboard.press('Escape')
  assert.equal(await pane.count(), 0)
  assert(await link('vendor-onboarding').evaluate(el => el === document.activeElement))
  await page.keyboard.press('Space')
  await pane.getByRole('heading', { name: 'vendor-onboarding', exact: true }).waitFor()
  await page.keyboard.press('Space')
  assert.equal(await pane.count(), 0)
  await preview('sanctions-screening').click()
  const divider = page.getByRole('separator', { name: 'Inspector', exact: true })
  await divider.focus(); await page.keyboard.press('End')
  assert.equal(Math.round((await pane.boundingBox()).width), 420)
  await page.keyboard.press('Home')
  assert.equal(Math.round((await pane.boundingBox()).width), 320)
  await page.keyboard.press('Escape')
  await page.waitForFunction(() => Math.round(document.querySelector('#desk-inspector').getBoundingClientRect().width) === 360)
  assert.equal(Math.round((await pane.boundingBox()).width), 360, 'Splitter Escape resets size; it must not close preview.')
  await pane.getByRole('link', { name: 'Open pack', exact: true }).click()
  await header.getByRole('button', { name: 'Edit', exact: true }).waitFor()
  assert.equal(await pane.count(), 0, 'Document retains its closed preference after opening from preview.')
  await page.getByRole('button', { name: 'Inspector', exact: true }).click()
  await page.getByRole('separator', { name: 'Inspector', exact: true }).focus()
  await page.keyboard.press('End')
  const documentWidth = Math.round((await pane.boundingBox()).width)
  assert(documentWidth > 420)
  await page.goBack()
  await pane.getByRole('heading', { name: 'sanctions-screening', exact: true }).waitFor()
  assert.equal(Math.round((await pane.boundingBox()).width), 360)
  await pane.getByRole('link', { name: 'Open pack', exact: true }).click()
  await header.getByRole('button', { name: 'Edit', exact: true }).waitFor()
  assert.equal(Math.round((await pane.boundingBox()).width), documentWidth)
  await page.goBack()
  await preview('sanctions-screening').waitFor()
  await pane.getByRole('button', { name: 'Close pack preview' }).click()
  await main.getByRole('button', { name: 'Sort packs' }).click()
  const menu = page.getByRole('menu', { name: 'Sort packs' })
  assert((await menu.boundingBox()).width < 400)
  await menu.getByRole('menuitemradio', { name: 'Pack ID: Z–A' }).click()
  assert.equal(await list.locator('a').first().getAttribute('href'), '/packs/vendor-onboarding')
  checks.push('Single-row header, separate preview/document state, full metadata, keyboard browsing, focus return, explicit sorting and preview resize limits')
  assert.equal(commands.length, 0)

  // Many rows and oversized metadata exercise virtualization, truncation and
  // pane-width responsiveness without changing the user's project.
  for (let i = 0; i < 150; i++) config.packs[`pack-${String(i).padStart(3, '0')}`] = { ...config.packs['sanctions-screening'], description: i % 2 ? longDescription : 'Short description' }
  const longId = 'pack-' + 'very-long-name-'.repeat(12) + 'end'
  config.packs[longId] = { ...config.packs['sanctions-screening'], description: longDescription }
  await writeFile(`${work}/project/jpack.json`, JSON.stringify(config))
  const geometry = []
  for (const [width, height, theme, density] of [
    [1512, 900, 'dark', 'comfortable'], [1400, 800, 'light', 'compact'], [1300, 800, 'dark', 'comfortable'],
    [1100, 800, 'light', 'comfortable'], [1024, 768, 'dark', 'compact'], [600, 700, 'light', 'compact'],
    [390, 844, 'dark', 'comfortable'], [320, 640, 'light', 'compact']
  ]) {
    await page.setViewportSize({ width, height })
    await page.emulateMedia({ colorScheme: theme })
    await page.goto('http://127.0.0.1:8821/packs')
    await link('pack-000').waitFor()
    await page.evaluate(density => { document.documentElement.dataset.density = density }, density)
    const first = link('pack-000')
    await first.focus(); await page.keyboard.press('End')
    await link('vendor-onboarding').waitFor()
    assert(await link('vendor-onboarding').evaluate(el => document.activeElement === el))
    const before = await header.boundingBox()
    await page.keyboard.press('Home')
    await first.waitFor()
    assert.deepEqual(await header.boundingBox(), before)
    await preview('pack-000').click()
    await pane.getByRole('heading', { name: 'pack-000', exact: true }).waitFor()
    const dock = await page.getByRole('separator', { name: 'Inspector', exact: true }).isVisible()
    if (dock) {
      for (const key of ['Home', 'End']) {
        await divider.focus(); await page.keyboard.press(key)
        assert((await main.boundingBox()).width >= 720)
        assert((await pane.boundingBox()).width <= 420)
      }
    }
    const sizes = await page.evaluate(() => {
      const list = document.querySelector('[data-pack-list]')
      const pane = document.querySelector('#desk-inspector .desk-inspector-slot')
      return { width: innerWidth, height: innerHeight, documentWidth: document.scrollingElement.scrollWidth,
        documentHeight: document.scrollingElement.scrollHeight, listWidth: list.clientWidth, listScrollWidth: list.scrollWidth,
        paneWidth: pane.clientWidth, paneScrollWidth: pane.scrollWidth }
    })
    assert(sizes.documentWidth <= width && sizes.documentHeight === height, JSON.stringify(sizes))
    assert(sizes.listScrollWidth <= sizes.listWidth + 1 && sizes.paneScrollWidth <= sizes.paneWidth + 1, JSON.stringify(sizes))
    geometry.push({ ...sizes, theme, density, dock })
    await page.screenshot({ path: `${output}/preview-${width}-${theme}.png` })
    await pane.getByRole('button', { name: 'Close pack preview' }).click()
    const search = main.getByRole('searchbox', { name: 'Search packs' })
    await search.fill(longId)
    await preview(longId).click()
    await pane.getByRole('heading', { name: longId, exact: true }).waitFor()
    await pane.getByRole('button', { name: 'Close pack preview' }).click()
    await search.fill('no-matching-pack')
    await main.getByRole('heading', { name: 'No matching packs' }).waitFor()
    assert.equal(await pane.count(), 0)
  }
  // A bad document still has a visible warning marker on a narrow list, and
  // its exact runtime explanation remains readable in preview.
  config.packs.broken = { path: 'missing-pack.json' }
  await writeFile(`${work}/project/jpack.json`, JSON.stringify(config))
  await page.goto('http://127.0.0.1:8821/packs')
  await preview('broken').waitFor()
  const warning = link('broken').getByRole('img')
  assert(await warning.isVisible())
  const explanation = await warning.getAttribute('aria-label')
  await preview('broken').click()
  assert((await pane.innerText()).includes(explanation))
  await pane.getByRole('button', { name: 'Close pack preview' }).click()
  await preview('pack-000').click()
  await pane.getByRole('link', { name: 'Open pack', exact: true }).click()
  await header.getByRole('button', { name: 'Edit', exact: true }).waitFor()
  assert.equal(await page.getByRole('dialog').count(), 0, 'Opening a pack from the narrow preview reveals the document.')
  checks.push('Malformed-pack warning on a narrow list and opening a document from the preview drawer')
  checks.push('Eight viewport/theme/density combinations, narrow drawers, 153 virtualized rows, long IDs/descriptions, no horizontal overflow and empty search')
  assert.equal(commands.length, 0)
  assert.deepEqual(errors, [])
  await writeFile(`${output}/verification.json`, JSON.stringify({ checks, geometry, commands, errors }, null, 2))
  console.log(JSON.stringify({ checks, configurations: geometry.length, suiteCalls: commands.length, pageErrors: errors.length }))
} catch (error) {
  if (page) { await page.screenshot({ path: `${output}/failure.png` }); console.error(await page.locator('#main').innerText()) }
  throw error
} finally {
  await browser?.close()
  if (server.exitCode === null) { const ended = once(server, 'exit'); server.kill('SIGTERM'); await ended }
  await rm(work, { recursive: true, force: true })
}
