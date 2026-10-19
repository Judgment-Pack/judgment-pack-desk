import assert from 'node:assert/strict'
import {writeFile} from 'node:fs/promises'
import {chromium} from 'playwright-core'
const [origin,setupCode]=process.argv.slice(2)
assert(origin&&setupCode,'Use the disposable TestChatQuestionBrowser fixture')
const browser=await chromium.launch({executablePath:process.env.CHROME_BIN||'/usr/bin/google-chrome',args:['--no-sandbox']})
const page=await browser.newPage({viewport:{width:1440,height:900},colorScheme:'dark'})
const errors=[],modelRequests=[]
async function api(path,method='GET',body,revision){
 const response=await fetch(origin+path,{method,headers:{Authorization:'Bearer '+setupCode,Origin:origin,'Content-Type':'application/json',...(revision?{'If-Match':revision}:{})},...(body?{body:JSON.stringify(body)}:{})});const result=await response.json();assert(response.ok,JSON.stringify(result));return result
}
page.setDefaultTimeout(20000)
page.on('pageerror',e=>errors.push(e.message))
page.on('request',r=>{if(r.url().includes('/chat/completions'))modelRequests.push(r.url())})
try {
 await page.goto(origin)
 await page.getByRole('button',{name:'Set up sign-in',exact:true}).click()
 await page.getByLabel('Owner setup code',{exact:true}).fill(setupCode)
 await page.getByRole('button',{name:'Continue',exact:true}).click()
 await page.getByRole('button',{name:'Set password',exact:true}).waitFor()
 await page.goto(origin)
 const composer=page.getByPlaceholder('Ask a question or describe a task…')
 await page.getByText('Enter to send · Shift+Enter for a new line',{exact:true}).waitFor()
 // Track the real shell through all streamed/checkpoint mutations, not just at rest.
 await page.evaluate(()=>{
   window.__unexpectedDirty=false
   new MutationObserver(()=>{if(document.querySelector('.desk-dirty')||document.title.startsWith('* '))window.__unexpectedDirty=true}).observe(document.documentElement,{subtree:true,childList:true,characterData:true})
 })
 await composer.fill('Interview me about an ICP.')
 await composer.press('Shift+Enter')
 await composer.pressSequentially('Do not start research.')
 assert.equal(await composer.inputValue(),'Interview me about an ICP.\nDo not start research.')
 for(const key of [{isComposing:true},{keyCode:229},{repeat:true}])await composer.dispatchEvent('keydown',{key:'Enter',...key})
 assert.equal(modelRequests.length,0)
 await composer.press('Enter')
 const card=page.getByRole('form',{name:'Who should we focus on as the first buyer?'})
 await card.waitFor()
 await page.getByRole('button',{name:'Stop',exact:true}).waitFor({state:'hidden'})
 await card.getByText('Enter to submit · Shift+Enter for a new line',{exact:true}).waitFor()
 assert.equal(await card.locator('input:checked').count(),0)
 assert(await card.getByRole('button',{name:'Submit answer',exact:true}).isDisabled())
 assert.equal((await card.getByRole('button',{name:'Submit answer',exact:true}).boundingBox()).height,28)
 assert.equal((await card.getByRole('button',{name:'Help me decide',exact:true}).boundingBox()).height,28)
 const box=await card.boundingBox();assert(box.height<350)
 await page.screenshot({path:'/tmp/jps-question-main-dark.png'})
 await card.getByRole('radio',{name:'Head of operations Recommended',exact:true}).focus()
 await page.keyboard.press('Space')
 await card.getByRole('textbox').fill('Regional teams')
 await page.waitForTimeout(1100)
 assert.equal(await page.evaluate(()=>window.__unexpectedDirty),false)
 await page.reload();await card.waitFor()
 assert(await card.getByRole('radio',{name:'Head of operations Recommended',exact:true}).isChecked())
 assert.equal(await card.getByRole('textbox').inputValue(),'Regional teams')
 assert.equal(modelRequests.length,1)
 assert.equal(await page.locator('.desk-dirty').count(),0)
 // Restore the same pending question in the real Dialects Assistant pane.
 await page.goto('about:blank')
 const saved=await api('/api/conversations')
 const chat=saved.content.chats[0];chat.input={id:'list'}
 await api('/api/conversations','PUT',saved.content,saved.sha256)
 await page.goto(origin+'/dialects?chat='+chat.id)
 await page.locator('[data-placement="pane"]').getByRole('form').waitFor()
 assert(await card.getByRole('radio',{name:'Head of operations Recommended',exact:true}).isChecked())
 assert.equal(await card.getByRole('textbox').inputValue(),'Regional teams')
 assert.equal(await card.evaluate(el=>el.scrollWidth>el.clientWidth+1),false)
 await page.screenshot({path:'/tmp/jps-question-inputs-pane.png'})
 assert.equal(modelRequests.length,1)
 await page.goto(origin+'/chats/'+chat.id);await card.waitFor()
 // Prose-only help must leave a usable panel, without an extra model call.
 await card.getByRole('button',{name:'Help me decide',exact:true}).click()
 await page.getByText('Operations owns the workflow; risk owns oversight. Choose the buyer who can sponsor your pilot.',{exact:true}).waitFor()
 await card.waitFor()
 await page.getByRole('button',{name:'Stop',exact:true}).waitFor({state:'hidden'})
 assert.equal(await card.locator('input:checked').count(),0)
 assert.equal(await card.getByRole('textbox').inputValue(),'')
 assert.equal(modelRequests.length,2)
 await page.waitForTimeout(1100);await page.reload();await card.waitFor()
 assert.equal(modelRequests.length,2)
 await card.getByRole('radio',{name:'Head of operations Recommended',exact:true}).check()
 const answer=card.getByRole('textbox')
 await answer.fill('Regional teams')
 await answer.press('Shift+Enter')
 await answer.pressSequentially('Claims operations')
 assert.equal(await answer.inputValue(),'Regional teams\nClaims operations')
 for(const key of [{isComposing:true},{keyCode:229},{repeat:true}])await answer.dispatchEvent('keydown',{key:'Enter',...key})
 assert.equal(modelRequests.length,2)

 // Narrow and light layouts use the same production component and tokens.
 for(const width of [600,390,320]){
  await page.setViewportSize({width,height:900})
  assert.equal(await card.evaluate(el=>el.scrollWidth>el.clientWidth+1),false)
  const b=await card.boundingBox();assert(b.x>=0&&b.x+b.width<=width+1)
  await page.screenshot({path:`/tmp/jps-question-${width}.png`})
 }
 await page.emulateMedia({colorScheme:'light'})
 await page.screenshot({path:'/tmp/jps-question-mobile-light.png'})
 await page.setViewportSize({width:1440,height:900});await page.emulateMedia({colorScheme:'dark'})
 // A failed checkpoint must not dispatch another model request.
 let refused=false
 await page.route('**/api/conversations',async route=>{
  if(route.request().method()==='PUT'&&route.request().postData()?.includes('"kind":"answered"')){refused=true;await route.fulfill({status:500,contentType:'application/json',body:JSON.stringify({error:'fixture-save-failed'})})}
  else await route.continue()
 })
 await answer.press('Enter')
 await page.getByRole('button',{name:'Continue',exact:true}).waitFor()
 assert.equal(modelRequests.length,2)
 await page.locator('.desk-dirty').waitFor()
 assert((await page.title()).startsWith('* '))
 await page.unroute('**/api/conversations')
 // Continue itself retries the failed checkpoint before any model dispatch.
 let held,release
 const writeStarted=new Promise(resolve=>{held=resolve}),allowWrite=new Promise(resolve=>{release=resolve})
 await page.route('**/api/conversations',async route=>{
  if(route.request().method()==='PUT'){held();await allowWrite}
  await route.continue()
 })
 await page.getByRole('button',{name:'Continue',exact:true}).click()
 await writeStarted
 assert.equal(await page.locator('.desk-dirty').count(),1)
 assert((await page.title()).startsWith('* '))
 release()
 await page.getByText('We will focus on operations leaders at regional teams. Research has not started.',{exact:true}).waitFor()
 assert.equal(modelRequests.length,3)
 await page.locator('.desk-dirty').waitFor({state:'hidden'})
 assert.equal((await page.title()).startsWith('* '),false)
 assert.equal(await card.count(),0)
 await page.waitForTimeout(1100);await page.reload()
 await page.getByText('We will focus on operations leaders at regional teams. Research has not started.',{exact:true}).waitFor()
 assert.equal(modelRequests.length,3)
 assert.deepEqual(errors,[])
 console.log(JSON.stringify({compact:true,helpReoffersChoices:true,helpReloadDidNotResume:true,inputsPane:true,keyboard:true,enterSubmits:true,shiftEnterNewline:true,compositionSafe:true,responsive:true,darkAndLight:true,pendingDraftRestored:true,failedSaveDidNotContinue:true,oneContinuation:true,answeredRestored:true,browserErrors:errors}))
} catch(e) {
 await page.screenshot({path:'/tmp/jps-question-browser-failure.png'})
 await writeFile('/tmp/jps-question-browser-dom.txt',await page.locator('body').innerText())
 throw e
} finally { await browser.close() }
