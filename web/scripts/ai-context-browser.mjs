import assert from 'node:assert/strict'
import {createHash} from 'node:crypto'
import {chromium} from 'playwright-core'
const [origin,setupCode]=process.argv.slice(2)
const browser=await chromium.launch({executablePath:'/usr/bin/google-chrome',args:['--no-sandbox']})
const page=await browser.newPage({viewport:{width:1440,height:900},colorScheme:'dark'})
page.setDefaultTimeout(15000)
const errors=[];page.on('pageerror',e=>errors.push(e.message))
async function api(path,method='GET',body,revision){const response=await fetch(origin+path,{method,headers:{Authorization:'Bearer '+setupCode,Origin:origin,'Content-Type':'application/json',...(revision?{'If-Match':revision}:{})},...(body?{body:JSON.stringify(body)}:{})});const result=await response.json();assert(response.ok,JSON.stringify(result));return result}
async function assertProgress(placement){
 const chat=page.locator(`[data-placement="${placement}"]`)
 const status=chat.locator('[role="status"][data-running="true"]').last()
 await status.waitFor()
 assert((await status.innerText()).trim().length>0)
 assert.equal(await status.locator('[data-spinner]').count(),1)
 const box=await status.boundingBox();assert(box&&box.height>0)
 const motion=await status.locator('span:not([data-spinner])').last().evaluate(el=>getComputedStyle(el).animationName)
 assert.notEqual(motion,'none')
 await page.screenshot({path:`/tmp/jps-context-gemini-${placement}.png`})
 await page.emulateMedia({reducedMotion:'reduce'})
 assert.equal(await status.locator('span:not([data-spinner])').last().evaluate(el=>getComputedStyle(el).animationName),'none')
 await page.emulateMedia({reducedMotion:'no-preference'})
}
try{
 await page.goto(origin)
 await page.getByRole('button',{name:'Set up sign-in',exact:true}).click()
 await page.getByLabel('Owner setup code',{exact:true}).fill(setupCode)
 await page.getByRole('button',{name:'Continue',exact:true}).click()
 await page.getByRole('button',{name:'Set password',exact:true}).waitFor()
 await page.goto(origin)
 let composer=page.getByPlaceholder('Ask a question or describe a task…')
 await composer.fill('Explain the next step.');await composer.press('Enter')
 await assertProgress('main')
 await fetch(origin+'/test/release')
 await page.getByText('Gemini fixture response complete.',{exact:true}).waitFor()
 await page.getByRole('button',{name:'Stop',exact:true}).waitFor({state:'hidden'})
 await page.waitForTimeout(1200)
 await page.goto('about:blank')
 const saved=await api('/api/conversations'),chat=saved.content.chats[0]
 const document={id:'fixture-pack',version:'0.1.0',title:'Saved fixture pack'}
 const digest=createHash('sha256').update(JSON.stringify(document)).digest('hex')
 chat.pack={id:'fixture-pack',path:'fixture.pack.json',digest};chat.createdCandidateDigest=digest
 chat.inputDrafts=[{version:1,draftId:'fixture-dialect',createdAt:new Date().toISOString(),name:'Fixture dialect',intent:'Retain supplied records',target:'records',fields:[{name:'name',type:'string',description:'Supplied name'}],criteria:[],limits:{records:1,searches:0,pages:0,modelCalls:1}}] // Related work links this pack chat to Dialects.
 chat.checkpoint.state.candidates=[{revision:1,text:JSON.stringify(document),document,digest,producedBy:'conversation'}]
 chat.checkpoint.state.status='ready';chat.checkpoint.state.phase='review'
 await api('/api/conversations','PUT',saved.content,saved.sha256)
 await page.goto(origin+'/dialects?chat='+chat.id)
 const pane=page.locator('[data-placement="pane"]')
 await pane.getByText('Explain the next step.',{exact:true}).waitFor()
 composer=pane.locator('textarea').last()
 await composer.fill('Continue mapping the dialect.');await composer.press('Enter')
 await assertProgress('pane')
 await fetch(origin+'/test/release')
 await pane.getByRole('button',{name:'Stop',exact:true}).waitFor({state:'hidden'})
 assert.equal(await pane.locator('[role="status"][data-running="true"]').count(),0)
 // Reload an interrupted diagnostic beside a pre-existing draft. A long,
 // disposable history forces current-request projection in the real adapter.
 await page.waitForTimeout(1200)
 await page.goto('about:blank')
 const interrupted=await api('/api/conversations'),held=interrupted.content.chats.find(c=>c.id===chat.id)
 const state=held.checkpoint.state,originalCandidate=state.candidates[0].text
 for(let i=0;i<50;i++)state.turns.push({id:'old-'+i,role:'user',kind:'message',text:'Earlier fixture context '+i,statement:'Earlier context '+('fixture '.repeat(500)),at:'2026-10-14T10:00:00Z'})
 const question='Why is omitted size a mismatch?'
 state.turns.push({id:'interrupted-question',role:'user',kind:'message',text:question,statement:question,input:question+'\nCURRENT CASE: routing not-documented; size omitted; expected set-aside; actual unresolved. Exceptions are evaluated first.',at:'2026-10-14T10:10:00Z'})
 state.responses.push({id:'failed-fixture',afterTurnId:'interrupted-question',documents:[],websites:[],sourceIds:[],work:{items:Array.from({length:20},(_,i)=>({id:'read-'+i,name:'read_context',status:'complete'})),notices:[]}})
 state.phase='conversation';state.status='failed';state.detail='Error: The assistant returned an empty reply'
 await api('/api/conversations','PUT',interrupted.content,interrupted.sha256)
 await page.goto(origin+'/dialects?chat='+chat.id)
 await pane.getByRole('button',{name:'Retry response',exact:true}).click()
 await assertProgress('pane')
 await fetch(origin+'/test/release')
 await pane.getByText('The missing-size exception runs before the no-routing rule. No changes were saved.',{exact:true}).waitFor({timeout:45000})
 await pane.getByRole('button',{name:'Stop',exact:true}).waitFor({state:'hidden'})
 assert.equal(await pane.getByText('Response interrupted',{exact:true}).count(),0)
 await page.waitForTimeout(1200)
 const finished=await api('/api/conversations'),completed=finished.content.chats.find(c=>c.id===chat.id).checkpoint.state
 assert.equal(completed.turns.filter(t=>t.id==='interrupted-question').length,1)
 assert.equal(completed.candidates[0].text,originalCandidate)
 assert.equal(completed.responses.at(-1).work.items.length,19)
 assert(completed.responses.at(-1).work.items.every(item=>item.name==='read_context'&&item.status==='complete'))
 await page.reload()
 await pane.getByText('The missing-size exception runs before the no-routing rule. No changes were saved.',{exact:true}).waitFor({timeout:45000})
 assert.equal(await pane.getByText('Response interrupted',{exact:true}).count(),0)
 assert.deepEqual(errors,[])
 console.log(JSON.stringify({geminiMainStatus:true,geminiRestoredDraftPaneStatus:true,spinner:true,dimming:true,reducedMotion:true,retryWithDraft:true,finalRequestReserved:true,currentRequestPreserved:true,reloadPreserved:true,browserErrors:errors}))
}catch(error){await page.screenshot({path:'/tmp/jps-context-browser-error.png'});console.error((await page.locator('body').innerText()).slice(-4500));throw error}
finally{await browser.close()}
