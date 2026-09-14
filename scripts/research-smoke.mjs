// A browser smoke for Research and draft against a running desk, a running
// gateway and real public sources: it opens the launch URL, starts a run from
// a brief and the URLs given, waits for the run to settle, and records what the
// page showed — screenshots wide and narrow, the Sources, Tests and Review
// tabs, the Inspector for the first source, and the Console's Activity tab.
//
//   node scripts/research-smoke.mjs <desk-launch-log> <artifact-dir> [brief] [urls]
//
// The launch log is what `jpack-desk --print-url` wrote; `PLAYWRIGHT_CHROME`
// names a Chrome executable (`/usr/bin/google-chrome` by default). This is a
// smoke and not a certification: what it proves is that the whole path — desk,
// gateway, adapter, reader service, model, runtime — ran once against the live
// sources named, and the artifacts say what happened.
import { createRequire } from 'node:module'
import { readFileSync, writeFileSync, mkdirSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

const root = resolve(dirname(fileURLToPath(import.meta.url)), '..')
const require = createRequire(resolve(root, 'web', 'package.json'))
const { chromium } = require('playwright-core')

const [launchLog, out] = process.argv.slice(2)
if (!launchLog || !out) {
  console.error('usage: node scripts/research-smoke.mjs <desk-launch-log> <artifact-dir> [brief] [urls]')
  process.exit(2)
}
mkdirSync(out, { recursive: true })
const log = readFileSync(launchLog, 'utf8')
const url = log.match(/http:\/\/127\.0\.0\.1:\d+\/launch\?secret=[A-Za-z0-9_-]+/)?.[0]
if (!url) throw new Error('no launch URL in ' + launchLog)
const brief = process.argv[4] ?? `Preliminary screening of an applicant against the published minimum requirements of Canada's Federal Skilled Worker Program (Express Entry): skilled work experience, language ability, education, and proof of funds. Preliminary requirement screening only: do not encode invitation rounds or CRS scores, admissibility, or final approval; name those as out of scope. Use IRCC pages and the Immigration and Refugee Protection Regulations as sources.`
const urls = process.argv[5] ?? 'https://www.canada.ca/en/immigration-refugees-citizenship/services/immigrate-canada/express-entry/eligibility/federal-skilled-workers.html\nhttps://www.canada.ca/en/immigration-refugees-citizenship/services/immigrate-canada/express-entry/documents/proof-funds.html'
const browser = await chromium.launch({ executablePath: process.env.PLAYWRIGHT_CHROME ?? '/usr/bin/google-chrome', headless: true })
const page = await browser.newPage({ viewport: { width: 1440, height: 900 } })
const messages = []
page.on('console', (m) => messages.push(`[${m.type()}] ${m.text()}`))
page.on('pageerror', (e) => messages.push(`[pageerror] ${e.message}`))
await page.goto(url, { waitUntil: 'networkidle' })
await page.goto(url.replace(/\/launch.*$/, '/create-pack/research'), { waitUntil: 'networkidle' })
await page.waitForTimeout(1500)
await page.screenshot({ path: `${out}/01-idle.png`, fullPage: false })
const blocked = await page.locator('[role=status]').allTextContents()
console.log('status at idle:', blocked)
await page.getByLabel(/The decision/).fill(brief)
await page.getByLabel(/Read these first/).fill(urls)
await page.getByRole('button', { name: 'Start research' }).click()
const started = Date.now()
let last = ''
for (;;) {
  await page.waitForTimeout(5000)
  const status = (await page.locator('header [role=status], [aria-label="Conversation"] [role=status]').allTextContents()).join(' | ')
  if (status !== last) { console.log(`${Math.round((Date.now() - started) / 1000)}s: ${status}`); last = status }
  const stop = await page.getByRole('button', { name: 'Stop' }).count()
  if (stop === 0 && Date.now() - started > 15000) break
  if (Date.now() - started > 20 * 60 * 1000) { console.log('TIMEOUT'); break }
  if ((Date.now() - started) % 60000 < 5000) await page.screenshot({ path: `${out}/02-running.png` })
}
await page.screenshot({ path: `${out}/03-settled-wide.png`, fullPage: true })
for (const tab of ['Sources', 'Tests', 'Review']) {
  const trigger = page.getByRole('tab', { name: new RegExp(`^${tab}`) })
  await trigger.click()
  await page.waitForTimeout(500)
  await page.screenshot({ path: `${out}/04-${tab.toLowerCase()}.png`, fullPage: true })
  console.log(`--- ${tab} tab text ---\n` + (await page.getByRole('tabpanel').innerText()).slice(0, 6000))
}
// Inspect the first source and the Inspector pane.
await page.getByRole('tab', { name: /^Sources/ }).click()
const first = page.getByRole('button', { name: /Inspect src-1/ })
if (await first.count()) {
  await first.click()
  await page.waitForTimeout(800)
  await page.screenshot({ path: `${out}/05-inspector.png`, fullPage: true })
  console.log('--- Inspector ---\n' + (await page.locator('#desk-inspector').innerText()).slice(0, 4000))
}
// The Console's Activity tab.
await page.getByRole('button', { name: /console/i }).first().click().catch(() => {})
await page.waitForTimeout(500)
const activity = page.getByRole('tab', { name: 'Activity' })
if (await activity.count()) { await activity.click(); await page.waitForTimeout(300) }
console.log('--- Console ---\n' + (await page.locator('#desk-console').innerText().catch(() => 'no console')).slice(0, 6000))
await page.screenshot({ path: `${out}/06-console.png`, fullPage: true })
// Narrow layout.
await page.setViewportSize({ width: 390, height: 844 })
await page.waitForTimeout(800)
await page.screenshot({ path: `${out}/07-narrow-conversation.png`, fullPage: true })
const draftSwitch = page.getByRole('radio', { name: 'Draft' })
if (await draftSwitch.count()) { await draftSwitch.click(); await page.waitForTimeout(500); await page.screenshot({ path: `${out}/08-narrow-draft.png`, fullPage: true }) }
writeFileSync(`${out}/browser-console.txt`, messages.join('\n'))
console.log('page errors:', messages.filter((m) => m.startsWith('[pageerror]') || m.startsWith('[error]')).slice(0, 10))
await browser.close()
