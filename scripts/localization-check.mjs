import { createRequire } from 'node:module'
import { randomBytes } from 'node:crypto'
import { cp, mkdtemp, readFile, mkdir, rm } from 'node:fs/promises'
import { spawn } from 'node:child_process'
import assert from 'node:assert/strict'
import { join, resolve } from 'node:path'
import { tmpdir } from 'node:os'
import { once } from 'node:events'

// Uses isolated data/configuration, never sends a message or calls a provider.
const root = resolve(new URL('..', import.meta.url).pathname)
const [binary, fixture, output = '/tmp/jp-localization-artifacts'] = process.argv.slice(2)
if (!binary || !fixture || !process.env.JPACK_BIN) throw new Error('Supply Desk, a project fixture and JPACK_BIN')
const { chromium } = createRequire(`${root}/web/package.json`)('playwright-core')
const work = await mkdtemp(join(tmpdir(), 'jp-localization-browser-'))
await cp(fixture, `${work}/project`, { recursive: true }); await mkdir(`${work}/config`); await mkdir(output, { recursive: true })
const secret = randomBytes(24).toString('hex'), origin = 'http://127.0.0.1:8851'
const server = spawn(binary, ['--dev-token', secret, '--port', '8851', '--jpack', process.env.JPACK_BIN, `${work}/project`], {
  env: { ...process.env, XDG_CONFIG_HOME: `${work}/config`, XDG_DATA_HOME: `${work}/data` }, stdio: 'ignore'
})
let browser
const results = [], errors = []
try {
  let ready = false
  for (let i = 0; i < 80; i++) {
    try { if ((await fetch(origin + '/api/conversations', { headers: { Authorization: `Bearer ${secret}` } })).ok) { ready = true; break } } catch {}
    await new Promise(resolve => setTimeout(resolve, 100))
  }
  assert(ready, 'server started')
  browser = await chromium.launch({ executablePath: process.env.PLAYWRIGHT_CHROME ?? '/usr/bin/google-chrome', headless: true, args: ['--no-sandbox'] })
  const languages = ['en', 'fr', 'es', 'de', 'it', 'pt-PT', 'pt-BR', 'ko', 'zh-Hans', 'zh-Hant', 'yue-Hant', 'ja']
  for (const language of languages) {
    const catalogue = JSON.parse(await readFile(`${root}/web/src/i18n/locales/${language}.json`, 'utf8'))
    const context = await browser.newContext({ viewport: { width: 1440, height: 900 }, colorScheme: 'dark', locale: language })
    const page = await context.newPage()
    page.on('pageerror', error => errors.push(error.message))
    await page.goto(`${origin}/launch?secret=${secret}`)
    await page.locator('textarea').first().waitFor()
    assert.equal(await page.locator('html').getAttribute('lang'), language)
    await page.getByRole('heading', { name: catalogue['What would you like to work on?'], exact: true }).waitFor()
    const editor = page.locator('textarea').first()
    await editor.fill('Draft /case/type = "approval" — 未送信')
    await page.locator('.desk-user').click()
    const label = catalogue.Language ?? 'Language'
    await page.getByRole('menuitem', { name: label, exact: true }).hover()
    await page.getByRole('menuitemradio', { name: 'English', exact: true }).waitFor()
    await page.screenshot({ path: `${output}/${language}-language-menu.png` })
    if (language === 'en' || language === 'de') {
      const item = page.getByRole('menuitemradio', { name: 'English', exact: true })
      const box = await item.boundingBox()
      assert(box)
      // Traverse the submenu grace area as a pointer does, instead of teleporting.
      await page.mouse.move(box.x + box.width / 2, box.y + box.height / 2, { steps: 12 })
      await item.click()
    } else {
      await page.getByRole('menuitemradio', { name: 'English', exact: true }).focus()
      await page.keyboard.press('Enter')
    }
    await page.keyboard.press('Escape')
    await page.waitForFunction(() => document.documentElement.lang === 'en')
    assert.equal(await editor.inputValue(), 'Draft /case/type = "approval" — 未送信')
    assert.equal(await page.evaluate(() => localStorage.getItem('jpack-desk.language.v1')), 'en')
    await page.reload(); await editor.waitFor()
    assert.equal(await page.locator('html').getAttribute('lang'), 'en')
    results.push(`${language}: system detection, landing label, switch to English, draft preservation, reload`)
    // Representative expansion/CJK layouts in both themes and narrow viewports.
    if (['de', 'yue-Hant', 'ja'].includes(language)) {
      await page.evaluate(language => { localStorage.setItem('jpack-desk.language.v1', language) }, language)
      for (const scheme of ['dark', 'light']) for (const width of [1440, 768, 390]) {
        await page.emulateMedia({ colorScheme: scheme }); await page.setViewportSize({ width, height: 900 }); await page.reload(); await editor.waitFor()
        const overflow = await page.evaluate(() => document.documentElement.scrollWidth > window.innerWidth + 1)
        assert.equal(overflow, false, `${language} ${scheme} ${width}: no page overflow`)
        await page.screenshot({ path: `${output}/${language}-${scheme}-${width}.png`, fullPage: true })
      }
    }
    await context.close()
  }
  const chats = await (await fetch(origin + '/api/conversations', { headers: { Authorization: `Bearer ${secret}` } })).json()
  assert(Array.isArray(chats.content?.chats), 'chat listing is complete')
  assert.equal(chats.content.chats.length, 0)
  assert.deepEqual(errors, [])
  console.log(JSON.stringify({ results, noSavedConversations: true, browserErrors: errors }, null, 2))
} finally {
  await browser?.close()
  server.kill('SIGTERM'); await Promise.race([once(server, 'exit'), new Promise(resolve => setTimeout(resolve, 1500))])
  await rm(work, { recursive: true, force: true })
}
