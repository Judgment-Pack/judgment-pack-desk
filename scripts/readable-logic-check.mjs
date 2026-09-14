import { createRequire } from 'node:module'
import { randomBytes } from 'node:crypto'
import { mkdtemp, cp, readFile, writeFile, mkdir, rm } from 'node:fs/promises'
import { spawn } from 'node:child_process'
import assert from 'node:assert/strict'
// Usage: node scripts/readable-logic-check.mjs <desk-binary> <project> <runtime-binary> [artifact-directory]
// The project and configuration are disposable copies; no personal session is read.
const root = new URL('..', import.meta.url).pathname.replace(/\/$/, '')
const [binary, project, runtime, artifactDirectory = '/tmp/jp-readable-screens'] = process.argv.slice(2)
if (!binary || !project || !runtime) throw new Error('Provide a desk binary, complete demo project, and runtime binary.')
const { chromium } = createRequire(`${root}/web/package.json`)('playwright-core')
const work = await mkdtemp('/tmp/jp-readable-fixture-')
await cp(project, `${work}/project`, { recursive: true })
await mkdir(`${work}/config`, { recursive: true })
await mkdir(artifactDirectory, { recursive: true })
const doc = JSON.parse(await readFile(`${root}/web/src/packs/__fixtures__/minimal.pack.json`, 'utf8'))
doc.title = 'Readable logic fixture'
doc.description = 'An authored introduction to the synthetic pack.'
doc.applicability = { op: 'fact', path: '/case/type', operator: 'in', value: ['settlement-mismatch', 'subscription-discrepancy', 'missing-approval', 'redemption-inconsistency', 'token-balance-discrepancy', 'wallet-assignment', 'wallet-reassignment', 'anomalous-nav-movement', 'reconciliation-confidence'] }
doc.evidenceRequirements = ['approval-record', 'source-document', 'custody-position', 'ledger-entry', 'counterparty-confirmation'].map(id => ({ id, description: 'Synthetic browser fixture.', required: true, kind: 'document' }))
doc.outcomes = [{ id: 'resolve', label: 'Resolve', description: 'A synthetic completed case.' }, { id: 'approve-correction', label: 'Approve correction', description: 'A synthetic case awaiting correction.' }]
doc.rules = [true, false].map((value, index) => ({ id: `rule-${index}`, description: 'Synthetic rule.', onUnknown: 'escalate', outcome: doc.outcomes[index].id,
  when: { op: 'all', conditions: [
    { op: 'fact', path: '/case/rootCause', operator: 'equals', value: 'confirmed' },
    { op: 'fact', path: '/case/correctionApplied', operator: 'equals', value },
    { op: 'evidence-present', evidenceRequirement: 'counterparty-confirmation' }
  ] } }))
doc.escalation = { triggers: ['unknown', 'no-match'], target: { kind: 'human-role', name: 'Compliance' } }
await writeFile(`${work}/project/packs/triage.pack.json`, JSON.stringify(doc))
const large = structuredClone(doc)
large.rules = Array.from({ length: 80 }, (_, i) => ({ ...structuredClone(doc.rules[i % 2]), id: `rule-${i}` }))
large.rules[0].when = { op: 'all', conditions: Array.from({ length: 12 }, (_, i) => ({ op: 'any', conditions: [
  { op: 'fact', path: `/case/long-property-${i}-${'nested'.repeat(8)}`, operator: 'equals', value: 'a long exact string '.repeat(20) },
  { op: 'not', condition: { op: 'literal', value: false } }
] })) }
await writeFile(`${work}/project/packs/large.pack.json`, JSON.stringify(large))
const config = JSON.parse(await readFile(`${work}/project/jpack.json`, 'utf8'))
config.packs.triage = { path: 'packs/triage.pack.json' }; config.packs.large = { path: 'packs/large.pack.json' }
await writeFile(`${work}/project/jpack.json`, JSON.stringify(config))
const secret = randomBytes(24).toString('hex')
const server = spawn(binary, ['--dev-token', secret, '--port', '8821', '--jpack', runtime, `${work}/project`], { env: { ...process.env, XDG_CONFIG_HOME: `${work}/config` }, stdio: 'ignore' })
let browser
const results = [], errors = []
try {
  let ready = false
  for (let i = 0; i < 100; i++) {
    try { if ((await fetch('http://127.0.0.1:8821/api/desk-config', { headers: { Authorization: `Bearer ${secret}` } })).ok) { ready = true; break } } catch {}
    await new Promise(resolve => setTimeout(resolve, 100))
  }
  assert(ready, 'isolated server started')
  browser = await chromium.launch({ ...(process.env.PLAYWRIGHT_CHROME ? { executablePath: process.env.PLAYWRIGHT_CHROME } : { channel: 'chrome' }), args: ['--no-sandbox'] })
  const context = await browser.newContext({ viewport: { width: 1700, height: 1100 }, colorScheme: 'dark' })
  const page = await context.newPage(); page.setDefaultTimeout(15000)
  page.on('pageerror', e => errors.push(e.message))
  await page.goto(`http://127.0.0.1:8821/launch?secret=${secret}`, { waitUntil: 'domcontentloaded' })
  await page.waitForFunction(() => sessionStorage.length > 0)
  await page.goto('http://127.0.0.1:8821/packs/triage?view=logic')
  await page.locator('[data-logic-pointer="/rules/1"]').waitFor()
  const main = page.locator('.desk-main [data-layout="page"]:visible')
  assert.equal(await page.getByRole('radio', { name: 'List', exact: true }).getAttribute('aria-checked'), 'true')
  assert.equal(await main.getByText('/case/correctionApplied', { exact: true }).count(), 2)
  assert.equal(await main.getByText('true', { exact: true }).count(), 1)
  assert.equal(await main.getByText('false', { exact: true }).count(), 1)
  await main.getByText('Compliance', { exact: true }).waitFor()
  const scopeValues = main.locator('[data-group="applicability"] [role="listitem"]')
  assert.deepEqual((await scopeValues.allTextContents()).map(value => JSON.parse(value)), doc.applicability.value)
  // Jump navigation clears a conflicting filter and focuses the main item.
  await page.getByRole('searchbox', { name: 'Find pack item' }).fill('correctionApplied')
  await page.getByRole('button', { name: 'Jump to', exact: true }).click()
  await page.screenshot({ path: `${artifactDirectory}/jump-to-dark.png` })
  await page.getByRole('searchbox', { name: 'Find section or item' }).fill('approval record')
  await page.getByRole('searchbox', { name: 'Find section or item' }).press('Enter')
  await page.waitForFunction(() => document.activeElement?.closest('[data-logic-pointer]')?.getAttribute('data-logic-pointer') === '/evidenceRequirements/0')
  assert.equal(await page.getByRole('dialog', { name: 'Jump to', exact: true }).count(), 0)
  assert.equal(await page.getByRole('searchbox', { name: 'Find pack item' }).inputValue(), '')
  assert.equal(await page.locator('.desk-inspector:visible').count(), 0, 'Jump to keeps Inspector closed')
  await main.locator('[data-page-scroll]').evaluate(e => { e.scrollTop = 0 })
  await main.locator('[data-page-header]').click({ position: { x: 5, y: 5 } })
  await page.screenshot({ path: `${artifactDirectory}/list-dark.png` })
  results.push('All essential values visible without inspection')
  const scrolling = main.locator('[data-page-scroll]')
  const headerY = await main.locator('[data-page-header]').evaluate(e => e.getBoundingClientRect().top)
  await scrolling.evaluate(e => { e.scrollTop = 500 })
  await page.waitForTimeout(100)
  const scrollY = await scrolling.evaluate(e => e.scrollTop)
  const toolbar = await page.getByRole('radio', { name: 'List', exact: true }).boundingBox()
  const bodyBox = await scrolling.boundingBox()
  assert(toolbar.y >= bodyBox.y - 1 && toolbar.y < bodyBox.y + 80, `Toolbar is sticky ${JSON.stringify({ toolbar, bodyBox })}`)
  assert.equal(await main.locator('[data-page-header]').evaluate(e => e.getBoundingClientRect().top), headerY)
  await page.getByRole('radio', { name: 'Map', exact: true }).click()
  await page.locator('.react-flow__node[data-id="/rules/1"]').waitFor()
  await page.waitForTimeout(400)
  assert.equal(await page.locator('.react-flow__attribution').count(), 0)
  const noOverlap = async () => {
    const positions = await page.locator('.react-flow__node').evaluateAll(nodes => nodes.map(n => ({ id: n.dataset.id, x: n.getBoundingClientRect().x, y: n.getBoundingClientRect().y, right: n.getBoundingClientRect().right, bottom: n.getBoundingClientRect().bottom, width: n.getBoundingClientRect().width })))
    for (let i = 0; i < positions.length; i++) for (let j = i + 1; j < positions.length; j++) {
      const a = positions[i], b = positions[j]
      assert(!(a.x < b.right - 1 && b.x < a.right - 1 && a.y < b.bottom - 1 && b.y < a.bottom - 1), `Overlapping nodes: ${a.id}, ${b.id}`)
    }
    return positions
  }
  await noOverlap()
  await page.screenshot({ path: `${artifactDirectory}/map-dark.png` })
  await page.getByRole('radio', { name: 'List', exact: true }).click()
  assert(Math.abs(await scrolling.evaluate(e => e.scrollTop) - scrollY) < 2, 'List scroll restored')
  await page.getByRole('radio', { name: 'Map', exact: true }).click()
  const viewport = () => page.locator('.react-flow__viewport').evaluate(e => e.style.transform)
  const beforeSearch = await viewport()
  await page.getByRole('searchbox', { name: 'Find pack item' }).fill('correctionApplied')
  assert.equal(await page.locator('.react-flow__node[data-search-match="true"]').count(), 2)
  assert.equal(await viewport(), beforeSearch, 'typing does not pan')
  await page.getByRole('searchbox', { name: 'Find pack item' }).press('Enter')
  await page.waitForFunction(() => document.activeElement?.getAttribute('data-id') === '/rules/0')
  assert.equal(new URL(page.url()).searchParams.get('at'), '/evidenceRequirements/0', 'search does not inspect')
  await page.keyboard.press('Enter')
  await page.waitForFunction(() => new URL(location.href).searchParams.get('at') === '/rules/0')
  assert.equal(await page.locator('.desk-inspector:visible').count(), 0, 'Selecting a map node keeps Inspector closed')
  await page.locator('.react-flow__node[data-id="/rules/0"]').getByRole('button', { name: 'View details: rule 0', exact: true }).click()
  await page.locator('.desk-inspector:visible').waitFor()
  assert.equal(await page.locator('.desk-inspector [data-condition-tree]').count(), 0, 'Inspector does not repeat visible conditions')
  await page.getByRole('button', { name: 'Close inspector', exact: true }).click()
  await page.setViewportSize({ width: 1480, height: 1000 })
  await page.getByRole('radio', { name: 'List', exact: true }).click()
  await page.getByRole('searchbox', { name: 'Find pack item' }).fill('')
  await main.locator('[data-page-scroll]').evaluate(e => { e.scrollTop = 0 })
  await main.getByRole('button', { name: 'View details: When this pack applies', exact: true }).click()
  const divider = page.getByRole('separator', { name: 'Inspector', exact: true })
  await divider.waitFor()
  for (const key of ['Home', 'End']) {
    await divider.press(key)
    await page.waitForTimeout(150)
    const labels = await main.locator('[data-group="evidenceRequirements"] [data-inspection-row]').evaluateAll(rows => rows.map(row => {
      const range = document.createRange(); range.selectNodeContents(row.querySelector('span > span'))
      return range.getClientRects().length
    }))
    assert(labels.every(lines => lines <= 2), `Evidence labels remain readable with Inspector ${key}`)
    assert.equal(await page.locator('.desk-inspector [data-condition-tree]').count(), 0)
    assert(await main.locator('[data-page-scroll]').evaluate(e => e.scrollWidth <= e.clientWidth + 1))
  }
  await main.locator('[data-page-header]').click({ position: { x: 5, y: 5 } })
  await page.mouse.move(20, 20)
  await page.screenshot({ path: `${artifactDirectory}/list-inspector-dark.png` })
  await page.getByRole('button', { name: 'Close inspector', exact: true }).click()
  results.push('1480px with Inspector at minimum and maximum width: readable names, structured values and no repeated conditions')
  await page.getByRole('searchbox', { name: 'Find pack item' }).fill('')
  results.push('Sticky toolbar/header, restored List position, search highlight and jump, direct keyboard inspection')
  for (const theme of ['dark', 'light']) for (const density of ['comfortable', 'compact']) for (const width of [1700, 1100, 640, 360]) {
    await page.evaluate(value => document.documentElement.setAttribute('data-density', value), density)
    await page.emulateMedia({ colorScheme: theme })
    await page.setViewportSize({ width, height: 1000 })
    await page.waitForTimeout(250)
    const close = page.getByRole('button', { name: 'Close inspector', exact: true })
    if (await page.getByRole('dialog', { name: 'Inspector', exact: true }).isVisible()) await close.click()
    await page.getByRole('radio', { name: 'List', exact: true }).click()
    await page.waitForTimeout(100)
    const overflow = await main.evaluate(e => { const body = e.querySelector('[data-page-scroll]'); return { document: document.documentElement.scrollWidth - innerWidth, main: body.scrollWidth - body.clientWidth } })
    assert(overflow.document <= 1 && overflow.main <= 1, `No horizontal overflow: ${theme} ${width}: ${JSON.stringify(overflow)}`)
    const evidenceNames = await main.locator('[data-group="evidenceRequirements"] [data-inspection-row]').evaluateAll(rows => rows.map(row => {
      const label = row.querySelector('span > span')
      const range = document.createRange(); range.selectNodeContents(label)
      return { label: label.textContent, lines: range.getClientRects().length }
    }))
    assert(evidenceNames.every(item => item.lines <= 2), `Evidence names stay readable: ${width} ${JSON.stringify(evidenceNames)}`)
    results.push(`${theme} ${density} ${width}px List contains full conditions`)
    if (width === 360 || width === 1700) await page.screenshot({ path: `${artifactDirectory}/list-${theme}-${width}-${density}.png` })
  }
  await page.evaluate(() => document.documentElement.removeAttribute('data-density'))
  await page.setViewportSize({ width: 1700, height: 1100 })
  await page.goto('http://127.0.0.1:8821/packs/triage?view=overview')
  const overview = page.getByRole('region', { name: 'Pack overview' })
  await overview.getByText(doc.description, { exact: true }).waitFor()
  assert.equal(await overview.getByText('Fallback outcome', { exact: true }).count(), 0)
  assert.equal(await overview.getByText('Handoff target', { exact: true }).count(), 0)
  await page.screenshot({ path: `${artifactDirectory}/overview-light.png` })
  results.push('Overview shows authored context and a brief summary, without repeating result configuration')
  await page.goto('http://127.0.0.1:8821/packs/large?view=logic&layout=map')
  await page.locator('.react-flow__node[data-id="/rules/79"]').waitFor()
  await page.waitForTimeout(500)
  await noOverlap()
  await page.getByRole('button', { name: 'Display', exact: true }).click()
  await page.getByLabel('Group map rules by outcome').check()
  await page.keyboard.press('Escape')
  assert.equal(await page.locator('.react-flow__node').count(), 4)
  await page.getByRole('button', { name: 'Expand rules: Resolve · 40 rules', exact: true }).click()
  await page.locator('.react-flow__node[data-id="/rules/78"]').waitFor()
  assert.equal(await page.locator('.react-flow__node').count(), 43)
  await page.waitForTimeout(300)
  await noOverlap()
  results.push('80 rules, 12 nested long conditions, grouping and expansion without overlap')
  await page.getByRole('button', { name: 'Display', exact: true }).click()
  await page.getByLabel('Show conditions', { exact: true }).uncheck()
  await page.keyboard.press('Escape')
  await page.waitForTimeout(300); await noOverlap()
  await page.reload()
  await page.locator('.react-flow__node').first().waitFor()
  assert.equal(await page.locator('.react-flow__node').count(), 4)
  await page.getByRole('button', { name: 'Jump to', exact: true }).click()
  await page.getByRole('searchbox', { name: 'Find section or item' }).fill('rule 79')
  await page.getByRole('searchbox', { name: 'Find section or item' }).press('Enter')
  await page.waitForFunction(() => document.activeElement?.getAttribute('data-id') === '/rules/79')
  assert.equal(await page.locator('.desk-inspector:visible').count(), 0, 'Grouped map jump does not open Inspector')
  results.push('Jump to searches, dismisses, restores target focus and expands grouped map targets')
  await page.getByRole('radio', { name: 'List', exact: true }).click()
  await page.evaluate(() => { document.documentElement.style.fontSize = '32px' })
  await page.getByRole('searchbox', { name: 'Find pack item' }).fill('long-property')
  await page.waitForTimeout(100)
  const textOverflow = await scrolling.evaluate(e => e.scrollWidth - e.clientWidth)
  assert(textOverflow <= 1, `200% text long condition containment: ${textOverflow}`)
  results.push('Display preferences persist; search reveals conditions; 200% text remains contained')
  assert.deepEqual(errors, [])
  await writeFile(`${artifactDirectory}/results.json`, JSON.stringify({ passed: true, results, errors }, null, 2))
  console.log(JSON.stringify({ passed: true, results, errors }))
} catch (e) {
  console.error(e.message)
  if (browser) { const pages = browser.contexts().flatMap(c => c.pages()); await pages[0]?.screenshot({ path: `${artifactDirectory}/failure.png` }) }
  throw e
} finally {
  if (browser) await browser.close()
  server.kill('SIGTERM')
  await rm(work, { recursive: true, force: true })
}
