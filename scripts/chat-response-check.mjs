import { createRequire } from 'node:module'
import { randomBytes } from 'node:crypto'
import { cp, mkdtemp, mkdir, readFile, writeFile, rm } from 'node:fs/promises'
import { spawn } from 'node:child_process'
import { once } from 'node:events'
import { resolve } from 'node:path'
import assert from 'node:assert/strict'
// Real Desk/runtime, isolated project, deterministic model replies intercepted
// in the browser. No API key is saved and no external model is called.
const root = resolve(new URL('..', import.meta.url).pathname)
const [binary, fixture, output = '/tmp/jp-chat-response-artifacts'] = process.argv.slice(2)
if (!binary || !fixture || !process.env.JPACK_BIN) throw new Error('Supply Desk, fixture and JPACK_BIN')
const { chromium } = createRequire(`${root}/web/package.json`)('playwright-core')
const work = await mkdtemp('/tmp/jp-chat-response-')
await cp(fixture, `${work}/project`, { recursive: true })
await mkdir(`${work}/config`); await mkdir(output, { recursive: true })
const secret = randomBytes(24).toString('hex'), origin = 'http://127.0.0.1:8849'
const server = spawn(binary, ['--dev-token', secret, '--port', '8849', '--jpack', process.env.JPACK_BIN, `${work}/project`], { env: { ...process.env, XDG_CONFIG_HOME: `${work}/config` }, stdio: 'ignore' })
const api = (path, init = {}) => fetch(origin + path, { ...init, headers: { Authorization: `Bearer ${secret}`, ...init.headers } })
const results = [], errors = [], requests = []
let browser, page
try {
  let ready = false
  for (let i = 0; i < 80; i++) { try { if ((await api('/api/conversations')).ok) { ready = true; break } } catch {} await new Promise(r => setTimeout(r, 100)) }
  assert(ready)
  const pack = JSON.parse(await readFile(`${work}/project/sanctions-screening-0.1.0.pack.json`, 'utf8'))
  browser = await chromium.launch({ executablePath: process.env.PLAYWRIGHT_CHROME ?? '/usr/bin/google-chrome', headless: true, args: ['--no-sandbox'] })
  page = await browser.newPage({ viewport: { width: 1440, height: 900 }, colorScheme: 'dark' })
  page.setDefaultTimeout(15000)
  page.on('pageerror', error => errors.push((error.stack ?? error.message).replaceAll(secret, '[redacted]')))
  await page.route('**/api/desk-config', async route => {
    const response = await route.fetch(), content = await response.json()
    await route.fulfill({ response, json: { ...content, present: true, content: JSON.stringify({ deskConfigVersion: 1, assistant: { engine: 'vercel', thinking: 'off', endpoint: { url: 'https://assistant.example.invalid/v1', kind: 'openai-compatible', model: 'preview-model', models: ['preview-model'], tools: ['get_schema', 'list_examples', 'get_example', 'validate', 'experimental_evaluate'] } } }) } })
  })
  await page.route('**/api/assistant/key', route => route.fulfill({ json: { present: true } }))
  let responseText = 'Hello! How can I help?', delay = 0, refusal = false
  await page.route('**/api/assistant/relay/v1/**', async route => {
    requests.push(route.request().postDataJSON())
    if (refusal) { await route.fulfill({ status: 400, json: { error: { message: 'This endpoint refuses the request.' } } }); return }
    const text = responseText
    if (delay) await new Promise(r => setTimeout(r, delay))
    await route.fulfill({ json: { id: 'scripted', model: 'preview-model', object: 'chat.completion', created: 1, choices: [{ index: 0, finish_reason: 'stop', message: { role: 'assistant', content: text } }], usage: { prompt_tokens: 10, completion_tokens: 10, total_tokens: 20 } } }).catch(() => {})
  })
  await page.goto(`${origin}/launch?secret=${secret}`)
  const chat = page.getByRole('region', { name: 'Assistant chat' })
  const message = () => chat.getByRole('textbox', { name: 'Message the assistant' })
  const send = async text => { await message().fill(text); await chat.getByRole('button', { name: 'Send', exact: true }).click() }
  await page.getByRole('heading', { name: 'What would you like to work on?' }).waitFor()
  assert.equal((await (await api('/api/conversations')).json()).content.chats.length, 0)
  await send('hi')
  await chat.getByText('Hello! How can I help?', { exact: true }).waitFor()
  await chat.getByRole('button', { name: 'Send', exact: true }).waitFor()
  assert.equal(requests.length, 1)
  assert.equal(await chat.getByText(/Activity ·|Work ·|Stopped|shape your pack/).count(), 0)
  assert.equal(await chat.getByRole('combobox', { name: 'Task tools' }).isDisabled(), false)
  await page.waitForFunction(() => !document.querySelector('[class*="footnote"]')?.textContent?.includes('Saving'))
  const first = (await (await api('/api/conversations')).json()).content.chats[0]
  const at = first.updatedAt
  await page.reload()
  await chat.getByText('Hello! How can I help?', { exact: true }).waitFor()
  await page.waitForFunction(() => !document.querySelector('[class*="footnote"]')?.textContent?.includes('Saving'))
  assert.equal((await (await api('/api/conversations')).json()).content.chats[0].updatedAt, at)
  assert.equal(await chat.getByText(/Stopped|Recheck.*draft/).count(), 0)
  assert.equal(requests.length, 1)
  results.push('Greeting: one model call, no authoring activity, no repeated status; reload preserves completion and recency')

  responseText = '**Summary**\n\n- First condition\n- Second condition\n\n[Official source](https://example.org/source)\n\n```json\n{"applies":true}\n```'
  await send('Explain the conditions using an example')
  await chat.getByRole('link', { name: 'Official source' }).waitFor()
  assert.equal(await chat.locator('article[data-role="assistant"] ul li').count(), 2)
  assert.equal(await chat.getByRole('button', { name: 'Copy Code' }).count(), 1)
  assert.equal(await chat.getByRole('button', { name: 'Open draft' }).count(), 0)
  await page.screenshot({ path: `${output}/formatted-response.png` })
  results.push('Markdown, source links, lists and copyable code render without treating examples as pack proposals')

  await message().fill('Keep this prompt')
  await page.locator('input[type=file]').setInputFiles({ name: 'policy.txt', mimeType: 'text/plain', buffer: Buffer.from('Example policy text.') })
  await chat.getByRole('button', { name: 'Remove policy.txt' }).waitFor()
  assert.equal(await message().inputValue(), 'Keep this prompt')
  await chat.getByRole('button', { name: 'policy.txt', exact: true }).click()
  await page.getByRole('button', { name: 'Copy Attachment' }).waitFor()
  await page.keyboard.press('Escape')
  await chat.getByRole('button', { name: 'Remove policy.txt' }).click()
  assert.equal(await message().inputValue(), 'Keep this prompt')
  results.push('Attachment chip previews and removes the file without modifying the prompt')

  // A slow local read must not race Send, and cancel must ignore late bytes.
  await page.evaluate(() => {
    const read = File.prototype.arrayBuffer
    File.prototype.arrayBuffer = function () {
      if (this.name !== 'slow.txt') return read.call(this)
      return new Promise(resolve => { window.finishAttachment = () => resolve(new TextEncoder().encode('Late file content').buffer) })
    }
  })
  const beforeUpload = requests.length
  await page.locator('input[type=file]').setInputFiles({ name: 'slow.txt', mimeType: 'text/plain', buffer: Buffer.from('Late file content') })
  await chat.getByRole('status').filter({ hasText: 'Reading files…' }).waitFor()
  assert(await chat.getByRole('button', { name: 'Send', exact: true }).isDisabled())
  await message().press('Enter')
  assert.equal(requests.length, beforeUpload)
  await chat.getByRole('button', { name: 'Cancel', exact: true }).click()
  await page.evaluate(() => window.finishAttachment())
  assert.equal(await chat.getByRole('button', { name: 'Remove slow.txt' }).count(), 0)
  assert.equal(await message().inputValue(), 'Keep this prompt')
  assert(await chat.getByRole('button', { name: 'Send', exact: true }).isEnabled())
  await page.locator('input[type=file]').setInputFiles({ name: 'unsupported.pdf', mimeType: 'application/pdf', buffer: Buffer.from('%PDF') })
  await chat.getByRole('alert').filter({ hasText: 'PDF and connected sources are not available yet.' }).waitFor()
  assert.equal(requests.length, beforeUpload)
  results.push('Reading blocks Send and Enter; cancel ignores late bytes; unsupported files give an honest error without losing the prompt')

  responseText = `Here is a draft.\n\n\`\`\`json\n${JSON.stringify({ proposal: { document: pack, unknowns: [] } })}\n\`\`\``
  await send('Create the supplied screening pack')
  await chat.getByText('Structure checked · Tests not run', { exact: false }).waitFor()
  await chat.getByRole('button', { name: 'Open draft', exact: true }).click()
  await page.getByRole('tab', { name: 'Review', exact: true }).click()
  assert.equal(await page.getByRole('button', { name: 'Review and create', exact: true }).count(), 1)
  assert.equal(await page.getByText('0 of 0 agree', { exact: false }).count(), 0)
  await page.getByRole('button', { name: 'Review and create', exact: true }).click()
  assert.equal(await message().isDisabled(), false)
  responseText = 'The possible outcomes are clear and hit.'
  await send('Explain the outcomes before I create it')
  await chat.getByText(responseText, { exact: true }).waitFor()
  await page.getByRole('button', { name: 'Create pack', exact: true }).waitFor()
  const changed = { ...pack, title: 'Updated screening decision' }
  responseText = `Updated draft.\n\n\`\`\`json\n${JSON.stringify({ proposal: { document: changed, unknowns: [] } })}\n\`\`\``
  await send('Change the title to Updated screening decision')
  await page.getByText('The draft changed. Review the latest revision before creating it.', { exact: true }).waitFor()
  assert.equal(await page.getByRole('button', { name: 'Create pack', exact: true }).count(), 0)
  await page.screenshot({ path: `${output}/review-with-chat.png` })
  results.push('One review action; questions stay enabled; a new revision invalidates the previous Create review')

  await page.locator('.desk-pane-head').getByRole('button', { name: 'New chat', exact: true }).click()
  await page.getByRole('heading', { name: 'What would you like to work on?' }).waitFor()
  responseText = 'This response must not appear after Stop'; delay = 1500
  await send('Wait while I check something')
  await chat.getByRole('button', { name: 'Stop', exact: true }).waitFor()
  assert.equal(await chat.getByText('Working…', { exact: true }).count(), 1)
  await chat.getByRole('button', { name: 'Stop', exact: true }).click()
  await chat.getByRole('button', { name: 'Retry response' }).waitFor()
  await page.waitForTimeout(1700)
  assert.equal(await chat.getByText(responseText, { exact: true }).count(), 0)
  delay = 0; responseText = 'Retried successfully.'
  await chat.getByRole('button', { name: 'Retry response' }).click()
  await chat.getByText(responseText, { exact: true }).waitFor()
  assert.equal(await chat.getByText('Wait while I check something', { exact: true }).count(), 1)
  results.push('Stop discards late replies; explicit retry preserves one user message')

  refusal = true
  await send('Check the endpoint refusal path')
  await chat.getByRole('status').filter({ hasText: 'This endpoint refuses the request.' }).waitFor()
  await chat.getByRole('button', { name: 'Retry response' }).waitFor()
  refusal = false
  responseText = 'The endpoint is available again.'
  await chat.getByRole('button', { name: 'Retry response' }).click()
  await chat.getByText(responseText, { exact: true }).waitFor()
  results.push('Endpoint refusal is explained in chat and retries cleanly without an unhandled browser rejection')

  await page.goto(`${origin}/chats/${first.id}`)
  await chat.getByText('Hello! How can I help?', { exact: true }).waitFor()
  await chat.locator('[class*="thread"]').evaluate(node => { node.scrollTop = 0; node.dispatchEvent(new Event('scroll')) })
  await chat.getByRole('button', { name: 'Jump to latest' }).click()
  results.push('Scrolled-up conversations offer Jump to latest without forcing the scroll position')
  await page.getByRole('button', { name: 'Chat', exact: true }).click()
  for (const [width, height] of [[1440, 900], [800, 700], [390, 720], [1440, 460]]) {
    await page.setViewportSize({ width, height })
    await page.waitForTimeout(100)
    assert.equal(await page.evaluate(() => document.documentElement.scrollWidth > innerWidth), false)
    const box = await message().boundingBox()
    assert(box && box.x >= 0 && box.x + box.width <= width + 1 && box.y + box.height <= height + 1)
    assert.equal(await chat.getByRole('combobox', { name: 'Model', exact: true }).locator('span').first().evaluate(node => getComputedStyle(node).whiteSpace), 'nowrap', 'Model names stay on one line in a narrow composer')
    await page.screenshot({ path: `${output}/chat-${width}x${height}.png` })
  }
  await page.emulateMedia({ colorScheme: 'light' })
  await page.screenshot({ path: `${output}/chat-light.png` })
  results.push('Composer stays visible with no page overflow at desktop, tablet, mobile and short-window sizes')
  assert.deepEqual(errors, [])
  await writeFile(`${output}/results.json`, JSON.stringify({ results, errors, modelCalls: requests.length }, null, 2))
  console.log(JSON.stringify({ passed: results.length, results, artifacts: output }, null, 2))
} catch (error) {
  await page?.screenshot({ path: `${output}/failure.png` }).catch(() => {})
  await writeFile(`${output}/failure.json`, JSON.stringify({ results, url: page?.url(), errors, modelCalls: requests.length }, null, 2)); throw new Error(String(error.stack ?? error).replaceAll(secret, '[redacted]'))
} finally {
  await browser?.close(); server.kill('SIGTERM'); await Promise.race([once(server, 'exit'), new Promise(r => setTimeout(r, 3000))]); await rm(work, { recursive: true, force: true })
}
