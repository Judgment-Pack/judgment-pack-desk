import assert from 'node:assert/strict'
import {tsImport} from 'tsx/esm/api'
import {chromium} from 'playwright-core'
const {signedDocument}=await tsImport('../src/documents/__fixtures__/signedDocument.ts',import.meta.url)
const {object,pin,reference}=await signedDocument()
const [origin,setupCode]=process.argv.slice(2)
const browser=await chromium.launch({executablePath:'/usr/bin/google-chrome',args:['--no-sandbox']})
const page=await browser.newPage({viewport:{width:1440,height:1000},colorScheme:'dark'})
page.setDefaultTimeout(15000)
const errors=[],models=[]
page.on('pageerror',e=>errors.push(e.message))
page.on('request',r=>{if(r.method()==='POST'&&/\/api\/(codex\/run|assistant\/)/.test(r.url()))models.push(r.url())})
async function api(path,method='GET',body,revision){
 const response=await fetch(origin+path,{method,headers:{Authorization:'Bearer '+setupCode,Origin:origin,'Content-Type':'application/json',...(revision?{'If-Match':revision}:{})},...(body?{body:JSON.stringify(body)}:{})})
 const result=await response.json();assert(response.ok,JSON.stringify(result));return result
}
try{
 // A fixture-only pin verifies the retained signed document; no gateway call.
 await page.route('**/api/desk-config',async route=>{
  const response=await route.fetch(),value=await response.json()
  value.present=true;value.sha256='a'.repeat(64);value.content=JSON.stringify({deskConfigVersion:1,research:{gateway:pin}})
  await route.fulfill({response,json:value})
 })
 await page.goto(origin)
 await page.getByRole('button',{name:'Set up sign-in',exact:true}).click()
 await page.getByLabel('Owner setup code',{exact:true}).fill(setupCode)
 await page.getByRole('button',{name:'Continue',exact:true}).click()
 await page.getByRole('button',{name:'Set password',exact:true}).waitFor()
 await page.goto('about:blank')
 await page.route('**/api/attachments/'+reference.id,route=>route.fulfill({json:object}))
 const chats=await api('/api/conversations'),pack=await api('/api/file?path=example.pack.json')
 const href=`attachment:${reference.id}/${reference.digest}/page/1`
 const text=`The page says [“line one line two”](${href}). A genuine mismatch: [“invented words”](${href}).`
 const state={brief:'Check the source',seedUrls:[],candidates:[],cases:[],expectationIssues:[],unknowns:[],sessions:[],revisionsUsed:0,detail:'',status:'complete',phase:'review',turns:[{id:'request',role:'user',kind:'message',text:'Check the saved source.',at:new Date().toISOString()},{id:'answer',role:'assistant',kind:'message',text,at:new Date().toISOString()}]}
 chats.content.chats.push({id:'citation-preview',title:'Check saved citations',pinned:false,archived:false,updatedAt:new Date().toISOString(),composer:'',model:'',mode:'draft',view:'chat',pack:{id:'example',path:'example.pack.json',digest:pack.sha256},documents:[{id:reference.id,name:'notes.txt',text:'',document:reference}],checkpoint:{state,sources:[]}})
 await api('/api/conversations','PUT',chats.content,chats.sha256)
 for(const placement of ['main','pane']){
  await page.goto(placement==='main'?origin+'/chats/citation-preview':origin+'/packs/example/matrix?chat=citation-preview')
  await page.getByRole('button',{name:'View source 1: notes.txt, page 1',exact:true}).click()
  const popover=page.getByRole('dialog',{name:'notes.txt',exact:true})
  await popover.getByText('Quote found on page 1',{exact:true}).waitFor()
  assert.equal(await popover.locator('mark').innerText(),'line one\nline two')
  assert.equal(await popover.getByRole('button',{name:'Retry',exact:true}).count(),0)
  await page.screenshot({path:`/tmp/desk-citation-preview-${placement}.png`})
  await popover.getByRole('button',{name:'Close',exact:true}).click()
  await page.getByRole('button',{name:'View source 2: notes.txt, page 1',exact:true}).click()
  await popover.getByText('This quote does not match the cited page.',{exact:true}).waitFor()
  assert.equal(await popover.locator('mark').count(),0)
  assert.equal(await popover.getByRole('button',{name:'Retry',exact:true}).count(),0)
  await popover.getByRole('button',{name:'Open source text',exact:true}).click()
  await page.getByText('This quote does not match the cited page.',{exact:true}).waitFor()
  assert(await page.getByText('line one line two',{exact:true}).isVisible())
 }
 assert.deepEqual(errors,[]);assert.deepEqual(models,[])
 console.log(JSON.stringify({mainChat:true,rightPane:true,restoredCitation:true,matchedWrapper:true,exactHighlight:true,trueMismatchRemainsUnverified:true,sourceInspectable:true,noUselessRetry:true,noModelRequests:true}))
}catch(e){await page.screenshot({path:'/tmp/desk-citation-preview-failure.png'});console.error((await page.locator('body').innerText()).slice(-2500));throw e}finally{await browser.close()}
