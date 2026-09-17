import { createRequire } from 'node:module'
import { randomBytes } from 'node:crypto'
import { cp, mkdtemp, readFile, writeFile, mkdir, rm } from 'node:fs/promises'
import { spawn } from 'node:child_process'
import assert from 'node:assert/strict'
import { dirname, resolve, join } from 'node:path'
import { fileURLToPath } from 'node:url'
import { tmpdir } from 'node:os'
import { once } from 'node:events'

// JPACK_BIN=/path/to/latest/jpack node scripts/packs-workspace-check.mjs
// /path/to/built/desk /path/to/runtime/internal/graph/testdata/project
const root = resolve(dirname(fileURLToPath(import.meta.url)), '..')
const [binary, fixture] = process.argv.slice(2)
if (!binary || !fixture || !process.env.JPACK_BIN) throw new Error('Supply Desk, a graph project fixture and JPACK_BIN.')
const { chromium } = createRequire(`${root}/web/package.json`)('playwright-core')
const work = await mkdtemp(join(tmpdir(), 'jp-navigation-browser-'))
await cp(fixture, `${work}/project`, { recursive: true })
await mkdir(`${work}/config`)
const config = JSON.parse(await readFile(`${work}/project/jpack.json`, 'utf8'))
const originalFlow = JSON.parse(await readFile(`${work}/project/onboarding.graph.json`, 'utf8'))
config.packs.flows = config.packs['vendor-onboarding']
config.packs.tests = config.packs['sanctions-screening']
config.graphs.broken = { path: 'broken.json' }
const longId = 'flow-' + 'long-name-'.repeat(9) + 'end'
config.graphs[longId] = { path: 'long.json', description: 'A long description '.repeat(40) }
await writeFile(`${work}/project/long.json`, JSON.stringify({ ...originalFlow, description: 'A long explanation '.repeat(80) }))
await writeFile(`${work}/project/broken.json`, '{')
await writeFile(`${work}/project/jpack.json`, JSON.stringify(config))
const secret = randomBytes(24).toString('hex')
const server = spawn(binary, ['--dev-token', secret, '--port', '8817', '--jpack', process.env.JPACK_BIN, `${work}/project`], {
  env: { ...process.env, XDG_CONFIG_HOME: `${work}/config`, XDG_DATA_HOME: `${work}/data` }, stdio: 'ignore'
})
let launchError
server.on('error', error => { launchError = error })
const output = resolve(root, 'docs/reviews/packs-workspace')
await mkdir(output, { recursive: true })
let browser, page
try {
  let ready = false
  for (let i = 0; i < 100; i++) {
    if (launchError) throw launchError
    assert.equal(server.exitCode, null)
    try { if ((await fetch('http://127.0.0.1:8817/api/desk-config', { headers: { Authorization: `Bearer ${secret}` } })).ok) { ready = true; break } } catch {}
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
  await page.goto(`http://127.0.0.1:8817/launch?secret=${secret}`, { waitUntil: 'domcontentloaded' })
  const main = page.locator('#main')
  const header = main.locator('[data-page-header]:visible')
  await main.getByRole('searchbox', { name: 'Search packs' }).waitFor()
  assert.equal(new URL(page.url()).pathname, '/packs')
  const navigation = main.getByRole('navigation', { name: 'Packs workspace' })
  await navigation.getByRole('link', { name: 'Tests', exact: true }).click()
  await main.getByRole('heading', { name: 'All pack tests' }).waitFor()
  await navigation.getByRole('link', { name: 'Pack flows', exact: true }).click()
  await main.getByRole('link', { name: 'onboarding', exact: true }).waitFor()
  assert.equal(commands.length, 0, 'Collection navigation must not execute any suite.')
  await main.getByRole('link', { name: 'onboarding', exact: true }).click()
  await main.locator('.react-flow__node').first().waitFor()
  assert.equal(await main.locator('.react-flow__node').count(), 2)
  await main.locator('.react-flow__node[data-id="screening"]').click()
  await page.locator('#desk-inspector').getByRole('link', { name: 'Open pack' }).waitFor()
  assert.equal(await page.locator('#desk-inspector').getByRole('link', { name: 'Open pack' }).getAttribute('href'), '/packs/sanctions-screening')
  await main.locator('.react-flow__node[data-id="onboarding"]').focus()
  await page.keyboard.press('Enter')
  assert.equal(await page.locator('#desk-inspector').getByRole('link', { name: 'Open pack' }).getAttribute('href'), '/packs/vendor-onboarding')
  await main.locator('.react-flow__edge').first().focus()
  await page.keyboard.press('Space')
  await page.locator('#desk-inspector').getByRole('heading', { name: 'Connection details' }).waitFor()
  assert.equal(commands.length, 0, 'Diagram inspection must not execute any suite.')
  checks.push('Home and contextual navigation; mouse/keyboard node and edge inspection; zero implicit test calls')
  await page.screenshot({ path: `${output}/flow-desktop.png` })
  await header.getByRole('link', { name: 'Tests', exact: true }).click()
  assert.equal(commands.length, 0)
  await header.getByRole('button', { name: 'Run tests', exact: true }).click()
  await header.getByRole('button', { name: 'Run tests', exact: true }).waitFor()
  await main.getByText('Last run', { exact: true }).waitFor()
  assert.deepEqual(commands, ['experimental_test_graphs'])
  await page.screenshot({ path: `${output}/flow-tests.png` })
  // A watched edit may refresh documents, but it must not run tests again.
  await writeFile(`${work}/project/onboarding.graph.json`, JSON.stringify({ ...originalFlow, description: 'Changed after the test run.' }))
  await page.waitForTimeout(600)
  assert.equal(commands.length, 1)
  await header.getByRole('link', { name: 'Pack flows', exact: true }).click()
  await main.getByText(/Last completed run:/).waitFor()
  assert.equal(commands.length, 1)
  checks.push('Explicit flow test, cached collection result and file-change isolation')
  await page.locator('.desk-chip').click()
  await page.getByRole('menuitem', { name: 'Project files' }).click()
  await header.getByRole('heading', { name: 'Project files' }).waitFor()
  assert.equal(new URL(page.url()).pathname, '/author')
  for (const id of ['flows', 'tests']) {
    await page.goto(`http://127.0.0.1:8817/packs/${id}`)
    await header.getByRole('button', { name: 'Edit', exact: true }).waitFor()
  }
  checks.push('Project files menu and existing pack IDs named flows/tests remain reachable')
  const geometry = []
  for (const [width, height, theme, density] of [
    [1700, 1000, 'dark', 'comfortable'], [1400, 800, 'light', 'compact'], [1024, 768, 'dark', 'comfortable'],
    [600, 700, 'light', 'compact'], [390, 844, 'light', 'comfortable'], [360, 640, 'dark', 'compact']
  ]) {
    await page.setViewportSize({ width, height })
    await page.emulateMedia({ colorScheme: theme })
    if (await page.getByRole('dialog', { name: 'Inspector', exact: true }).isVisible()) await page.keyboard.press('Escape')
    await page.goto(`http://127.0.0.1:8817/graphs/${encodeURIComponent(longId)}`)
    await main.locator('[aria-label="Flow diagram"]').waitFor()
    if (await page.getByRole('dialog', { name: 'Inspector', exact: true }).isVisible()) await page.keyboard.press('Escape')
    await main.getByRole('region', { name: 'Flow diagram' }).waitFor()
    await page.evaluate(density => { document.documentElement.dataset.density = density }, density)
    const scroll = main.locator('[data-page-scroll]:visible')
    const before = await header.boundingBox()
    await scroll.evaluate(element => { element.scrollTop = element.scrollHeight })
    assert.deepEqual(await header.boundingBox(), before, 'Flow headers must stay fixed while scrolling.')
    const sizes = await page.evaluate(() => {
      const body = document.querySelector('#main [data-page-scroll]')
      return { width: innerWidth, height: innerHeight, documentWidth: document.scrollingElement.scrollWidth,
        documentHeight: document.scrollingElement.scrollHeight, bodyWidth: body.clientWidth, bodyScrollWidth: body.scrollWidth }
    })
    assert(sizes.documentWidth <= width && sizes.documentHeight === height)
    assert(sizes.bodyScrollWidth <= sizes.bodyWidth + 1)
    geometry.push({ ...sizes, theme, density })
    await main.getByRole('button', { name: /screening → onboarding/ }).click()
    await page.getByRole('heading', { name: 'Connection details' }).waitFor()
    const divider = page.getByRole('separator', { name: 'Inspector', exact: true })
    if (await divider.isVisible()) {
      for (const key of ['Home', 'End']) {
        await divider.focus(); await page.keyboard.press(key)
        const box = await header.boundingBox()
        assert(box && box.x >= 0 && box.x + box.width <= width)
      }
    }
    if (await page.getByRole('dialog', { name: 'Inspector', exact: true }).isVisible()) await page.keyboard.press('Escape')
    await scroll.evaluate(element => { element.scrollTop = 0 })
    await page.screenshot({ path: `${output}/flow-${width}-${theme}.png` })
  }
  await page.goto('http://127.0.0.1:8817/graphs/broken')
  await main.getByText(/could not|unexpected|invalid|unreadable/i).waitFor()
  assert.equal(await main.locator('.react-flow__node').count(), 0)
  const { graphs: _graphs, ...withoutFlows } = config
  await writeFile(`${work}/project/jpack.json`, JSON.stringify(withoutFlows))
  await page.goto('http://127.0.0.1:8817/graphs')
  await main.getByText('No pack flows are configured.').waitFor()
  checks.push('Six viewport/theme/density combinations; sticky headers; Inspector limits/drawers; malformed and empty flows')
  assert.equal(commands.length, 1)
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
