import assert from 'node:assert/strict'
import {chromium} from 'playwright-core'
const [origin,setupCode]=process.argv.slice(2)
assert(origin&&setupCode,'Use the disposable pack workspace fixture')
const browser=await chromium.launch({executablePath:'/usr/bin/google-chrome',args:['--no-sandbox']})
const page=await browser.newPage({viewport:{width:1440,height:1000},colorScheme:'dark'})
page.setDefaultTimeout(15000)
const errors=[],modelRequests=[]
page.on('pageerror',e=>errors.push(e.message))
page.on('request',r=>{if(r.method()==='POST'&&/\/api\/(codex\/run|assistant\/)/.test(r.url()))modelRequests.push(r.url())})
async function api(path,method='GET',body,revision){
 const response=await fetch(origin+path,{method,headers:{Authorization:'Bearer '+setupCode,Origin:origin,'Content-Type':'application/json',...(revision?{'If-Match':revision}:{})},...(body?{body:JSON.stringify(body)}:{})})
 const result=await response.json();assert(response.ok);return result
}
try {
 await page.goto(origin)
 await page.getByRole('button',{name:'Set up sign-in',exact:true}).click()
 await page.getByLabel('Owner setup code',{exact:true}).fill(setupCode)
 await page.getByRole('button',{name:'Continue',exact:true}).click()
 await page.getByRole('button',{name:'Set password',exact:true}).waitFor()
 const at='2026-10-13T12:00:00Z'
 const step={id:'legacy-tool',name:'propose_input_recipe',status:'failed',inputRecipeFailure:'criteria'}
 const chat={id:'legacy-dialect-history',title:'Existing dialect conversation',input:{id:'list'},pinned:false,archived:false,updatedAt:at,composer:'',model:'',mode:'draft',view:'chat',checkpoint:{state:{status:'complete',brief:'',seedUrls:[],unknowns:[],sessions:[],turns:[{id:'user',role:'user',kind:'message',text:'Keep this earlier conversation',at},{id:'assistant',role:'assistant',kind:'message',text:'This saved conversation is still available.',at}],candidates:[],cases:[],expectationIssues:[],revisionsUsed:0,responses:[{id:'response',messageId:'assistant',afterTurnId:'user',documents:[],websites:[],sourceIds:[],work:{items:[step],notices:[]}}]},sources:[]}}
 const before=await api('/api/conversations')
 await api('/api/conversations','PUT',{...before.content,chats:[...before.content.chats,chat]},before.sha256)
 // Delay the real history read: loading must use the shared spacing and animation.
 let release
 const gate=new Promise(resolve=>{release=resolve})
 await page.route('**/api/conversations',async route=>{if(route.request().method()==='GET')await gate;await route.continue()})
 await page.goto(origin+'/dialects')
 const loading=page.getByRole('status').filter({hasText:/^Loading chats…$/})
 await loading.waitFor()
 assert.equal(await loading.locator('[data-spinner]').count(),1)
 const style=await loading.evaluate(el=>({padding:getComputedStyle(el.parentElement).paddingLeft,font:getComputedStyle(el).fontSize,animation:getComputedStyle(el.querySelector('span:not([data-spinner])')).animationName}))
 assert.equal(style.padding,'16px');assert(style.animation!=='none');assert(parseFloat(style.font)<=13)
 await page.screenshot({path:'/tmp/jps-chat-history-loading.png'})
 await page.setViewportSize({width:980,height:820})
 await page.emulateMedia({colorScheme:'light'})
 assert(await page.evaluate(()=>document.documentElement.scrollWidth<=window.innerWidth))
 const bounds=await loading.boundingBox();assert(bounds&&bounds.x>=0&&bounds.x+bounds.width<=980)
 await page.screenshot({path:'/tmp/jps-shared-loading-narrow.png'})
 await page.setViewportSize({width:1440,height:1000})
 await page.emulateMedia({colorScheme:'dark'})
 await page.emulateMedia({reducedMotion:'reduce'})
 assert.equal(await loading.locator('span:not([data-spinner])').evaluate(el=>getComputedStyle(el).animationName),'none')
 await page.emulateMedia({reducedMotion:'no-preference'})
 release()
 await page.getByText('This saved conversation is still available.',{exact:true}).waitFor()
 await page.unroute('**/api/conversations')
 assert.equal(await page.getByRole('button',{name:/^Chat history unavailable/}).count(),0)
 await page.reload()
 await page.getByText('This saved conversation is still available.',{exact:true}).waitFor()
 await page.goto(origin+'/chats/'+chat.id)
 await page.getByText('This saved conversation is still available.',{exact:true}).waitFor()
 // A failed read must stop loading and preserve a useful Retry in the bottom strip.
 await page.route('**/api/conversations',route=>route.request().method()==='GET'?route.fulfill({status:503,contentType:'application/json',body:JSON.stringify({error:'History temporarily unavailable'})}):route.continue())
 await page.goto(origin+'/dialects')
 const unavailable=page.getByRole('complementary',{name:'Assistant',exact:true}).getByRole('status').filter({hasText:/^Chat history unavailable$/})
 await unavailable.waitFor()
 assert.equal(await unavailable.locator('[data-spinner]').count(),0)
 assert.equal(await page.getByText('Loading chats…',{exact:true}).count(),0)
 assert.equal(await unavailable.locator('span').evaluate(el=>getComputedStyle(el).animationName),'none')
 await page.screenshot({path:'/tmp/jps-chat-history-unavailable.png'})
 await page.unroute('**/api/conversations')
 await page.getByRole('button',{name:/^Chat history unavailable/}).click()
 await page.getByRole('button',{name:'Retry',exact:true}).click()
 await page.getByText('This saved conversation is still available.',{exact:true}).waitFor()
 // The test collection uses the same loading status, without a premature missing-case message.
 let releaseTests
 const testsGate=new Promise(resolve=>{releaseTests=resolve})
 await page.route('**/api/pack-tests',async route=>{if(route.request().method()==='GET')await testsGate;await route.continue()})
 try {
  await page.goto(origin+'/packs/example/evaluate')
  const testsLoading=page.getByRole('status').filter({hasText:/^Loading tests…$/})
  await testsLoading.waitFor()
  assert.equal(await testsLoading.locator('[data-spinner]').count(),1)
  assert.equal(await page.getByText('Case not found',{exact:true}).count(),0)
  assert(await page.evaluate(()=>document.documentElement.scrollWidth<=window.innerWidth))
  await page.screenshot({path:'/tmp/jps-shared-loading-tests.png'})
 } finally {releaseTests();await page.unrouteAll({behavior:'wait'})}
 await page.getByRole('button',{name:'Add case',exact:true}).waitFor()
 const saved=await api('/api/conversations')
 assert.deepEqual(saved.content.chats.find(c=>c.id===chat.id).checkpoint.state.responses[0].work.items[0],step)
 assert.equal(modelRequests.length,0);assert.deepEqual(errors,[])
 console.log(JSON.stringify({legacyChatRestored:true,mainChat:true,dialectPane:true,reload:true,styledLoading:true,narrowLayout:true,testsLoading:true,reducedMotion:true,errorStopsSpinner:true,retryRestoresHistory:true,historicalStepPreserved:true,browserErrors:errors}))
} catch(error) {
 await page.screenshot({path:'/tmp/jps-chat-history-browser-error.png'})
 console.error((await page.locator('body').innerText()).slice(-3000));throw error
} finally {await browser.close()}
