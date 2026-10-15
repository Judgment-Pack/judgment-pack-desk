import assert from 'node:assert/strict'
import {createHash} from 'node:crypto'
import {chromium} from 'playwright-core'
const [origin,setupCode]=process.argv.slice(2)
const browser=await chromium.launch({executablePath:'/usr/bin/google-chrome',args:['--no-sandbox']})
const page=await browser.newPage({viewport:{width:1440,height:900},colorScheme:'dark'})
page.setDefaultTimeout(15000)
const errors=[];page.on('pageerror',e=>errors.push(e.message))
async function api(path,method='GET',body,revision){const response=await fetch(origin+path,{method,headers:{Authorization:'Bearer '+setupCode,Origin:origin,'Content-Type':'application/json',...(revision?{'If-Match':revision}:{})},...(body?{body:JSON.stringify(body)}:{})});const result=await response.json();assert(response.ok,JSON.stringify(result));return result}
try{
 await page.goto(origin)
 await page.getByRole('button',{name:'Set up sign-in',exact:true}).click()
 await page.getByLabel('Owner setup code',{exact:true}).fill(setupCode)
 await page.getByRole('button',{name:'Continue',exact:true}).click()
 await page.getByRole('button',{name:'Set password',exact:true}).waitFor()
 await page.goto(origin)
 const main=page.locator('[data-placement="main"]')
 const composer=page.getByPlaceholder('Ask a question or describe a task…')
 await composer.fill('Explain the retained work.');await composer.press('Enter')
 await main.getByText('Finishing response from completed work…',{exact:true}).waitFor()
 await main.getByText('Recovery completed using retained work.',{exact:true}).waitFor()
 await main.getByRole('button',{name:'Stop',exact:true}).waitFor({state:'hidden'})
 assert.equal(await main.getByRole('article',{name:'Response interrupted'}).count(),0)
 await page.waitForTimeout(800)
 const saved=await api('/api/conversations'),chat=saved.content.chats[0]
 assert.equal(chat.checkpoint.state.execution.used,2)
 assert.equal(chat.checkpoint.state.execution.completions[0].result.finishReason,'length')
 assert.equal(chat.checkpoint.state.execution.completions[0].result.reasoningTokens,8192)
 assert.equal(chat.checkpoint.state.execution.completions[1].phase,'synthesis')
 await page.goto('about:blank')
 chat.inputDrafts=[{version:1,draftId:'fixture-dialect',createdAt:new Date().toISOString(),name:'Fixture dialect',intent:'Retain supplied records',target:'records',fields:[{name:'name',type:'string',description:'Supplied name'}],criteria:[],limits:{records:1,searches:0,pages:0,modelCalls:1}}]
 // Retained checks on an earlier draft reproduce refresh-time recovery loss.
 const identity=value=>JSON.stringify(sort(value))
 const sort=value=>Array.isArray(value)?value.map(sort):value&&typeof value==='object'?Object.fromEntries(Object.entries(value).sort(([a],[b])=>a<b?-1:a>b?1:0).map(([key,member])=>[key,sort(member)])):value
 const digest=value=>createHash('sha256').update(value).digest('hex')
 const text=JSON.stringify({specVersion:'0.2.0-draft',id:'https://example.org/retained',version:'1.0.0',title:'Earlier draft',decision:{intent:'Review work',question:'Accept?'},outcomes:[{id:'accept',label:'Accept'}],rules:[],fallbackOutcome:'accept'})
 const tool={name:'jpack',version:'0.1.0'}
 const record={version:1,at:new Date().toISOString(),documentDigest:digest(text),inputs:[],runtime:identity({tool}),context:'0'.repeat(64),statements:[],validation:{content:[{type:'text',text:JSON.stringify({tool,status:'valid',diagnostics:[]})}]}}
 record.digest=digest(identity(record))
 chat.mode='draft'
 chat.checkpoint.state.candidates=[{text,revision:1,producedBy:'conversation',responseId:chat.checkpoint.state.responses[0].id,previousCheck:{documentDigest:digest(text),valid:true,diagnostics:[],cases:[],record}}]
 chat.checkpoint.state.checkRun='complete'
 await api('/api/conversations','PUT',saved.content,saved.sha256)
 await page.goto(origin+'/dialects?chat='+chat.id)
 const pane=page.locator('[data-placement="pane"]')
 await pane.getByText('Explain the retained work.',{exact:true}).waitFor()
 const paneComposer=pane.locator('textarea').last()
 await paneComposer.fill('Check the current task.');await paneComposer.press('Enter')
 let recovery=pane.getByRole('article',{name:'Response interrupted'})
 await recovery.getByText('The AI reached its output-token limit before completing the response. Completed work is retained.',{exact:true}).waitFor()
 assert.equal(await pane.getByRole('article',{name:'Response interrupted'}).count(),1)
 await page.waitForTimeout(800)
 await page.reload()
 recovery=pane.getByRole('article',{name:'Response interrupted'})
 await recovery.waitFor()
 // Let runtime reconnect and draft verification settle before asserting controls.
 await page.waitForTimeout(1500)
 await recovery.getByRole('button',{name:'Retry response',exact:true}).waitFor()
 await recovery.locator('summary').filter({hasText:'Technical details'}).click()
 await recovery.getByText('reasoningTokens',{exact:false}).waitFor()
 const failed=await api('/api/conversations'),state=failed.content.chats.find(c=>c.id===chat.id).checkpoint.state
 assert.equal(state.failure.code,'output-limit')
 assert.equal(state.failure.completion.finishReason,'length')
 assert.equal(state.execution.used,2)
 assert.equal(state.execution.limit,20)
 // The same failed response also survives refresh in the full-page chat.
 await page.goto(origin+'/chats/'+chat.id)
 recovery=main.getByRole('article',{name:'Response interrupted'})
 await recovery.waitFor();await page.reload();await page.waitForTimeout(1500)
 await recovery.getByRole('button',{name:'Retry response',exact:true}).waitFor()
 for(const theme of ['light','dark'])for(const density of ['comfortable','compact']){
  await page.evaluate(({theme,density})=>{document.documentElement.dataset.theme=theme;document.documentElement.dataset.density=density},{theme,density})
  const button=recovery.getByRole('button',{name:'Retry response',exact:true})
  const geometry=await button.evaluate(element=>({height:element.getBoundingClientRect().height,font:getComputedStyle(element).fontSize,width:element.getBoundingClientRect().width,right:element.getBoundingClientRect().right,icons:element.querySelectorAll('svg').length}))
  const bounds=await recovery.boundingBox()
  assert.equal(geometry.height,density==='compact'?28:32)
  assert.equal(geometry.font,'13px');assert.equal(geometry.icons,1)
  assert(geometry.width<bounds.width/2);assert(Math.abs(geometry.right-(bounds.x+bounds.width))<2)
 }
 await page.evaluate(()=>{document.documentElement.dataset.theme='dark';document.documentElement.dataset.density='comfortable'})
 await recovery.screenshot({path:'/tmp/desk-retry-main-dark.png'})
 await page.goto(origin+'/dialects?chat='+chat.id)
 recovery=pane.getByRole('article',{name:'Response interrupted'})
 await recovery.screenshot({path:'/tmp/desk-retry-pane-dark.png'})
 await recovery.getByRole('button',{name:'Retry response',exact:true}).click()
 await pane.getByRole('button',{name:'Stop',exact:true}).waitFor({state:'hidden'})
 await recovery.waitFor({state:'hidden'})
 await page.waitForTimeout(800)
 const final=await api('/api/conversations'),completed=final.content.chats.find(c=>c.id===chat.id).checkpoint.state
 assert.equal(completed.turns.filter(t=>t.role==='user').length,2)
 assert(!completed.failure)
 assert.equal(completed.execution.used,3)
 // A failure exactly at the request ceiling needs an explicit grant, not a vanished action.
 await page.goto('about:blank')
 const exhausted=await api('/api/conversations'),held=exhausted.content.chats.find(c=>c.id===chat.id)
 held.checkpoint.state=structuredClone(state);held.checkpoint.state.execution.used=20
 await api('/api/conversations','PUT',exhausted.content,exhausted.sha256)
 await page.goto(origin+'/dialects?chat='+chat.id);await page.reload();await page.waitForTimeout(1500)
 recovery=pane.getByRole('article',{name:'Response interrupted'})
 const extend=recovery.getByRole('button',{name:'Retry · +10 requests',exact:true})
 await extend.waitFor()
 assert.equal(await recovery.getByRole('button',{name:'Retry response',exact:true}).count(),0)
 await recovery.screenshot({path:'/tmp/desk-retry-extension-dark.png'})
 await extend.click();await recovery.waitFor({state:'hidden'})
 await pane.getByRole('button',{name:'Stop',exact:true}).waitFor({state:'hidden'})
 await page.waitForTimeout(800)
 const granted=await api('/api/conversations'),resumed=granted.content.chats.find(c=>c.id===chat.id).checkpoint.state
 assert.equal(resumed.execution.used,21);assert.equal(resumed.execution.limit,30);assert.equal(resumed.execution.grants.length,1)
 assert.equal(resumed.turns.filter(t=>t.role==='user').length,2)
 // After repeated observations, a different action must remain available.
 await paneComposer.fill('Use retained evidence to complete the task.');await paneComposer.press('Enter')
 await pane.getByText('The different action completed using retained results.',{exact:true}).waitFor()
 await pane.getByRole('button',{name:'Stop',exact:true}).waitFor({state:'hidden'})
 await page.waitForTimeout(800)
 const useful=(await api('/api/conversations')).content.chats.find(c=>c.id===chat.id).checkpoint.state
 assert(!useful.failure);assert.equal(useful.execution.used,6)
 assert.equal(useful.execution.operations.length,5)
 assert(useful.execution.operations.every(op=>op.status==='complete'))
 // If repetition continues, retain the refused call, the cause and retry after reload.
 await paneComposer.fill('Check another current task.');await paneComposer.press('Enter')
 let stopped=pane.getByRole('article',{name:'Stopped repeating work'})
 await stopped.waitFor()
 await pane.getByRole('button',{name:'Stop',exact:true}).waitFor({state:'hidden'})
 await page.waitForTimeout(800);await page.reload();await page.waitForTimeout(1500)
 await stopped.getByRole('button',{name:'Retry response',exact:true}).waitFor()
 const stalled=(await api('/api/conversations')).content.chats.find(c=>c.id===chat.id).checkpoint.state
 assert.equal(stalled.failure.code,'repeated-work');assert.equal(stalled.execution.used,6)
 assert.equal(stalled.execution.operations.length,6);assert.deepEqual(stalled.execution.grants,[])
 assert(stalled.execution.operations.slice(0,4).every(op=>op.status==='complete'))
 for(const refused of stalled.execution.operations.slice(4)){
  assert.equal(refused.name,'list_context');assert.equal(refused.args.offset,100000)
  assert.equal(refused.status,'failed');assert.equal(refused.result.structuredContent.failure.outcome,'not-started')
 }
 await stopped.screenshot({path:'/tmp/desk-stall-pane-dark.png'})
 await page.goto(origin+'/chats/'+chat.id);await page.reload();await page.waitForTimeout(1500)
 stopped=main.getByRole('article',{name:'Stopped repeating work'})
 await stopped.getByRole('button',{name:'Retry response',exact:true}).waitFor()
 await stopped.screenshot({path:'/tmp/desk-stall-main-dark.png'})
 assert.deepEqual(errors,[])
 console.log(JSON.stringify({gemini:true,main:true,pane:true,usefulRecovery:true,refusedCallsRetained:true,repeatedWorkRefresh:true,finishingStatus:true,boundedRecovery:true,refresh:true,olderDraftCheck:true,explicitExhaustedRetry:true,sharedStyle:true,finishReason:true,tokenCounts:true,noDuplicateUserTurn:true,browserErrors:errors}))
}catch(error){console.error((await page.locator('body').innerText()).slice(-4000));throw error}
finally{await browser.close()}
