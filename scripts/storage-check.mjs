import { createRequire } from 'node:module'
import { randomBytes, createHash } from 'node:crypto'
import { cp, mkdtemp, mkdir, readFile, writeFile, rm, stat } from 'node:fs/promises'
import { spawn } from 'node:child_process'
import { once } from 'node:events'
import { resolve } from 'node:path'
import assert from 'node:assert/strict'
const root = resolve(new URL('..', import.meta.url).pathname)
const [binary, fixture, output = '/tmp/jp-storage-artifacts'] = process.argv.slice(2)
if (!binary || !fixture || !process.env.JPACK_BIN) throw new Error('Supply Desk, fixture and JPACK_BIN')
const { chromium } = createRequire(`${root}/web/package.json`)('playwright-core')
const work = await mkdtemp('/tmp/jp-storage-browser-')
await cp(fixture, `${work}/project`, { recursive: true }); await mkdir(output, { recursive: true }); await mkdir(`${work}/config`)
const secret = randomBytes(24).toString('hex'), origin = 'http://127.0.0.1:8851'
const server = spawn(binary, ['--dev-token', secret, '--port', '8851', '--jpack', process.env.JPACK_BIN, `${work}/project`], { env: { ...process.env, XDG_CONFIG_HOME: `${work}/config`, XDG_DATA_HOME: `${work}/data` }, stdio: 'ignore' })
const api = (path, init = {}) => fetch(origin + path, { ...init, headers: { Authorization: `Bearer ${secret}`, ...init.headers } })
let browser
const errors = [], results = []
try {
 let ready = false
 for (let i=0;i<100;i++) { try { if ((await api('/api/storage')).ok) { ready=true;break } } catch {} await new Promise(resolve => setTimeout(resolve,100)) }
 assert(ready, 'Desk starts with isolated storage')
 browser = await chromium.launch({ executablePath: process.env.PLAYWRIGHT_CHROME ?? '/usr/bin/google-chrome', headless: true, args: ['--no-sandbox'] })
 const page = await browser.newPage({ viewport: { width: 1440, height: 1000 }, colorScheme: 'dark', acceptDownloads: true })
 page.setDefaultTimeout(15000)
 page.on('pageerror', error => errors.push(error.message.replaceAll(secret,'[redacted]')))
 await page.goto(`${origin}/launch?secret=${secret}`)
 await page.getByRole('heading',{ name:'What would you like to work on?' }).waitFor()
 const saved = await (await api('/api/conversations')).json()
 assert.equal(saved.content.chats.length,0)
 const now=new Date().toISOString()
 const body={ version:1,chats:[{id:'storage-chat',title:'Retained storage test',updatedAt:now,pinned:false,archived:false,composer:'A saved draft',model:'',mode:'draft',view:'chat',attachments:[{id:'source',name:'policy.txt',text:'Retained source material'}]}] }
 assert.equal((await api('/api/conversations',{method:'PUT',headers:{'If-Match':saved.sha256,'Content-Type':'application/json'},body:JSON.stringify(body)})).status,200)
 await page.goto(origin+'/admin#storage')
 await page.getByRole('heading',{name:'Storage & data',exact:true}).waitFor()
 await page.getByRole('heading',{name:'Project files',exact:true}).waitFor()
 const change=page.getByRole('button',{name:'Change location…',exact:true})
 await change.waitFor()
 await page.screenshot({path:`${output}/storage-dark.png`,fullPage:true})
 const before=await (await api('/api/storage')).json()
 const target=`${work}/moved/private chat data`
 await change.click()
 await page.getByLabel('New folder',{exact:true}).fill(target)
 await page.screenshot({path:`${output}/move-dialog-dark.png`})
 await page.getByRole('button',{name:'Cancel',exact:true}).click()
 assert.equal((await (await api('/api/storage')).json()).path,before.path)
 assert(await change.evaluate(element=>element===document.activeElement))
 await change.click(); await page.getByLabel('New folder',{exact:true}).fill(target)
 await page.getByRole('button',{name:'Move data',exact:true}).click()
 await page.getByText('Chat data moved. The original folder was kept as a recovery copy.',{exact:true}).waitFor()
 const after=await (await api('/api/storage')).json()
 assert.equal(after.path,target);assert.equal(after.previousPath,before.path)
 const reply=await (await api('/api/conversations')).json()
 assert.deepEqual(reply.content,body)
 assert.equal((await stat(target)).mode & 0o777,0o700)
 results.push('Private location, cancel/focus restoration, verified move and original recovery copy')
 const downloadPromise=page.waitForEvent('download')
 await page.getByRole('button',{name:'Download chat backup',exact:true}).click()
 const download=await downloadPromise
 const backupPath=`${work}/chat-backup.zip`;await download.saveAs(backupPath)
 assert.equal(await download.failure(),null)
 const backup=await readFile(backupPath)
 assert(backup.includes(Buffer.from('Retained source material')))
 results.push('Backup downloads saved source text and chat data')
 await page.getByRole('button',{name:'Restore backup…',exact:true}).click()
 await page.getByLabel('Chat backup',{exact:true}).setInputFiles({name:'broken.zip',mimeType:'application/zip',buffer:Buffer.from('not a ZIP')})
 await page.getByLabel('Restore into',{exact:true}).fill(`${work}/restored`)
 await page.screenshot({path:`${output}/restore-dialog-dark.png`})
 await page.getByRole('button',{name:'Restore and reload',exact:true}).click()
 await page.getByRole('alert').waitFor()
 assert.equal((await (await api('/api/storage')).json()).path,target)
 await page.getByLabel('Chat backup',{exact:true}).setInputFiles(backupPath)
 await page.getByRole('button',{name:'Restore and reload',exact:true}).click()
 await page.waitForFunction(path => [...document.querySelectorAll('code')].some(node=>node.textContent===path),`${work}/restored`)
 assert.deepEqual((await (await api('/api/conversations')).json()).content,body)
 results.push('Corrupt restore keeps current data; valid restore reloads with the same saved history')
 const currentRecord=`conversations-${createHash('sha256').update(reply.project).digest('hex')}.json`
 const previousProject=`${work}/previous-project`
 const oldRecord=`conversations-${createHash('sha256').update(previousProject).digest('hex')}.json`
 await writeFile(`${work}/restored/${oldRecord}`,JSON.stringify(body),{mode:0o600})
 await rm(`${work}/restored/${currentRecord}`)
 await page.reload()
 await page.getByRole('button',{name:'Recover project history…',exact:true}).click()
 await page.getByLabel('Previous project folder',{exact:true}).fill(previousProject)
 await page.getByRole('button',{name:'Find history',exact:true}).click()
 await page.getByText('1 saved chat found.',{exact:true}).waitFor()
 assert.equal((await (await api('/api/conversations')).json()).sha256,'absent')
 await page.getByRole('button',{name:'Link history and reload',exact:true}).click()
 await page.getByRole('dialog',{name:'Recover project history',exact:true}).waitFor({state:'hidden'})
 assert.deepEqual((await (await api('/api/conversations')).json()).content,body)
 results.push('Moved project history requires a preview and explicit link before reappearing')
 for (const width of [1440,1024,640,390]) {
  await page.setViewportSize({width,height:900})
  await page.emulateMedia({colorScheme:width===1440||width===640?'light':'dark'})
  await page.locator('#main').evaluate(node=>node.scrollTop=0)
  assert(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth+1),'No page overflow at '+width)
  const main=page.locator('#main');assert(await main.evaluate(node=>node.scrollWidth<=node.clientWidth+1),'No main overflow at '+width)
  await page.screenshot({path:`${output}/storage-${width}.png`,fullPage:true})
  await page.getByRole('button',{name:'Change location…',exact:true}).click()
  const dialog=page.getByRole('dialog',{name:'Move chat data',exact:true})
  assert(await dialog.evaluate(node=>node.scrollWidth<=node.clientWidth+1),'No dialog overflow at '+width)
  const bounds=await dialog.boundingBox();assert(bounds.x>=0&&bounds.x+bounds.width<=width+1)
  await page.keyboard.press('Escape')
 }
 results.push('Neutral light/dark settings and dialogs fit 1440, 1024, 640 and 390px widths')
 assert.deepEqual(errors,[])
 await writeFile(`${output}/results.json`,JSON.stringify({results,errors},null,2))
 console.log(JSON.stringify({passed:results.length,results,output},null,2))
} finally {
 if(browser)await browser.close()
 if(server.exitCode===null){server.kill('SIGTERM');await once(server,'exit')}
 await rm(work,{recursive:true,force:true})
}
