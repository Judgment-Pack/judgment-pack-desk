import assert from 'node:assert/strict'
import {chromium} from 'playwright-core'
const [origin,setupCode]=process.argv.slice(2)
const browser=await chromium.launch({executablePath:'/usr/bin/google-chrome',args:['--no-sandbox']})
const page=await browser.newPage({viewport:{width:1440,height:960},colorScheme:'dark'})
page.setDefaultTimeout(15000)
const errors=[],models=[],checked=[]
page.on('pageerror',e=>errors.push(e.message))
page.on('request',r=>{if(r.method()==='POST'&&/\/api\/(codex\/run|assistant\/)/.test(r.url()))models.push(r.url())})
async function api(path,method='GET',body,revision){
 const r=await fetch(origin+path,{method,headers:{Authorization:'Bearer '+setupCode,Origin:origin,'Content-Type':'application/json',...(revision?{'If-Match':revision}:{})},...(body?{body:JSON.stringify(body)}:{})})
 const value=await r.json();assert(r.ok,JSON.stringify(value));return value
}
let hold
const byPath=path=>request=>request.method()==='GET'&&new URL(request.url()).pathname===path
const byTool=(...tools)=>request=>{try{return tools.includes(request.postDataJSON()?.params?.name)}catch{return false}}
function delay(matches,fail=false){
 let release;const promise=new Promise(resolve=>{release=resolve})
 const state={matches,fail,promise,release,count:0};hold=state;return state
}
async function settle(state){state.release();if(hold===state)hold=undefined;await page.locator('[data-page-loading]:visible').waitFor({state:'hidden'})}
async function centered(label){
 const loading=page.locator('[data-page-loading]:visible')
 await loading.waitFor();assert.equal(await loading.count(),1)
 const status=loading.getByRole('status');assert.equal(await status.innerText(),label)
 assert.equal(await loading.locator('[data-spinner]').count(),1)
 assert.equal(await loading.getByRole('status').count(),1)
 assert.equal(await page.locator('[data-collection-layout] > :first-child [data-page-loading]').count(),0)
 const bounds=await loading.boundingBox(),message=await status.boundingBox()
 assert(bounds.height>120,JSON.stringify({label,bounds}))
 assert(Math.abs((bounds.x+bounds.width/2)-(message.x+message.width/2))<2)
 assert(Math.abs((bounds.y+bounds.height/2)-(message.y+message.height/2))<2)
 assert(message.x>=0&&message.x+message.width<=page.viewportSize().width)
 assert.equal(await status.evaluate(el=>getComputedStyle(el).fontSize),'12px')
 assert.equal(await page.evaluate(()=>document.documentElement.scrollWidth>innerWidth),false)
}
try{
 await page.goto(origin)
 await page.getByRole('button',{name:'Set up sign-in',exact:true}).click()
 await page.getByLabel('Owner setup code',{exact:true}).fill(setupCode)
 await page.getByRole('button',{name:'Continue',exact:true}).click()
 await page.getByRole('button',{name:'Set password',exact:true}).waitFor()
 await page.goto('about:blank')
 const saved=await api('/api/input-preparations','POST',{name:'Public notices',recipe:{intent:'Review public notices'}})
 await api('/api/input-preparations/'+saved.document.id+'/imports','POST',{kind:'csv',name:'notices.csv',content:'Name,Status\nSupplied notice,Open\n'},saved.sha256)
 await page.routeWebSocket('**/ws',socket=>{
  const server=socket.connectToServer()
  socket.onMessage(async message=>{
   let body;try{body=JSON.parse(message.toString())}catch{}
   const state=hold
   if(state?.matches({method:()=> 'MCP',postDataJSON:()=>body})) {state.count++;await state.promise}
   server.send(message)
  })
 })
 await page.route('**/*',async route=>{
  const state=hold
  if(state?.matches(route.request())){
   state.count++;await state.promise
   if(state.fail)return route.fulfill({status:503,json:{error:'Temporarily unavailable'}})
  }
  if(['/api/operations/jobs','/api/operations/runs'].includes(new URL(route.request().url()).pathname)&&route.request().method()==='GET')return route.fulfill({json:{items:[]}})
  await route.continue().catch(()=>{})
 })
 for(const [name,path,label,matches,ready] of [
  ['library','/library','Loading library…',byTool('list_packs','experimental_list_graphs'),()=>page.locator('[data-pack-list] a[data-row]').filter({hasText:'example'}).first()],
  ['dialects','/dialects','Loading dialects…',byPath('/api/input-preparations'),()=>page.getByRole('link',{name:'Public notices',exact:true})],
  ['dialect','/dialects/'+saved.document.id,'Loading dialect…',byPath('/api/input-preparations/'+saved.document.id),()=>page.getByRole('button',{name:'Supplied notice',exact:true})],
  ['graphs','/graphs','Loading graphs…',byTool('experimental_list_graphs'),()=>page.getByRole('button',{name:'Run all graph tests',exact:true})],
  ['jobs','/jobs','Loading jobs…',byPath('/api/operations/jobs'),()=>page.getByText('No jobs yet',{exact:true})],
  ['runs','/jobs/runs','Loading runs…',byPath('/api/operations/runs'),()=>page.getByText('No runs yet',{exact:true})],
  ['pack','/packs/example','Loading pack…',byTool('get_pack'),()=>page.getByRole('link',{name:'Overview',exact:true})],
  ['tests','/packs/example/matrix','Loading pack…',byTool('get_pack'),()=>page.getByRole('link',{name:'saved-case',exact:true})]
 ]){
  const state=delay(matches)
  await page.goto(origin+path,{waitUntil:'domcontentloaded'})
  await centered(label);assert(state.count>0);console.log('Loading checked: '+name)
  assert(await page.locator('[data-page-header]:visible').first().isVisible())
  if(name==='graphs')assert(await page.getByRole('button',{name:'Run all graph tests',exact:true}).isVisible())
  if(name==='library'||name==='dialects'){
   await page.screenshot({path:`/tmp/desk-page-loading-${name}.png`})
   const collapse=page.locator('button[aria-controls="desk-inspector"][aria-expanded="true"]:visible')
   if(await collapse.count())await collapse.first().click()
   await page.setViewportSize({width:720,height:640});await centered(label)
   await page.emulateMedia({reducedMotion:'reduce',colorScheme:'light'})
   const animations=await page.locator('[data-page-loading] *').evaluateAll(elements=>elements.map(el=>getComputedStyle(el).animationName))
   assert(animations.every(name=>name==='none'))
   await page.screenshot({path:`/tmp/desk-page-loading-${name}-narrow.png`})
   await page.setViewportSize({width:1440,height:960});await page.emulateMedia({reducedMotion:'no-preference',colorScheme:'dark'})
  }
  await settle(state);await ready().waitFor();checked.push(name)
 }
 // A failed first read ends loading and stays distinct from an empty collection.
 const failed=delay(byPath('/api/input-preparations'),true)
 await page.goto(origin+'/dialects',{waitUntil:'domcontentloaded'});await centered('Loading dialects…')
 await settle(failed);await page.getByRole('heading',{name:'Dialects unavailable',exact:true}).waitFor()
 assert.equal(await page.getByText('Prepare data for your decisions',{exact:true}).count(),0)
 // Refreshing an already loaded collection keeps its rows while waiting and on failure.
 await page.reload();await page.getByRole('link',{name:'Public notices',exact:true}).waitFor()
 const refresh=delay(byPath('/api/input-preparations'),true)
 await page.waitForFunction(()=>true)
 const until=Date.now()+10000;while(!refresh.count&&Date.now()<until)await page.waitForTimeout(100)
 assert(refresh.count>0);assert.equal(await page.locator('[data-page-loading]:visible').count(),0)
 assert(await page.getByRole('link',{name:'Public notices',exact:true}).isVisible())
 refresh.release();hold=undefined
 await page.getByRole('button',{name:'Dialects unavailable',exact:false}).waitFor()
 assert(await page.getByRole('link',{name:'Public notices',exact:true}).isVisible())
 assert.equal(await page.locator('[data-page-loading]:visible').count(),0)
 assert.deepEqual(errors,[]);assert.deepEqual(models,[])
 console.log(JSON.stringify({checked,centered:true,stableChrome:true,narrowViewport:true,reducedMotion:true,firstLoadFailure:true,retainedOnRefresh:true,noModelRequests:true}))
}catch(error){hold?.release();await page.screenshot({path:'/tmp/desk-page-loading-failure.png'});console.error((await page.locator('body').innerText()).slice(-2500));throw error}finally{hold?.release();await browser.close()}
