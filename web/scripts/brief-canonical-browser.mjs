// Only the disposable TestAssistantBriefBrowser fixture serves this probe.
import assert from 'node:assert/strict'
import {chromium} from 'playwright-core'
const [origin,setupCode]=process.argv.slice(2)
assert(origin&&setupCode,'Use the disposable TestAssistantBriefBrowser fixture')
const browser=await chromium.launch({executablePath:process.env.CHROME_BIN||'/usr/bin/google-chrome',args:['--no-sandbox']})
const page=await browser.newPage({viewport:{width:1440,height:900},colorScheme:'dark'})
const errors=[],starts=[],commits=[],actions=[],browserModels=[]
page.setDefaultTimeout(20000)
page.on('pageerror',error=>errors.push(error.message))
page.on('request',request=>{
 const path=new URL(request.url()).pathname
 if(request.method()==='POST'&&path==='/api/assistant-runs')starts.push(JSON.parse(request.postData()))
 if(request.method()==='POST'&&/^\/api\/assistant-runs\/[^/]+\/brief$/.test(path))commits.push(JSON.parse(request.postData()))
 if(request.method()==='POST'&&path.endsWith('/actions'))actions.push(JSON.parse(request.postData()))
 if(/\/chat\/completions|\/api\/agent\/run|\/api\/assistant\/model/.test(path))browserModels.push(path)
})
const context=()=>page.getByText('Frozen context <&> 한글',{exact:true})
async function regenerate(){
 await page.getByRole('button',{name:'Brief actions',exact:true}).click()
 await page.getByRole('menuitem',{name:'Regenerate brief',exact:true}).click()
 const dialog=page.getByRole('dialog',{name:'Regenerate brief',exact:true})
 await dialog.waitFor()
 assert((await dialog.getByRole('combobox',{name:'Brief model',exact:true}).innerText()).includes('shared-model'))
 const started=page.waitForResponse(response=>new URL(response.url()).pathname==='/api/assistant-runs'&&response.request().method()==='POST'&&response.status()===201)
 await dialog.getByRole('button',{name:'Regenerate brief',exact:true}).click()
 await started
}
try {
 await page.goto(origin)
 await page.getByRole('button',{name:'Set up sign-in',exact:true}).click()
 await page.getByLabel('Owner setup code',{exact:true}).fill(setupCode)
 await page.getByRole('button',{name:'Continue',exact:true}).click()
 await page.getByRole('button',{name:'Set password',exact:true}).waitFor()
 await page.goto(origin+'/packs/one?view=brief')
 await page.getByRole('button',{name:'Generate brief',exact:true}).waitFor()
 assert.equal(starts.length,0)
 const started=page.waitForResponse(response=>new URL(response.url()).pathname==='/api/assistant-runs'&&response.request().method()==='POST'&&response.status()===201)
 await page.getByRole('button',{name:'Generate brief',exact:true}).click()
 await started
 await page.goto(origin+'/packs')
 await page.goto(origin+'/packs/one?view=brief')
 await context().waitFor()
 assert.equal(starts.length,1,'navigation must only discover/reconnect')
 assert.equal(commits.length,1)
 assert.equal(starts[0].purpose,'brief')
 assert.equal(starts[0].ownerId,'pack:one')
 assert.equal(starts[0].admissionId,`brief-${starts[0].requestId}`)
 assert.deepEqual(Object.keys(starts[0]).sort(),['admissionId','ownerId','purpose','requestId','revision'])
 assert.deepEqual(commits[0],{token:starts[0].requestId})
 await page.reload();await context().waitFor()
 assert.equal(starts.length,1,'opening saved output must never regenerate')
 await regenerate();await context().waitFor()
 await page.getByText(/^Brief · Revision 2 · /).waitFor()
 assert.equal(starts.length,2);assert.equal(commits.length,2)
 assert.notEqual(starts[0].requestId,starts[1].requestId)
 await regenerate()
 const providerDeadline=Date.now()+20000
 while(true){
  const response=await page.request.get(origin+'/brief-probe/status')
  assert(response.ok(),'fixture provider status must be available')
  if((await response.json()).calls===3)break
  assert(Date.now()<providerDeadline,'third generation must reach the fixture provider before Stop')
  await page.waitForTimeout(100)
 }
 await page.getByRole('button',{name:'Stop',exact:true}).waitFor()
 await page.getByRole('button',{name:'Stop',exact:true}).click()
 await page.getByRole('button',{name:'Stop',exact:true}).waitFor({state:'hidden'})
 await page.reload();await context().waitFor()
 await page.getByText(/^Brief · Revision 2 · /).waitFor()
 assert.equal(starts.length,3);assert.equal(commits.length,2)
 assert.equal(actions.filter(action=>action.kind==='cancel').length,1)
 assert.equal(browserModels.length,0,'BriefPane must not enter browser provider transport')
 assert.deepEqual(errors,[])
 console.log('Brief browser: explicit subject/token/model, navigation detach/reconnect, saved reopen, reviewed regeneration, Stop preserving revision, canonical-only model dispatch passed.')
} finally {await browser.close()}
