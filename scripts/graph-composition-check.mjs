// Run with: JPACK_BIN=/path/jpack node scripts/graph-composition-check.mjs /path/jpack-desk /path/runtime/internal/graph/testdata/composition
// Creates a disposable project and Desk profile. Does not call a model provider.
import { createRequire } from 'node:module'
import { randomBytes } from 'node:crypto'
import { mkdtemp, readFile, writeFile, mkdir, cp, rm } from 'node:fs/promises'
import { spawn } from 'node:child_process'
import assert from 'node:assert/strict'
import { dirname, resolve, join } from 'node:path'
import { fileURLToPath } from 'node:url'
import { tmpdir } from 'node:os'
import { once } from 'node:events'
const root=resolve(dirname(fileURLToPath(import.meta.url)),'..'), [binary,fixture]=process.argv.slice(2)
if(!binary||!fixture||!process.env.JPACK_BIN) throw new Error('Provide Desk, Runtime, and the Runtime composition fixture directory.')
const {chromium}=createRequire(`${root}/web/package.json`)('playwright-core')
const work=await mkdtemp(join(tmpdir(),'jp-composition-')),secret=randomBytes(24).toString('hex'),base='http://127.0.0.1:8813'
await cp(fixture,`${work}/project`,{recursive:true});await mkdir(`${work}/config`)
const server=spawn(binary,['--dev-token',secret,'--port','8813','--jpack',process.env.JPACK_BIN,`${work}/project`],{env:{...process.env,XDG_CONFIG_HOME:`${work}/config`,XDG_DATA_HOME:`${work}/data`},stdio:'ignore'})
let browser,page
const errors=[],calls=[]
try{
  for(let i=0;i<100;i++){assert.equal(server.exitCode,null);try{if((await fetch(`${base}/api/desk-config`,{headers:{Authorization:`Bearer ${secret}`}})).ok)break}catch{}await new Promise(r=>setTimeout(r,100))}
  browser=await chromium.launch({executablePath:process.env.PLAYWRIGHT_CHROME??'/opt/google/chrome/chrome',args:['--no-sandbox']})
  page=await browser.newPage({viewport:{width:1540,height:1080},colorScheme:'dark'});page.setDefaultTimeout(20000)
  page.on('pageerror',e=>errors.push(e.message));page.on('request',r=>{if(r.url().includes('/api/graphs/'))calls.push({path:new URL(r.url()).pathname,method:r.method()})})
  await page.goto(`${base}/launch?secret=${secret}`,{waitUntil:'domcontentloaded'});await page.locator('.desk').waitFor();await page.waitForFunction(()=>sessionStorage.length>0)
  await page.goto(`${base}/packs`)
  const library=page.getByRole('navigation',{name:'Decisions',exact:true})
  await library.getByRole('link',{name:/fan-in/}).waitFor();await library.getByRole('link',{name:/sanctions-screening/}).waitFor()
  await page.getByRole('radio',{name:'Graphs',exact:true}).click();assert.equal(await library.getByRole('link').count(),2)
  await page.getByRole('radio',{name:'Packs',exact:true}).click();await page.getByRole('checkbox',{name:'Select sanctions-screening for composition'}).check()
  await page.getByRole('link',{name:/Compose graph/}).click()
  await page.getByLabel('Configured graph id').fill('browser-composition');assert.equal(await page.getByLabel('Graph path').inputValue(),'browser-composition.graph.json')
  const choose=async(label,value)=>{await page.getByRole('combobox',{name:label,exact:true}).click();await page.getByRole('option',{name:value,exact:true}).click()}
  await choose('Add pack','vendor-onboarding');await page.getByRole('button',{name:'Add',exact:true}).click()
  await choose('Result node','vendor-onboarding')
  await choose('From','sanctions-screening');await choose('To','vendor-onboarding')
  await page.getByLabel('Fact destination',{exact:true}).fill('/screening/status');await page.getByLabel('Evidence requirement',{exact:true}).fill('screening-outcome')
  await page.getByRole('button',{name:'Connect',exact:true}).click()
  await page.getByRole('button',{name:'Undo',exact:true}).click();await page.getByRole('button',{name:'Redo',exact:true}).click()
  assert.equal(calls.filter(r=>r.path.endsWith('/write')).length,0)
  await page.getByRole('button',{name:'Keep draft',exact:true}).click();await page.waitForURL(url=>url.searchParams.has('draft'))
  const draftURL=page.url();await page.reload();await page.getByLabel('Configured graph id').waitFor();assert.equal(await page.getByLabel('Configured graph id').inputValue(),'browser-composition')
  await page.getByRole('button',{name:'Validate',exact:true}).click();await page.getByLabel('Runtime findings',{exact:true}).waitFor()
  await page.getByRole('button',{name:'Review graph write',exact:true}).click();await page.getByRole('button',{name:'Confirm graph write',exact:true}).waitFor()
  assert.equal(calls.filter(r=>r.path.endsWith('/write')).length,0)
  await page.getByRole('button',{name:'Confirm graph write',exact:true}).click();await page.waitForURL(url=>url.pathname==='/graphs/browser-composition')
  const written=JSON.parse(await readFile(`${work}/project/browser-composition.graph.json`,'utf8'))
  assert.equal(written.id,'browser-composition');assert.equal(written.result,'vendor-onboarding');assert.deepEqual(written.edges,[{from:'sanctions-screening',to:'vendor-onboarding',fact:'/screening/status',evidence:{id:'screening-outcome',onUnresolved:'unknown'}}])
  assert.equal(JSON.parse(await readFile(`${work}/project/jpack.json`,'utf8')).graphs['browser-composition'].path,'browser-composition.graph.json')
  await page.goto(`${base}/graphs/fan-in?view=rehearsal`)
  const rows=JSON.parse(await readFile(`${work}/project/fan-in.rows.json`,'utf8'))
  await page.getByLabel('Inputs by node id').fill(JSON.stringify(rows.cases[0].inputs));await page.getByRole('button',{name:'Rehearse',exact:true}).click()
  await page.getByText('The evaluated graph, configuration and packs match the files just read.',{exact:true}).waitFor()
  const pack=`${work}/project/sanctions-screening-0.1.0.pack.json`;await writeFile(pack,(await readFile(pack,'utf8'))+'\n')
  await page.getByRole('button',{name:'Check revisions',exact:true}).click();await page.getByText('The project has changed since this rehearsal. Run again to test the current files.',{exact:true}).waitFor()
  await page.goto(`${base}/graphs/browser-composition?view=compose`)
  await page.getByLabel('Configured graph id').waitFor()
  for(const theme of ['dark','light']){
    await page.emulateMedia({colorScheme:theme});await page.screenshot({path:`/tmp/jps-composition-${theme}.png`,fullPage:true})
    await page.setViewportSize({width:390,height:844})
    assert(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth),`page overflow at 390px in ${theme}`)
    await page.screenshot({path:`/tmp/jps-composition-${theme}-narrow.png`,fullPage:true});await page.setViewportSize({width:1540,height:1080})
  }
  await page.goto(draftURL);await page.getByLabel('Configured graph id').waitFor()
  assert.deepEqual(errors,[])
  console.log(JSON.stringify({passed:true,checks:['combined browsing','type filters','pack selection','form composition','undo/redo','retained draft reload','explicit exact write','rehearsal revision binding','stale result detection','light/dark','390px no page overflow'],graphRequests:calls.length}))
} catch(error){if(page){await page.screenshot({path:'/tmp/jps-composition-failure.png',fullPage:true});console.error((await page.locator('body').innerText()).slice(0,12000))}throw error}
finally{if(browser)await browser.close();if(server.pid&&server.exitCode===null&&server.signalCode===null){const exited=once(server,'exit');server.kill('SIGTERM');await exited}await rm(work,{recursive:true,force:true})}
