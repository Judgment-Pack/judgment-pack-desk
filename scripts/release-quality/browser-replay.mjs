import assert from 'node:assert/strict'
import {chromium} from '../../web/node_modules/playwright-core/index.mjs'
import {readPrivate,writePrivate,validateControl,validateBrowserReport,captureSettings,captureTools,retainBrowserObservations,digest} from './replay-report.mjs'

// Only a private control file path travels on argv. Never print caught browser
// errors: they can include control values, URLs, source or response bodies.
let control
try{control=validateControl(readPrivate(process.argv[2]))}catch{process.stderr.write('offline control refused\n');process.exit(1)}
const started=Date.now(),deadline=started+Math.min(control.caseDeadlineMs,control.aggregateDeadlineMs)
let result={kind:'offline-mounted-browser-result',provenance:'offline-test',caseId:control.caseId,state:'incomplete',reason:'pending',runId:null,chatId:null,answers:null,tools:null,operations:null,counts:null,settings:null,elapsedMs:0}
writePrivate(control.reportPath,result)
let bootstrapSubphase='launch'
let browser,page,phase='bootstrap',bearer='',starts=0,browserErrors=0,unexpectedRequests=0,runId=null
let timedOut=false,deadlineCleanup
const timer=setTimeout(()=>{
 timedOut=true
 deadlineCleanup=(async()=>{
  try{const stop=page?.getByRole('button',{name:'Stop',exact:true});if(stop&&await stop.count())await stop.first().click({timeout:1500})}catch{}
  try{await browser?.close()}catch{}
 })()
},Math.max(1,deadline-Date.now()))
const left=()=>{const n=deadline-Date.now();assert(n>0);return n}
const pause=()=>new Promise(resolve=>setTimeout(resolve,250))
const api=async path=>{
 assert(bearer&&/^\/api\/(assistant-runs\/arun_[a-f0-9]+|conversations)$/.test(path))
 const response=await page.request.get(control.origin+path,{headers:{Authorization:bearer},timeout:left()})
 assert(response.ok());const text=await response.text();assert(Buffer.byteLength(text)<=4*1024*1024)
 // Application snapshots are transient and never dumped or persisted.
 return JSON.parse(text)
}
try{
 browser=await chromium.launch({executablePath:control.browserBin,args:['--no-sandbox'],timeout:left()})
 bootstrapSubphase='context'
 const context=await browser.newContext({viewport:{width:1440,height:1000},locale:'en-CA'})
 bootstrapSubphase='page'
 page=await context.newPage();page.setDefaultTimeout(Math.min(15000,left()))
 bootstrapSubphase='route'
 await context.route('**/*',async route=>{
  const u=new URL(route.request().url())
  if(u.origin!==control.origin||/\/api\/model-providers\/.*\/(?:login|install|logout)/.test(u.pathname)){
   unexpectedRequests++;await route.abort();return
  }
  await route.continue()
 })
 page.on('pageerror',()=>browserErrors++)
 page.on('request',request=>{
  const path=new URL(request.url()).pathname
  if(request.method()==='POST'&&path==='/api/assistant-runs'){
   starts++;const body=JSON.parse(request.postData()??'null')
   if(starts!==1||body?.purpose!=='chat')unexpectedRequests++
   void request.headerValue('authorization').then(value=>{bearer=value??''})
  }
  if(/^\/api\/(?:agent\/run|codex\/run|assistant\/relay\/)/.test(path))unexpectedRequests++
 })
 page.on('response',response=>{
  if(response.request().method()==='POST'&&new URL(response.url()).pathname==='/api/assistant-runs'){
   void response.json().then(value=>{if(response.status()!==201||!/^arun_[a-f0-9]+$/.test(value.id))unexpectedRequests++;else runId=value.id}).catch(()=>unexpectedRequests++)
  }
 })
 bootstrapSubphase='navigation'
 await page.goto(control.origin,{timeout:left()})
 bootstrapSubphase='owner-setup'
 await page.getByRole('button',{name:'Set up sign-in',exact:true}).click()
 await page.getByLabel('Owner setup code',{exact:true}).fill(control.setupCode)
 await page.getByRole('button',{name:'Continue',exact:true}).click()
 await page.getByRole('button',{name:'Set password',exact:true}).waitFor()
 bootstrapSubphase='owner-navigation'
 await page.goto(control.origin,{timeout:left()})
 phase='settings'
 const composer=page.getByPlaceholder('Ask a question or describe a task…')
 await composer.waitFor()
 await page.getByRole('combobox',{name:'Task tools',exact:true}).click()
 await page.getByRole('option',{name:'Chat',exact:true}).click()
 await page.getByRole('button',{name:/^Model: gpt-6-sol/}).click()
 await page.getByRole('radio',{name:'gpt-6-sol',exact:true}).click()
 const slider=page.getByRole('slider',{name:'Reasoning',exact:true})
 await slider.press('Home');await slider.press('ArrowRight')
 assert.equal(await slider.getAttribute('aria-valuetext'),'Medium')
 await page.keyboard.press('Escape')
 await page.getByRole('button',{name:'Assistant settings',exact:true}).click()
 await page.getByRole('combobox',{name:'Research',exact:true}).click()
 await page.getByRole('option',{name:'Provided sources only',exact:true}).click()
 await page.getByRole('combobox',{name:'Adversarial review',exact:true}).selectOption('off')
 await page.keyboard.press('Escape')
 phase='dispatch'
 await composer.fill(control.prompt);await composer.press('Enter')
 while(!runId){left();assert.equal(unexpectedRequests,0);await pause()}
 result.runId=runId
 phase='terminal'
 let run
 while(true){
  left();assert.equal(unexpectedRequests,0);assert.equal(browserErrors,0)
  run=await api('/api/assistant-runs/'+runId)
  if(['complete','stopped','failed','interrupted','awaiting-input','awaiting-client','paused-budget'].includes(run.state))break
  await pause()
 }
 assert.equal(run.state,'complete');assert.equal(run.checkpoint?.status,'complete');assert.equal(run.chatProjection?.acknowledged,true)
 phase='capture'
 const saved=await api('/api/conversations'),chat=saved.content.chats.find(c=>c.id===run.owner.id)
 assert(chat);result.chatId=chat.id
 const turns=chat.checkpoint.state.turns,user=turns.filter(t=>t.role==='user')
 assert.equal(user.length,1);assert.equal(user[0].text,control.prompt)
 result.captureStage='settings'
 result.settings=captureSettings(chat,run.checkpoint)
 result.captureStage='answers'
 const messages=turns.filter(t=>t.role==='assistant'&&t.kind==='message')
 assert(messages.length>0&&messages.length<=8)
 const pane=page.locator(`[data-chat-id="${chat.id}"]`).first()
 const articles=pane.locator('article[data-role="assistant"][data-kind="message"]')
 await articles.first().waitFor({timeout:left()})
 assert.equal(await articles.count(),messages.length)
 result.answers=[]
 for(let i=0;i<messages.length;i++){
  const article=articles.nth(i),message=messages[i]
  assert.equal(await article.getAttribute('data-message-id'),message.id)
  const renderedText=await article.locator('[class*="markdown"]').first().innerText()
  assert.equal(typeof message.text,'string');assert(message.text.length>0)
  result.answers.push({id:message.id,text:message.text,renderedText,reloadedText:null})
 }
 result.captureStage='reload'
 // Reopen the saved chat; navigation/reload must not create another turn.
 await page.goto(control.origin+'/chats/'+chat.id,{timeout:left()})
 await page.locator('article[data-role="assistant"][data-kind="message"]').first().waitFor({timeout:left()})
 assert.deepEqual(await page.locator('article[data-role="assistant"][data-kind="message"]').evaluateAll(nodes=>nodes.map(n=>n.getAttribute('data-message-id'))),messages.map(m=>m.id))
 const reloaded=await api('/api/conversations')
 const restored=reloaded.content.chats.find(c=>c.id===chat.id)
 assert.deepEqual(restored.checkpoint.state.turns,turns)
 for(let i=0;i<messages.length;i++)result.answers[i].reloadedText=await page.locator('article[data-role="assistant"][data-kind="message"]').nth(i).locator('[class*="markdown"]').first().innerText()
 assert.deepEqual(result.answers.map(a=>a.reloadedText),result.answers.map(a=>a.renderedText))
 result.captureStage='events'
 const calls=(run.events??[]).map(e=>e.event).filter(e=>e.type==='tool_call')
 const tools=(run.events??[]).map(e=>e.event).filter(e=>e.type==='tool_result')
 assert.equal(calls.length,tools.length)
 result.counts={starts,models:run.operations.filter(o=>o.kind==='model').length,callbacks:calls.length,browserErrors,unexpectedRequests}
 result.tools=captureTools(run.events??[])
 assert(!(run.events??[]).some(e=>['error','guardrail','budget_paused'].includes(e.event.type)))
 result.captureStage='operations'
 result.operations=run.operations.map(o=>({id:o.id,callId:o.callId,kind:o.kind,name:o.name,state:o.state,intentSha256:digest(Buffer.from(o.intent,'base64').toString('utf8')),result:o.result}))
 result.captureStage='validation'
 result.state='complete';result.reason='complete';result.elapsedMs=Date.now()-started
 validateBrowserReport(result)
}catch(error){
 if(phase==='bootstrap')result.bootstrapFailure={subphase:bootstrapSubphase,errorName:['Error','TimeoutError','TypeError','AssertionError','RangeError'].includes(error?.name)?error.name:'Other'}
 result.state='incomplete';result.reason=timedOut||Date.now()>=deadline?'deadline':unexpectedRequests?'unexpected-request':browserErrors?'browser-error':phase
}finally{
 clearTimeout(timer)
 // Normal UI Stop cancels the owner; Server.Close is the launcher fallback.
 if(result.state!=='complete'&&page){
  try{const stop=page.getByRole('button',{name:'Stop',exact:true});if(await stop.count())await stop.first().click({timeout:2000})}catch{}
 }
 try{await browser?.close()}catch{result.state='incomplete';result.reason='cleanup'}
 await deadlineCleanup
 result.elapsedMs=Date.now()-started
 if(result.state!=='complete')result=retainBrowserObservations(result)
 try{validateBrowserReport(result);writePrivate(control.reportPath,result)}catch{process.stderr.write('offline browser report refused\n');process.exitCode=1}
}
if(result.state!=='complete'){process.stderr.write('offline browser incomplete: '+result.reason+'\n');process.exitCode=1}else process.stdout.write('offline browser complete\n')
