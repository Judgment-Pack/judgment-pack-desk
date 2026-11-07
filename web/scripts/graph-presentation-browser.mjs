// Offline browser proof for #9/#10. No Desk, Runtime, provider, or external calls.
import assert from 'node:assert/strict'
import { createServer } from 'vite'
import { chromium } from 'playwright-core'
const server = await createServer({ server: { host: '127.0.0.1', port: 0, strictPort: false, proxy: {} } })
let browser
try {
  await server.listen()
  browser = await chromium.launch({ executablePath: process.env.CHROME_PATH, headless: true, args: ['--no-sandbox', '--disable-dev-shm-usage'] })
  const page = await browser.newPage({ viewport: { width: 900, height: 700 }, reducedMotion: 'reduce' })
  const errors = []
  page.on('pageerror', error => errors.push(error.message))
  const address = server.httpServer.address()
  await page.route('**/*', route => new URL(route.request().url()).hostname === '127.0.0.1' ? route.continue() : route.abort())
  await page.goto(`http://127.0.0.1:${address.port}/scripts/fixtures/graph-presentation.html`)
  await page.locator('#wide svg').waitFor()
  const dimensions = await page.evaluate(() => {
    const region = document.querySelector('#wide .diagram-scroll')
    const svg = document.querySelector('#wide svg')
    const box = document.querySelector('#wide .diagram-box')
    return { client: region?.clientWidth, scroll: region?.scrollWidth, overflow: region && getComputedStyle(region).overflowX,
      svg: svg.getBoundingClientRect().width, node: box.getBoundingClientRect().width,
      labels: document.querySelectorAll('#wide .diagram-node-id').length,
      page: document.documentElement.scrollWidth, viewport: innerWidth }
  })
  assert.equal(dimensions.svg, 2720, 'wide SVG stays at natural width')
  assert.equal(dimensions.node, 240, 'node boxes remain readable at 1:1')
  assert.equal(dimensions.labels, 11, 'ten nodes and composite are rendered')
  assert.equal(dimensions.overflow, 'auto', 'wide graph contains horizontal overflow')
  assert.ok(dimensions.scroll > dimensions.client)
  assert.ok(dimensions.page <= dimensions.viewport, 'graph does not widen the page')
  const region = page.locator('#wide [role="region"]')
  assert.equal(await region.getAttribute('tabindex'), '0')
  assert.match(await region.getAttribute('aria-label'), /presentation/)
  await region.focus()
  await page.keyboard.press('ArrowRight')
  await page.waitForFunction(() => document.querySelector('#wide .diagram-scroll').scrollLeft > 0)
  const scrollLeft = await region.evaluate(el => el.scrollLeft)
  for (const [id, expected] of [['one', 272], ['two', 544], ['fallback', 520]]) {
    assert.equal(await page.locator(`#${id} .diagram-scroll`).count(), 0)
    assert.equal(await page.locator(`#${id} svg`).evaluate(el => el.getBoundingClientRect().width), expected)
  }
  const summary = page.locator('#trace summary')
  const details = page.locator('#trace details')
  assert.match(await summary.innerText(), /Trace of screening.*3 trace entries/)
  assert.equal(await details.evaluate(el => el.open), false)
  assert.equal(await page.locator('#trace .trace-list').first().isVisible(), false)
  await summary.focus()
  await page.keyboard.press('Enter')
  assert.equal(await details.evaluate(el => el.open), true)
  assert.equal(await page.locator('#trace .trace-list').first().isVisible(), true)
  const content = await details.innerText()
  assert.ok(content.indexOf('first-rule') < content.indexOf('middle-exception'))
  assert.ok(content.indexOf('middle-exception') < content.indexOf('last-rule'))
  await page.keyboard.press('Space')
  assert.equal(await details.evaluate(el => el.open), false)
  assert.deepEqual(errors, [])
  console.log(JSON.stringify({ status: 'passed', dimensions, scrollLeft, keyboard: ['ArrowRight scroll', 'Enter open trace', 'Space close trace'], narrow: { one: 272, two: 544, fallback: 520 } }))
} finally {
  await browser?.close()
  await server.close()
}
