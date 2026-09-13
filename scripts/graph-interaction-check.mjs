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
// scripts/graph-interaction-check.mjs /path/jpack-desk /path/vendor.pack.json
// Uses localhost:8803 and a disposable project/configuration.
const root = resolve(dirname(fileURLToPath(import.meta.url)), '..')
const [binary, fixture] = process.argv.slice(2)
if (!binary || !fixture) throw new Error('Supply the built desk binary and a valid pack fixture with all seven map groups.')
const { chromium } = createRequire(`${root}/web/package.json`)('playwright-core')
const work = await mkdtemp(join(tmpdir(), 'jp-graph-fixture-'))
await mkdir(`${work}/project/packs`, { recursive: true })
await mkdir(`${work}/config`, { recursive: true })
const doc = JSON.parse(await readFile(fixture, 'utf8'))
await writeFile(`${work}/project/packs/vendor.pack.json`, JSON.stringify(doc))
const emptyDoc = JSON.parse(JSON.stringify(doc))
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
const server = spawn(binary, ['--dev-token', secret, '--port', '8803', '--jpack', process.env.JPACK_BIN ?? 'jpack', `${work}/project`], { env: { ...process.env, XDG_CONFIG_HOME: `${work}/config` }, stdio: 'ignore' })
let launchError
server.on('error', error => { launchError = error })
let browser
try {
  let ready = false
  for (let i = 0; i < 100; i++) {
    if (launchError) throw new Error('Could not start the disposable desk binary.', { cause: launchError })
    assert.equal(server.exitCode, null, 'The disposable server exited before it became ready.')
    try { if ((await fetch('http://127.0.0.1:8803/api/desk-config', { headers: { Authorization: `Bearer ${secret}` } })).ok) { ready = true; break } } catch {}
    await new Promise(resolve => setTimeout(resolve, 100))
  }
  assert(ready, 'The disposable server did not become ready on port 8803.')
  browser = await chromium.launch({ ...(process.env.PLAYWRIGHT_CHROME ? { executablePath: process.env.PLAYWRIGHT_CHROME } : { channel: 'chrome' }), args: ['--no-sandbox'] })
  const context = await browser.newContext({ viewport: { width: 1700, height: 1100 }, colorScheme: 'dark' })
  const page = await context.newPage()
  page.setDefaultTimeout(15000)
  const errors = []
  page.on('pageerror', e => errors.push(e.message))
  await page.goto(`http://127.0.0.1:8803/launch?secret=${secret}`, { waitUntil: 'domcontentloaded' })
  await page.locator('.desk').waitFor()
  await page.waitForFunction(() => sessionStorage.length > 0)
  await page.goto('http://127.0.0.1:8803/packs/vendor?view=logic&layout=map&at=/applicability')
  await page.locator('.react-flow__node[data-id="rules"]').waitFor()
  const baseline = process.argv.includes('--baseline')
  if (baseline) {
    await page.locator('.react-flow__node[data-id="rules"]').click()
    console.log(JSON.stringify({ reproduction: true, badge: await page.locator('.react-flow__attribution').count(), selected: await page.locator('.react-flow__node.selected').getAttribute('data-id'), query: new URL(page.url()).search, generalOutline: await page.getByRole('heading', { name: 'Pack outline' }).count() }))
  } else {
    assert.equal(await page.locator('.react-flow__attribution').count(), 0)
    const transform = () => page.locator('.react-flow__viewport').evaluate(e => e.style.transform)
    const original = await transform()
    const ids = ['rules', 'evidenceRequirements', 'exceptions', 'resolution', 'outcomes', 'sources', 'applicability']
    const selections = []
    for (const id of ids) {
      const node = page.locator(`.react-flow__node[data-id="${id}"]`)
      await node.click()
      await page.waitForFunction(id => document.querySelector(`.react-flow__node[data-id="${id}"]`)?.getAttribute('aria-pressed') === 'true', id)
      assert.equal(await page.locator('.react-flow__node[aria-pressed="true"]').count(), 1)
      assert.equal(await transform(), original, `selection moved canvas: ${id}`)
      const params = new URL(page.url()).searchParams
      assert(params.has('at') || params.get('group') === id, `no selection address: ${id}`)
      assert.equal(await page.getByRole('heading', { name: 'Pack outline' }).count(), 0)
      selections.push({ id, at: params.get('at'), group: params.get('group') })
    }
    const rules = page.locator('.react-flow__node[data-id="rules"]')
    await rules.focus(); await page.keyboard.press('Enter')
    await page.getByRole('heading', { name: /^Decision rules ·/ }).waitFor()
    assert.equal(new URL(page.url()).searchParams.get('group'), 'rules')
    const firstRule = page.locator('.desk-inspector [data-outline-pointer]').first()
    await firstRule.click()
    await page.waitForURL(url => url.searchParams.get('at') === '/rules/0')
    assert.equal(new URL(page.url()).searchParams.get('at'), '/rules/0')
    assert.equal(new URL(page.url()).searchParams.has('group'), false)
    assert.equal(await rules.getAttribute('aria-pressed'), 'true')
    const resolution = page.locator('.react-flow__node[data-id="resolution"]')
    await resolution.focus(); await page.keyboard.press('Space')
    await page.locator('.desk-inspector').getByRole('heading', { name: 'Result handling · 2' }).waitFor()
    assert.equal(new URL(page.url()).searchParams.get('group'), 'resolution')
    assert.equal(await page.locator('.desk-inspector [data-outline-pointer]').count(), 2)
    assert.equal(await page.locator('.desk-inspector').getByText('/resolution', { exact: true }).count(), 0)
    // A tiny pointer movement on a fixed node must not pan or lose the click.
    const box = await rules.boundingBox()
    const beforeGesture = await transform()
    await page.mouse.move(box.x + 30, box.y + 30); await page.mouse.down()
    await page.mouse.move(box.x + 32, box.y + 31); await page.mouse.up()
    await page.locator('.react-flow__node[data-id="rules"][aria-pressed="true"]').waitFor()
    assert.equal(await transform(), beforeGesture)
    assert.equal(await rules.getAttribute('aria-pressed'), 'true')
    // Canvas dragging still pans; zoom controls retain selection.
    const pane = await page.locator('.react-flow__pane').boundingBox()
    await page.mouse.move(pane.x + pane.width - 35, pane.y + 40); await page.mouse.down()
    await page.mouse.move(pane.x + pane.width - 80, pane.y + 55, { steps: 5 }); await page.mouse.up()
    assert.notEqual(await transform(), beforeGesture)
    await page.getByRole('button', { name: 'Zoom in', exact: true }).click()
    assert.equal(await rules.getAttribute('aria-pressed'), 'true')
    await page.reload()
    await page.locator('.react-flow__node[data-id="rules"][aria-pressed="true"]').waitFor()
    await page.getByRole('heading', { name: /^Decision rules ·/ }).waitFor()
    await page.setViewportSize({ width: 640, height: 900 })
    await page.getByRole('dialog', { name: 'Inspector' }).waitFor()
    assert.equal(new URL(page.url()).searchParams.get('group'), 'rules')
    await page.getByRole('button', { name: 'Close inspector', exact: true }).click()
    await page.setViewportSize({ width: 1700, height: 1100 })
    await page.emulateMedia({ colorScheme: 'light' })
    await page.goto('http://127.0.0.1:8803/packs/empty?view=logic&layout=map')
    const sources = page.locator('.react-flow__node[data-id="sources"]')
    await sources.waitFor(); await sources.click()
    await page.getByRole('heading', { name: 'Source references · 0' }).waitFor()
    assert.equal(await sources.getAttribute('aria-pressed'), 'true')
    await page.locator('.desk-inspector').getByText('None declared.', { exact: true }).waitFor()
    assert.equal(await page.locator('.react-flow__attribution').count(), 0)
    assert.deepEqual(errors, [])
    // Touch follows the same click path on the real canvas.
    const touch = await browser.newContext({ viewport: { width: 1400, height: 1000 }, hasTouch: true })
    const tap = await touch.newPage(); tap.setDefaultTimeout(15000)
    await tap.goto(`http://127.0.0.1:8803/launch?secret=${secret}`, { waitUntil: 'domcontentloaded' })
    await tap.waitForFunction(() => sessionStorage.length > 0)
    await tap.goto('http://127.0.0.1:8803/packs/vendor?view=logic&layout=map')
    await tap.locator('.react-flow__node[data-id="rules"]').tap()
    await tap.getByRole('heading', { name: /^Decision rules ·/ }).waitFor()
    assert.equal(await tap.locator('.react-flow__node[data-id="rules"]').getAttribute('aria-pressed'), 'true')
    await touch.close()
    console.log(JSON.stringify({ passed: true, selections, checks: ['badge absent', 'all seven nodes', 'mouse', 'Enter/Space', 'touch', 'single current selection', 'group-only Inspector', 'real member pointers', 'slight pointer motion', 'canvas pan', 'zoom', 'reload', 'drawer resize', 'empty group', 'light/dark'], errors }))
  }
} finally {
  if (browser) await browser.close()
  if (server.pid && server.exitCode === null && server.signalCode === null) {
    const stopped = once(server, 'exit')
    server.kill('SIGTERM')
    await stopped
  }
  await rm(work, { recursive: true, force: true })
}
