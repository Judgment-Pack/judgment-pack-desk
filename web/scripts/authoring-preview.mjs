import assert from 'node:assert/strict'
import { mkdir, writeFile } from 'node:fs/promises'
import { resolve } from 'node:path'
import { chromium } from 'playwright-core'
import { createServer } from 'vite'
import react from '@vitejs/plugin-react'

const output = resolve(process.argv[2] ?? '../docs/design/authoring')
const executablePath = process.env.PLAYWRIGHT_CHROMIUM_EXECUTABLE_PATH
if (!executablePath) throw new Error('Set PLAYWRIGHT_CHROMIUM_EXECUTABLE_PATH to an installed Chromium binary.')
await mkdir(output, { recursive: true })
const server = await createServer({ configFile: false, plugins: [react()], server: { host: '127.0.0.1', port: 5199, strictPort: true, watch: null }, logLevel: 'error' })
await server.listen()
const browser = await chromium.launch({ executablePath, headless: true, args: ['--no-sandbox'] })
let checks = 0
try {
  const page = await browser.newPage()
  const errors = []
  page.on('pageerror', error => errors.push(error.message))
  for (const theme of ['dark', 'light']) for (const density of ['comfortable', 'compact']) {
    for (const state of ['initial', 'active', 'review']) {
      for (const [width, height] of [[1440, 1000], [1280, 900], [1024, 800], [768, 900], [390, 844]]) {
        await page.setViewportSize({ width, height })
        await page.goto(`http://127.0.0.1:5199/authoring-preview.html?state=${state}&theme=${theme}&density=${density}`)
        await page.evaluate(() => document.fonts.ready)
        const horizontalOverflow = await page.evaluate(() => [...document.querySelectorAll('main, aside, textarea, [role="table"], header, footer')]
          .filter(element => element.clientWidth && element.scrollWidth > element.clientWidth + 2)
          .map(element => ({ tag: element.tagName, width: element.clientWidth, content: element.scrollWidth })))
        assert.deepEqual(horizontalOverflow, [], `${theme}/${density}/${state}/${width}`)
        assert.equal(await page.evaluate(() => document.documentElement.scrollWidth > innerWidth), false)
        if (width === 1440 && density === 'comfortable') await page.screenshot({ path: resolve(output, `${state}-${theme}.png`) })
        if (width === 390 && theme === 'dark' && state === 'active' && density === 'comfortable') {
          await page.getByText('Chat', { exact: true }).click()
          await page.screenshot({ path: resolve(output, 'active-mobile.png') })
        }
        checks++
      }
    }
  }
  await page.setViewportSize({ width: 1440, height: 760 })
  await page.goto('http://127.0.0.1:5199/authoring-preview.html?state=active')
  const header = page.getByRole('heading', { name: 'Expense reimbursement', exact: true })
  const before = await header.boundingBox()
  await page.evaluate(() => {
    for (const element of document.querySelectorAll('main > div, aside > div')) {
      if (element.scrollHeight > element.clientHeight) element.scrollTop = element.scrollHeight
    }
  })
  assert.equal((await header.boundingBox()).y, before.y)
  await page.getByRole('button', { name: 'Stop', exact: true }).click()
  await page.getByText('Paused', { exact: true }).first().waitFor()
  await page.getByRole('button', { name: 'Review draft', exact: true }).click()
  await page.getByRole('button', { name: 'View test results', exact: true }).click()
  assert.equal(await page.getByRole('row').count(), 8)
  await page.getByRole('button', { name: 'Create pack', exact: true }).last().click()
  await page.getByRole('status').filter({ hasText: 'no pack was created' }).waitFor()
  assert.deepEqual(errors, [])
  const verification = { layoutScenarios: checks, stickyHeader: true, stopReviewAndTestNavigation: true, browserErrors: errors }
  await writeFile(resolve(output, 'verification.json'), JSON.stringify(verification, null, 2) + '\n')
  console.log(JSON.stringify({ ...verification, output }))
} finally {
  await browser.close()
  await server.close()
}
