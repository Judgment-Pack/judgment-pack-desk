// Inactive offline presentation/protocol proof, never B3 or resource qualification.
// Run from web: CHROME_PATH=/path/to/chromium node scripts/retained-selection-review-browser.mjs
import assert from 'node:assert/strict'
import {createHash} from 'node:crypto'
import {readFile} from 'node:fs/promises'
import {fileURLToPath} from 'node:url'
import {createServer} from 'vite'
import react from '@vitejs/plugin-react'
import {chromium} from 'playwright-core'
const root = fileURLToPath(new URL('../', import.meta.url))
const sha = raw => createHash('sha256').update(raw).digest('hex')
const bytes = text => Buffer.from(text, 'utf8')
const u32 = n => {const b = Buffer.alloc(4); b.writeUInt32BE(n); return b}
const u64 = n => {const b = Buffer.alloc(8); b.writeBigUInt64BE(BigInt(n)); return b}
const q = value => JSON.stringify(value).replace(/[<>&\u2028\u2029]/g, c => '\\u' + c.charCodeAt(0).toString(16).padStart(4, '0'))
const frame = (tag, text) => Buffer.concat([...(tag ? [Buffer.from([tag])] : []), u32(bytes(text).length), bytes(text)])
const media = 'application/vnd.jpack-desk.retained-selection.v1'
const session = 'offline-fixture-issued-session'
let phase = 'startup', source, artifact, browser, server, origin, releasePrepare, releaseRead
let prepares = 0, reads = 0, closes = 0, auth = 0, sessions = 0
const requests = [], faults = [], pageErrors = [], timings = [], pendingResponses = new Set()
const identities = {}
// Independent handwritten Inputs: original lexical JSON is never parsed/re-serialized.
const longToken = '"' + 'x'.repeat(9000) + '😀TAIL"'
function input(id, second = false) {
 const c = JSON.stringify(source.conversionContract), selected = c.slice(0, -1) + ',"roles":["document-validation","preview","preparation"]}'
 const custody = '{"values":{},"prepared":{},"origin":{"preparationId":' + q(source.preparationId) + ',"recordId":' + q(id) + ',"sourceId":"offline-source","sourceDigest":' + q(source.packDigest) + ',"row":1,"importedAt":"fixture","importRecipeVersion":1,"recipeVersion":1,"packDigest":' + q(source.packDigest) + ',"originalValues":{"blank":"","missing":null},"conversions":{}},"retained":{"contractVersion":"1","origin":"asserted","values":{"blank":"","missing":null},"research":null},"unknownSibling":"custody tail"}'
 const facts = second ? '{"row":"SECOND_ROW"}' : '{"unsafe":9007199254740993,"negative":-0,"decimal":1.0,"exponent":1E+03,"null":null,"false":false,"empty":"","escaped":"\\ud800","html":"<script>globalThis.retainedXSS=true</script>","long":' + longToken + ',' + Array.from({length: 51}, (_, i) => '"k' + i + '":' + i).join(',') + '}'
 return '{"facts":' + facts + ',"source":{"conversionContract":' + selected + ',"mapping":{},"mappingDigest":"sha256:' + 'b'.repeat(64) + '","retainedCaseJson":' + q(custody) + '},"preparation":{"conversionContract":' + selected + ',"version":2,"verifiedAt":"2026-10-01T00:00:00Z","mappingDigest":"sha256:' + 'b'.repeat(64) + '","verification":"fixture","cites":[],"lineage":[],"outcomes":[]}}'
}
function makeArtifact() {
 const inputs = source.recordIds.map((id, i) => bytes(input(id, i === 1)))
 const c = source.conversionContract
 const contract = '{"contract":' + q(c.contract) + ',"spec":{"version":' + q(c.spec.version) + ',"revision":' + q(c.spec.revision) + ',"schemaSha256":' + q(c.spec.schemaSha256) + ',"corpusSha256":' + q(c.spec.corpusSha256) + '},"profile":' + q(c.profile) + '}'
 const ids = Buffer.concat([bytes('jpack-desk/retained-selection/1:ids\0'), u32(2), ...source.recordIds.flatMap(id => [u32(bytes(id).length), bytes(id)])])
 const header = '{"wireVersion":1,"viewId":"rv_' + '3'.repeat(48) + '","preparationId":' + q(source.preparationId) + ',"revision":' + q(source.revision) + ',"packDigest":' + q(source.packDigest) + ',"conversionContract":' + contract + ',"origin":"asserted","rows":2,"maxInputBytes":37748736,"selectionSha256":' + q(sha(ids)) + '}'
 const rows = inputs.map((raw, i) => ({index: i, id: source.recordIds[i], inputBytes: raw.length, inputSha256: sha(raw)}))
 const index = sha(Buffer.concat([bytes('jpack-desk/retained-selection/1:index\0'), Buffer.from(sha(bytes(header)), 'hex'), u32(2), ...rows.flatMap(r => [u32(r.index), u32(bytes(r.id).length), bytes(r.id), u64(r.inputBytes), Buffer.from(r.inputSha256, 'hex')])]))
 const total = inputs.reduce((sum, b) => sum + b.length, 0)
 const end = '{"rows":2,"inputBytesTotal":' + total + ',"indexSha256":' + q(index) + '}'
 const wire = Buffer.concat([bytes('JPACK-RS/1\n'), frame(0, header), ...rows.flatMap((r, i) => [frame(1, JSON.stringify(r)), inputs[i]]), frame(2, end)])
 return {wire, inputs, rows, index, viewId: 'rv_' + '3'.repeat(48)}
}
function reply(res, status, body, type = 'application/json', extra = {}) {
 const raw = Buffer.isBuffer(body) ? body : bytes(JSON.stringify(body))
 res.writeHead(status, {'Content-Type': type, 'Content-Length': raw.length, ...extra}); res.end(raw)
}
async function body(req) {
 let value = ''
 for await (const part of req) {value += part; assert.ok(value.length <= 4096, 'bounded fixture request')}
 return JSON.parse(value)
}
async function api(req, res) {
 const path = new URL(req.url, 'http://fixture.invalid').pathname
 if (!path.startsWith('/api/')) return false
 requests.push({phase, method: req.method, path})
 if (path === '/api/auth/status') {assert.equal(req.method, 'GET'); auth++; reply(res, 200, {enabled: false, unavailable: false, sessionActive: false}); return true}
 if (path === '/api/session') {assert.equal(req.method, 'POST'); sessions++; reply(res, 200, {id: session, subject: 'offline', issuer: 'fixture'}); return true}
 assert.equal(req.headers.authorization, 'Bearer ' + session, 'supported bootstrap supplies synthetic bearer')
 assert.equal(req.method, 'POST')
 assert.ok(source && artifact, 'fixture metadata captured before explicit preparation')
 const prefix = '/api/input-preparations/' + source.preparationId + '/'
 const data = await body(req)
 if (path === prefix + 'prepare') {
  prepares++; assert.deepEqual(data, {recordIds: source.recordIds}); assert.equal(req.headers['if-match'], source.revision)
  const raw = artifact.wire
  res.writeHead(200, {'Content-Type': media, 'Content-Length': raw.length})
  // Hold the final byte so no successful EOF is available to the real fetch reader.
  pendingResponses.add(res); res.write(raw.subarray(0, -1))
  releasePrepare = () => {pendingResponses.delete(res); if (!res.destroyed) res.end(raw.subarray(-1))}
  return true
 }
 if (path === prefix + 'prepared-view-close') {
  closes++; assert.deepEqual(data, {viewId: artifact.viewId, indexSha256: artifact.index}); reply(res, 200, {}); return true
 }
 if (path === prefix + 'prepared-row') {
  reads++; const row = artifact.rows[data.index]
  assert.ok(row); assert.deepEqual(data, {viewId: artifact.viewId, indexSha256: artifact.index, index: row.index, id: row.id}); assert.equal(req.headers['if-match'], artifact.index)
  const expected = [0, 1, 0, 1, 0, 1, 1, 0, 0, 1][reads - 1]
  assert.equal(row.index, expected, 'exact explicit read sequence')
  if (reads === 6) {reply(res, 409, {code: 'review-not-ready'}); return true}
  if (reads === 8) {reply(res, 410, {code: 'review-unavailable'}); return true}
  if (reads === 10) {reply(res, 401, {code: 'unauthorized'}, 'application/json', {'X-Jpack-Desk-Refusal': 'unauthorized'}); return true}
  const raw = artifact.inputs[row.index]
  if (reads === 4) {
   res.writeHead(200, {'Content-Type': 'application/json', 'Content-Length': raw.length}); res.write(raw.subarray(0, 1)); pendingResponses.add(res)
   releaseRead = () => {pendingResponses.delete(res); if (!res.destroyed) res.end(raw.subarray(1))}
  } else reply(res, 200, raw)
  return true
 }
 throw new Error('Unexpected API call: ' + req.method + ' ' + path)
}
const wait = async (predicate, label) => {
 const until = Date.now() + 8000
 while (!predicate()) {assert.ok(Date.now() < until, 'timeout: ' + label); await new Promise(resolve => setTimeout(resolve, 20))}
}
const counts = () => ({auth, sessions, prepares, reads, closes})
const checkpoint = name => {timings.push({phase: name, elapsedMs: Date.now() - started, ...counts()}); phase = name}
const started = Date.now()
const watchdog = setTimeout(async () => {
 console.error(JSON.stringify({status: 'timeout', phase, elapsedMs: Date.now() - started, counts: counts(), identities}))
 for (const response of pendingResponses) response.destroy()
 // Close only this driver's owned browser/server, never search for processes.
 try {await browser?.close()} finally {await server?.close()}
 process.exit(1)
}, 165000)
try {
 assert.ok(process.env.CHROME_PATH, 'CHROME_PATH must name the parent-provided browser')
 for (const path of ['src/dialects/RetainedRecordReview.tsx', 'src/dialects/retainedSelection/InputDetails.tsx', 'src/dialects/retainedSelection/client.ts', 'src/dialects/retainedSelection/controller.ts', 'src/dialects/retainedSelection/input.ts', 'src/dialects/retainedSelection/wire.ts', 'scripts/retained-selection-review-browser.mjs', 'scripts/fixtures/retained-selection-review.html', 'scripts/fixtures/retained-selection-review.tsx']) identities['web/' + path] = sha(await readFile(new URL('../' + path, import.meta.url)))
 server = await createServer({root, configFile: false, plugins: [react(), {name: 'offline-retained-api', configureServer(vite) {
  vite.middlewares.use((req, res, next) => {api(req, res).then(handled => {if (!handled) next()}).catch(error => {faults.push(error.message); if (!res.headersSent) reply(res, 500, {code: 'fixture-failure'}); else res.destroy()})})
 }}], server: {host: '127.0.0.1', port: 0, strictPort: false, proxy: {}, hmr: false}, clearScreen: false})
 await server.listen()
 origin = 'http://127.0.0.1:' + server.httpServer.address().port
 browser = await chromium.launch({executablePath: process.env.CHROME_PATH, headless: true, args: ['--no-sandbox', '--disable-dev-shm-usage']})
 const context = await browser.newContext({viewport: {width: 1100, height: 800}, reducedMotion: 'reduce', serviceWorkers: 'block'})
 await context.route('**/*', route => {
  const url = new URL(route.request().url())
  if (url.origin !== origin) {faults.push('Denied external request: ' + url.origin); return route.abort()}
  return route.continue()
 })
 const page = await context.newPage(); page.setDefaultTimeout(8000)
 page.on('pageerror', error => pageErrors.push(error.message))
 const open = async () => {await page.locator('#open-review').click(); await page.getByRole('dialog', {name: 'Review records'}).waitFor()}
 const button = name => page.getByRole('button', {name, exact: true})
 const select = async (label, option) => {await page.getByRole('combobox', {name: label, exact: true}).click(); await page.getByRole('option', {name: option, exact: true}).click()}
 const visibleCode = async token => {await page.locator('code').filter({hasText: token}).first().waitFor(); assert.ok((await page.locator('code').allTextContents()).includes(token), 'exact lexical token ' + token)}
 const noMounted = async () => assert.equal(await page.getByRole('combobox', {name: 'Section', exact: true}).count(), 0, 'no mounted Input')
 const settle = async () => {await page.waitForTimeout(100); assert.deepEqual(faults, []); assert.deepEqual(pageErrors, [])}
 const noReplay = async () => {const before = counts(); await page.evaluate(() => {window.dispatchEvent(new Event('focus')); document.dispatchEvent(new Event('visibilitychange'))}); await settle(); assert.deepEqual(counts(), before, 'focus/visibility never replay work')}
 checkpoint('explicit-prepare')
 await page.goto(origin + '/scripts/fixtures/retained-selection-review.html')
 await page.locator('#open-review').waitFor()
 source = await page.evaluate(() => window.retainedBrowserFixture.source); artifact = makeArtifact()
 await open(); assert.deepEqual(counts(), {auth: 0, sessions: 0, prepares: 0, reads: 0, closes: 0})
 // Dispatch two browser clicks in the same event turn, while the action is live.
 await button('Prepare review').evaluate(el => {el.click(); el.click()})
 await wait(() => prepares === 1 && releasePrepare, 'first prepare held')
 await noMounted(); assert.equal(await page.getByRole('combobox', {name: 'Record', exact: true}).count(), 0)
 await noReplay(); assert.equal(prepares, 1)
 releasePrepare(); releasePrepare = undefined
 await page.getByRole('combobox', {name: 'Record', exact: true}).waitFor(); await noMounted(); assert.equal(reads, 0)
 checkpoint('explicit-mount-and-lexical-values')
 await select('Record', '1 · row-a'); await visibleCode('9007199254740993')
 assert.equal(await page.getByRole('combobox', {name: 'Section', exact: true}).count(), 1)
 for (const token of ['-0', '1.0', '1E+03', 'null', 'false', '""', String.raw`"\ud800"`, '"<script>globalThis.retainedXSS=true</script>"']) await visibleCode(token)
 assert.equal(await page.evaluate(() => window.retainedXSS), undefined)
 assert.equal(await page.getByRole('dialog').locator('script').count(), 0)
 await noReplay()
 checkpoint('member-and-token-pagination')
 const longRow = page.getByRole('row').filter({has: page.getByRole('cell', {name: 'long', exact: true})})
 await longRow.getByRole('button', {name: 'View full value', exact: true}).click()
 const tokenDetails = page.locator('details').filter({has: page.getByLabel('Exact text', {exact: true})}).last()
 let combined = ''
 for (let i = 0; i < 3; i++) {
  const text = await tokenDetails.getByLabel('Exact text', {exact: true}).textContent(); assert.ok(text.length <= 4096); combined += text
  if (i < 2) await tokenDetails.getByRole('button', {name: 'Next page', exact: true}).click()
 }
 assert.equal(combined, longToken, 'every exact original token unit is reachable once'); assert.ok(combined.endsWith('😀TAIL"'))
 await tokenDetails.getByRole('button', {name: 'Previous page', exact: true}).click()
 assert.equal(await tokenDetails.getByLabel('Exact text', {exact: true}).textContent(), longToken.slice(4095, 8190))
 const memberNext = button('Next page').first()
 await memberNext.click(); await page.getByRole('cell', {name: 'k50', exact: true}).waitFor()
 assert.equal(await page.getByRole('cell', {name: 'unsafe', exact: true}).count(), 0)
 assert.equal(await page.locator('tbody tr').count(), 11)
 await button('First page').click(); await visibleCode('9007199254740993'); assert.equal(await page.locator('tbody tr').count(), 50)
 checkpoint('revisit-exact-bytes')
 const technicalBytes = async () => {
  await page.getByText('Technical details', {exact: true}).click()
  const details = page.locator('details').filter({has: page.getByRole('combobox', {name: 'Source', exact: true})}).last()
  let text = ''
  for (let i = 0; ; i++) {
   text += await details.getByLabel('Exact text', {exact: true}).textContent()
   const next = details.getByRole('button', {name: 'Next page', exact: true})
   if (await next.isDisabled()) break
   assert.ok(i < 10); await next.click()
  }
  return text
 }
 assert.equal(await technicalBytes(), artifact.inputs[0].toString('utf8'))
 await button('Next record').click(); await visibleCode('"SECOND_ROW"')
 await button('Previous record').click(); await visibleCode('9007199254740993')
 assert.equal(await technicalBytes(), artifact.inputs[0].toString('utf8')); assert.equal(reads, 3)
 checkpoint('superseded-read')
 await button('Next record').click(); await wait(() => reads === 4 && releaseRead, 'delayed row read'); await noMounted()
 await button('Previous record').click(); await button('Read again').waitFor(); await noMounted()
 releaseRead(); releaseRead = undefined; await settle(); await noMounted(); assert.equal(reads, 4)
 await button('Read again').click(); await visibleCode('9007199254740993'); assert.equal(reads, 5)
 checkpoint('409-explicit-same-row-retry')
 await button('Next record').click(); await page.getByText('This review is not ready. Read the record again.', {exact: true}).waitFor(); await noMounted()
 await noReplay(); assert.equal(reads, 6); assert.equal(prepares, 1)
 await button('Read again').click(); await visibleCode('"SECOND_ROW"'); assert.equal(reads, 7); assert.equal(prepares, 1)
 checkpoint('layout-and-theme')
 const bounds = []
 for (const width of [1100, 390]) for (const scheme of ['light', 'dark']) {
  await page.setViewportSize({width, height: 800}); await page.emulateMedia({colorScheme: scheme})
  const b = await page.getByRole('dialog').evaluate(el => {const r = el.getBoundingClientRect(); return {left: r.left, right: r.right, top: r.top, bottom: r.bottom, viewport: innerWidth, page: document.documentElement.scrollWidth, colorScheme: getComputedStyle(document.documentElement).colorScheme}})
  assert.equal(b.colorScheme, scheme, 'production stylesheet follows emulated scheme')
  assert.ok(b.left >= 0 && b.right <= width + 1 && b.top >= 0 && b.bottom <= 801 && b.page <= width, 'dialog/page within viewport')
  bounds.push({width, scheme, ...b})
 }
 checkpoint('expired-drop-and-explicit-reprepare')
 await button('Previous record').click(); await page.getByText('This review is no longer available. Prepare it again to continue.', {exact: true}).waitFor(); await noMounted()
 await wait(() => closes === 1, 'expired view close'); await noReplay(); assert.equal(reads, 8); assert.equal(prepares, 1)
 await button('Prepare review again').click(); await wait(() => prepares === 2 && releasePrepare, 'explicit reprepare'); releasePrepare(); releasePrepare = undefined
 await page.getByRole('combobox', {name: 'Record', exact: true}).waitFor(); await select('Record', '1 · row-a'); await visibleCode('9007199254740993')
 checkpoint('session-ended-drop-no-retry')
 await button('Next record').click(); await page.getByText('Your session ended. Sign in again to continue.', {exact: true}).waitFor(); await noMounted(); await noReplay()
 assert.equal(await button('Prepare review again').count(), 0)
 await button('Close').focus(); await page.keyboard.press('Enter'); await page.getByRole('dialog').waitFor({state: 'hidden'})
 assert.equal(await page.locator('#open-review').evaluate(el => document.activeElement === el), true)
 await open(); await page.keyboard.press('Escape'); await page.getByRole('dialog').waitFor({state: 'hidden'})
 assert.equal(await page.locator('#open-review').evaluate(el => document.activeElement === el), true)
 checkpoint('keyboard-cancel-pending-prepare')
 // New document uses a fresh context and the supported bootstrap again.
 await context.close()
 const cancelContext = await browser.newContext({viewport: {width: 900, height: 700}, serviceWorkers: 'block'})
 await cancelContext.route('**/*', route => new URL(route.request().url()).origin === origin ? route.continue() : (faults.push('Denied external request in cancel context'), route.abort()))
 const cancelPage = await cancelContext.newPage(); cancelPage.setDefaultTimeout(8000); cancelPage.on('pageerror', error => pageErrors.push(error.message))
 await cancelPage.goto(origin + '/scripts/fixtures/retained-selection-review.html'); await cancelPage.locator('#open-review').click()
 await cancelPage.getByRole('button', {name: 'Prepare review', exact: true}).click(); await wait(() => prepares === 3 && releasePrepare, 'cancel prepare held')
 await cancelPage.getByRole('button', {name: 'Cancel', exact: true}).focus(); await cancelPage.keyboard.press('Enter'); await cancelPage.getByRole('dialog').waitFor({state: 'hidden'})
 releasePrepare(); releasePrepare = undefined; await cancelPage.waitForTimeout(100)
 assert.equal(await cancelPage.getByRole('dialog').count(), 0); assert.equal(await cancelPage.locator('#open-review').evaluate(el => document.activeElement === el), true)
 assert.deepEqual(counts(), {auth: 2, sessions: 2, prepares: 3, reads: 10, closes: 1}); assert.deepEqual(faults, []); assert.deepEqual(pageErrors, [])
 checkpoint('complete')
 console.log(JSON.stringify({status: 'passed', qualification: 'offline presentation/protocol only; no actual B3/source authenticity, resource maxima, or public activation', browser: browser.version(), elapsedMs: Date.now() - started, identities, fixture: {wireSHA256: sha(artifact.wire), inputSHA256: artifact.rows.map(r => r.inputSha256), indexSHA256: artifact.index}, counts: counts(), requests, timings, bounds}))
 await cancelContext.close()
} catch (error) {
 console.error(JSON.stringify({status: 'failed', phase, browser: browser?.version(), elapsedMs: Date.now() - started, identities, counts: counts(), requests, faults, pageErrors, error: error.message, stack: error.stack})); process.exitCode = 1
} finally {
 clearTimeout(watchdog)
 for (const response of pendingResponses) response.destroy()
 await browser?.close()
 await server?.close()
}
