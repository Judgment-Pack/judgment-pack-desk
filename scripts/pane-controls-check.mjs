/** Browser verification of pointer/keyboard paint and shared shell toggle states.
 * Usage: node scripts/pane-controls-check.mjs <preview-launch-log> <artifact-dir>
 * Point the preview at a throwaway pack project; no pack content is modified.
 */
import { createRequire } from 'node:module'
import { readFile, writeFile, mkdir } from 'node:fs/promises'
import { resolve } from 'node:path'
const { chromium } = createRequire(new URL('../web/package.json', import.meta.url))('playwright-core')
const [log, artifactDir] = process.argv.slice(2)
if (!log || !artifactDir) throw new Error('Expected preview launch log and artifact directory')
const launch = (await readFile(log, 'utf8')).match(/http:\/\/[^\s]+\/launch\?secret=[^\s]+/)?.[0]
if (!launch) throw new Error('Preview launch URL unavailable')
const origin = new URL(launch).origin, secret = new URL(launch).searchParams.get('secret')
const browser = await chromium.launch({ executablePath: process.env.PLAYWRIGHT_CHROME, headless: true, args: ['--no-sandbox'] })
const results = [], errors = []
await mkdir(artifactDir, { recursive: true })
const check = (name, pass, detail) => results.push({ name, pass: !!pass, detail })
async function paint(p) {
  return p.getByRole('separator', { name: 'Inspector', exact: true }).evaluate(e => {
    const s = getComputedStyle(e), line = getComputedStyle(e, '::after'), root = getComputedStyle(document.documentElement)
    return { color: line.backgroundColor, width: line.width, outline: s.outlineStyle, shadow: s.boxShadow,
      focused: document.activeElement === e, dragging: e.hasAttribute('data-dragging'),
      accent: root.getPropertyValue('--accent').trim(), cursor: document.body.style.cursor }
  })
}
const idle = s => s.color === 'rgba(0, 0, 0, 0)' && s.outline === 'none' && s.shadow === 'none' && !s.dragging && !s.cursor
async function buttonPaint(button) {
  return button.evaluate(e => {
    const s = getComputedStyle(e)
    return { background: s.backgroundColor, color: s.color, border: s.borderColor }
  })
}
try {
 for (const theme of ['dark', 'light']) for (const density of ['comfortable', 'compact']) {
  const p = await browser.newPage({ viewport: { width: 1440, height: 900 }, colorScheme: theme })
  p.on('pageerror', e => errors.push(e.message.replaceAll(secret, '[redacted]')))
  await p.goto(launch)
  await p.locator('a[href^="/packs/"]').first().waitFor()
  const path = await p.locator('a[href^="/packs/"]').evaluateAll(as => as.map(a => a.getAttribute('href')).find(h => /^\/packs\/[^/?]+$/.test(h)))
  await p.goto(origin + path + '?view=logic&layout=list')
  await p.locator('[data-logic-pointer]').first().waitFor()
  await p.evaluate(d => { document.documentElement.dataset.density = d }, density)
  await p.locator('[data-logic-pointer]').first().click()
  const d = p.getByRole('separator', { name: 'Inspector', exact: true })
  await d.waitFor()
  const tag = theme + '/' + density
  await p.mouse.move(400, 90)
  check(tag + ': idle separator has no interaction paint', idle(await paint(p)), await paint(p))
  check(tag + ': connection is present only in footer', !(await p.getByRole('banner').innerText()).includes('connected') && (await p.getByRole('contentinfo').innerText()).includes('connected to'))
  const input = p.getByRole('searchbox', { name: 'Find pack item', exact: true })
  await input.click()
  const rect = await d.boundingBox(), x = rect.x + rect.width / 2, y = rect.y + 150
  await p.mouse.move(x, y)
  const hover = await paint(p)
  await p.mouse.down()
  await p.mouse.move(x - 40, y, { steps: 4 })
  const drag = await paint(p)
  if (density === 'comfortable') await p.screenshot({ path: resolve(artifactDir, theme + '-drag.png') })
  check(tag + ': hover and text-field-to-drag use one neutral line', hover.color === drag.color && drag.color !== 'rgba(0, 0, 0, 0)' && drag.width === '2px' && drag.outline === 'none' && drag.dragging, { hover, drag })
  await p.mouse.up()
  await p.mouse.move(400, 90)
  check(tag + ': pointer release clears paint while retaining focus', idle(await paint(p)) && (await paint(p)).focused, await paint(p))
  // Real Tab entry, starting at the last visible control before the divider.
  await p.locator('#main').evaluate(e => [...e.querySelectorAll('a[href],button,input,select,textarea,[tabindex="0"]')].filter(n => n.getBoundingClientRect().width > 0 && !n.disabled).at(-1)?.focus())
  await p.keyboard.press('Tab')
  const keyboard = await paint(p)
  if (density === 'comfortable') await p.screenshot({ path: resolve(artifactDir, theme + '-keyboard.png') })
  check(tag + ': Tab focus paints one accessible line', keyboard.focused && keyboard.color !== hover.color && keyboard.color !== 'rgba(0, 0, 0, 0)' && keyboard.width === '2px' && keyboard.outline === 'none', keyboard)
  const before = Number(await d.getAttribute('aria-valuenow'))
  await p.keyboard.press('ArrowLeft')
  check(tag + ': arrow resizing still works', Number(await d.getAttribute('aria-valuenow')) === Math.min(before + 8, Number(await d.getAttribute('aria-valuemax'))))
  const r = await d.boundingBox()
  await p.mouse.move(r.x + r.width / 2, y)
  await p.mouse.down()
  await p.mouse.move(r.x - 12, y)
  await p.mouse.up()
  await p.mouse.move(400, 90)
  check(tag + ': pointer use clears earlier keyboard highlight', idle(await paint(p)), await paint(p))
  await p.keyboard.press('ArrowRight')
  check(tag + ': keyboard reuse restores focus cue', (await paint(p)).color === keyboard.color)
  await p.keyboard.press('Tab')
  check(tag + ': Tab away clears focus cue', idle(await paint(p)))
  const head = p.locator('.desk-head')
  await head.getByRole('button', { name: 'Console', exact: true }).click()
  await p.mouse.move(400, 90)
  const inspectorStyle = await buttonPaint(head.getByRole('button', { name: 'Inspector', exact: true }))
  const consoleStyle = await buttonPaint(head.getByRole('button', { name: 'Console', exact: true }))
  const footerStyle = await buttonPaint(p.getByRole('button', { name: 'Collapse console', exact: true }))
  check(tag + ': pressed and expanded pane toggles share neutral paint', JSON.stringify(inspectorStyle) === JSON.stringify(consoleStyle) && JSON.stringify(consoleStyle) === JSON.stringify(footerStyle) && inspectorStyle.border === 'rgba(0, 0, 0, 0)', { inspectorStyle, consoleStyle, footerStyle })
  await p.screenshot({ path: resolve(artifactDir, theme + '-' + density + '.png') })
  await p.setViewportSize({ width: 800, height: 800 })
  await p.getByRole('button', { name: 'Close inspector', exact: true }).click()
  await head.getByRole('button', { name: 'Project navigation', exact: true }).click()
  const navStyle = await buttonPaint(head.getByRole('button', { name: 'Project navigation', exact: true, includeHidden: true }))
  check(tag + ': left drawer uses the same selected style', JSON.stringify(navStyle) === JSON.stringify(consoleStyle), navStyle)
  await p.keyboard.press('Escape')
  check(tag + ': narrow layout stays contained', await p.evaluate(() => document.documentElement.scrollWidth <= innerWidth))
  await p.close()
 }
 const p = await browser.newPage({ viewport: { width: 1440, height: 900 }, hasTouch: true })
 await p.goto(launch)
 await p.locator('a[href^="/packs/"]').first().waitFor()
 await p.getByRole('banner').getByRole('button', { name: 'Inspector', exact: true }).click()
 const d = p.getByRole('separator', { name: 'Inspector', exact: true })
 await d.waitFor()
 const cdp = await p.context().newCDPSession(p)
 for (const finish of ['touchEnd', 'touchCancel']) {
  const r = await d.boundingBox(), x = r.x + r.width / 2, y = r.y + 150
  await cdp.send('Input.dispatchTouchEvent', { type: 'touchStart', touchPoints: [{ x, y }] })
  await cdp.send('Input.dispatchTouchEvent', { type: 'touchMove', touchPoints: [{ x: x - 30, y }] })
  await cdp.send('Input.dispatchTouchEvent', { type: finish, touchPoints: [] })
  await p.waitForTimeout(80)
  check(finish + ': touch leaves no sticky hover/focus', idle(await paint(p)), await paint(p))
 }
 await p.close()
} catch (e) { errors.push(String(e.message).replaceAll(secret, '[redacted]')) }
await browser.close()
const output = { summary: { checks: results.length, failed: results.filter(r => !r.pass).length, errors: errors.length }, results, errors }
await writeFile(resolve(artifactDir, 'results.json'), JSON.stringify(output, null, 2))
console.log(JSON.stringify({ summary: output.summary, failures: results.filter(r => !r.pass), errors }))
if (output.summary.failed || errors.length) process.exitCode = 1
