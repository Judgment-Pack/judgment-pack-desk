import {chromium} from '../../../web/node_modules/playwright-core/index.mjs'
import fs from 'node:fs/promises'
import crypto from 'node:crypto'

// This harness creates one policy in an explicitly isolated local Desk. Only
// the source API is mocked, after the real unsupported-Gateway check passes.
const base=process.argv[2],out=process.argv[3]
if(!base||!out||process.env.JPACK_POLICY_SOURCE_BROWSER_ISOLATED!=='1')throw Error('Pass an isolated Desk URL and evidence directory, with JPACK_POLICY_SOURCE_BROWSER_ISOLATED=1.')
const origin=new URL(base)
if(origin.protocol!=='http:'||origin.hostname!=='127.0.0.1')throw Error('Use the isolated 127.0.0.1 Desk listener.')
await fs.mkdir(out,{recursive:true})
const wire=JSON.parse(await fs.readFile(new URL('../../../internal/desk/testdata/policy-source-observation.json',import.meta.url),'utf8'))
const hash=bytes=>crypto.createHash('sha256').update(bytes).digest('hex')
const canonical=value=>JSON.stringify(value===null||typeof value!=='object'?value:Array.isArray(value)?value.map(item=>JSON.parse(canonical(item))):Object.fromEntries(Object.keys(value).sort().map(key=>[key,JSON.parse(canonical(value[key]))])))
const title='Policy source browser smoke',narrative='Saved candidate A.\n\nA local administrator must review each source update before publishing.'
const browser=await chromium.launch({executablePath:process.env.CHROMIUM_BIN||'/usr/bin/google-chrome',headless:true,args:['--no-sandbox','--disable-dev-shm-usage']})
const context=await browser.newContext({viewport:{width:1440,height:1050},colorScheme:'light',acceptDownloads:true})
const page=await context.newPage(),errors=[],policyMutations=[],sourceWrites=[],sourceReads=[],externalRequests=[],modelRequests=[],screenshots=[],geometries=[]
page.setDefaultTimeout(15_000)
page.on('pageerror',error=>errors.push(error.message))
page.on('request',request=>{const path=new URL(request.url()).pathname;if(request.method()==='POST'&&/\/(?:relay|model-relay|research-relay|codex|claude)(?:\/|$)/.test(path))modelRequests.push(path)})
page.on('response',async response=>{
 const request=response.request(),path=new URL(response.url()).pathname
 if(path.startsWith('/api/policies')&&['POST','PUT'].includes(request.method())){
  try{const body=await response.json();policyMutations.push({path,status:response.status(),delivery:body.delivery,policyId:body.policy?.document.id,actor:body.receipt?.actor})}catch{}
 }
})
await context.route('**/*',async route=>{
 const request=route.request(),url=new URL(request.url())
 if(['http:','https:'].includes(url.protocol)&&url.origin!==origin.origin){externalRequests.push(url.origin+url.pathname);await route.abort();return}
 if(request.method()==='POST'&&/\/(?:relay|model-relay|research-relay|codex|claude)(?:\/|$)/.test(url.pathname)){await route.abort();return}
 await route.continue()
})
async function api(path){
 return page.evaluate(async path=>{
  const token=sessionStorage.getItem('jpack-desk-session:'+location.host),desk=sessionStorage.getItem('jpack.active-desk')
  if(!token)throw Error('The real local sign-in did not establish a session.')
  const response=await fetch(path,{headers:{Authorization:'Bearer '+token,...(desk?{'X-Jpack-Desk':desk}:{})}})
  if(!response.ok)throw Error(path+' returned '+response.status)
  return response.json()
 },path)
}
async function screenshot(name){await page.screenshot({path:out+'/'+name,fullPage:true});screenshots.push(name)}
async function geometry(label){
 const result=await page.evaluate(()=>{
  const visible=element=>{const r=element.getBoundingClientRect();return r.width>0&&r.height>0&&getComputedStyle(element).visibility!=='hidden'&&element.getClientRects().length>0&&!element.closest('[aria-hidden="true"],[inert]')}
  const dialogs=[...document.querySelectorAll('[role="dialog"]')].filter(visible),modal=dialogs.at(-1)
  const scrollContainers=[...document.querySelectorAll('[data-page-scroll],[data-pane-scroll]')].filter(visible)
  // A modal covers the mobile takeover pane; its occluded background can
  // retain layout geometry while Radix removes it from interaction. Check the
  // active modal now and all visible page containers again after it closes.
  return {viewport:innerWidth,documentWidth:document.documentElement.scrollWidth,surface:modal?'modal':'page',
   excludedModalBackgroundContainers:modal?scrollContainers.filter(element=>!modal.contains(element)).length:0,
   scrollContainers:scrollContainers.filter(element=>!modal||modal.contains(element)).map(element=>({role:element.getAttribute('role'),client:element.clientWidth,scroll:element.scrollWidth})),
   dialogs:dialogs.map(element=>{const r=element.getBoundingClientRect();return {left:r.left,right:r.right,client:element.clientWidth,scroll:element.scrollWidth}})}
 })
 geometries.push({label,...result})
 if(result.documentWidth>result.viewport+1||result.scrollContainers.some(item=>item.scroll>item.client+1)||result.dialogs.some(item=>item.left< -1||item.right>result.viewport+1||item.scroll>item.client+1))throw Error(label+' has horizontal overflow: '+JSON.stringify(result))
}
function syntheticObservation(revision,id){
 const document=structuredClone(wire.document)
 document.document.version=revision;document.provenance.source.version=revision
 const bytes=Buffer.from(canonical(document))
 const metadata={id,bindingId:wire.bindingId,source:wire.source,sourceRevision:revision,observedAt:wire.observedAt,retainedAt:wire.observedAt,name:document.document.name,availability:'present',coverage:'snapshot-only',lifecycle:{kind:'not-exposed'},content:wire.content,
  extraction:{...wire.extraction,content:{...wire.extraction.content,bytesDigest:'sha256:'+hash(bytes),byteLength:bytes.length}},
  original:{sha256:wire.content.bytesDigest.slice(7),bytes:wire.content.byteLength},document:{sha256:hash(bytes),bytes:bytes.length},semanticDigest:hash('synthetic revision '+revision)}
 return {metadata,document}
}
let policyId,realUnsupported,phase='A',sourcePath
try{
 await page.goto(new URL('/library',origin).href,{waitUntil:'domcontentloaded'})
 await page.getByRole('button',{name:'Create',exact:true}).click()
 await page.getByRole('menuitem',{name:'New policy',exact:true}).click()
 await page.getByRole('textbox',{name:'Title',exact:true}).fill(title)
 await page.getByRole('textbox',{name:'Artifact version',exact:true}).fill('1.0.0')
 await page.getByRole('textbox',{name:'Policy text',exact:true}).fill(narrative)
 await page.getByRole('button',{name:'Save draft',exact:true}).click()
 await page.waitForURL(/\/policies\/policy_/)
 policyId=new URL(page.url()).pathname.split('/').at(-1)
 const signIn=await api('/api/auth/status')
 if(!signIn.sessionActive||!signIn.localAccess)throw Error('The harness requires real local administrator sign-in.')
 const saved=await api('/api/policies/'+policyId)
 if(saved.document.candidate.narrative!==narrative)throw Error('Real policy creation did not retain the saved candidate.')
 sourcePath='/api/policies/'+policyId+'/source-sync'
 realUnsupported=await api(sourcePath)
 if(realUnsupported.supported!==false)throw Error('Use a Gateway installation that actually lacks this source capability for the first check.')
 await page.getByRole('tab',{name:'Source',exact:true}).click()
 await page.getByText('The installed Gateway does not support automatic policy source checks.',{exact:true}).waitFor()
 const choose=page.getByRole('button',{name:'Choose Google Drive source',exact:true})
 if(await choose.count()&&await choose.isVisible()&&await choose.isEnabled())throw Error('Unsupported Gateway exposed an enabled source connection control.')
 await geometry('real unsupported Gateway, desktop')
 await screenshot('unsupported-gateway-desktop.png')

 // Deliberately synthetic browser presentation fixtures. They do not bind a
 // Gateway grant, retain backend source artifacts, or prove Google access.
 const a=syntheticObservation('7',hash('browser synthetic observation A'))
 const b=syntheticObservation('8',hash('browser synthetic observation B'))
 const fixture=(current)=>({revision:'synthetic-source-'+current,supported:true,pending:true,state:{version:1,generation:1,state:'active',frequencyMinutes:15,gap:false,
  grant:{actor:{kind:'local',subject:'local-admin'},at:wire.observedAt,resourceId:wire.source.resourceId},
  binding:{version:1,operationId:hash('browser synthetic bind'),bindingId:wire.bindingId,source:wire.source,createdAt:wire.observedAt,state:'active',coverage:'snapshot-only',lifecycle:{kind:'not-exposed'}},
  lastAttempt:wire.observedAt,lastSuccess:wire.observedAt,nextCheck:wire.observedAt,cleanup:[],actions:[],resolutions:[],observations:current==='A'?[a.metadata]:[a.metadata,b.metadata]}})
 await page.route(url=>url.origin===origin.origin&&(url.pathname===sourcePath||url.pathname.startsWith(sourcePath+'/observations/')),async route=>{
  const request=route.request(),path=new URL(request.url()).pathname
  if(request.method()!=='GET'){
   sourceWrites.push({path,method:request.method(),body:request.postDataJSON()})
   await route.fulfill({status:409,contentType:'application/json',body:JSON.stringify({code:'source-conflict',outcome:'not-committed'})});return
  }
  if(path===sourcePath){sourceReads.push(phase);await route.fulfill({contentType:'application/json',headers:{'Cache-Control':'no-store','X-Policy-Source-Browser-Fixture':'synthetic'},body:JSON.stringify(fixture(phase))});return}
  const item=path.includes(b.metadata.id)?b:a
  if(path.endsWith('/original')){await route.fulfill({contentType:wire.content.mediaType,body:Buffer.from(item.document.original.bytes,'base64')});return}
  await route.fulfill({contentType:'application/json',body:JSON.stringify({observation:item.metadata,document:item.document})})
 })
 await page.reload({waitUntil:'domcontentloaded'})
 await page.getByRole('tab',{name:'Source',exact:true}).click()
 const source=page.getByRole('tabpanel',{name:'Source',exact:true}),table=source.getByRole('table',{name:'Source updates',exact:true})
 await table.waitFor()
 await table.getByRole('cell',{name:'7',exact:true}).waitFor()
 await source.getByRole('region',{name:'Retained source snapshot',exact:true}).getByText('A test policy document.',{exact:true}).waitFor()
 await source.getByRole('button',{name:'Download original',exact:true}).waitFor()
 await geometry('synthetic source table, desktop')
 await screenshot('synthetic-source-table-desktop.png')
 await source.getByRole('button',{name:'Review keeping current candidate',exact:true}).click()
 const review=page.getByRole('dialog',{name:'Keep the current candidate?',exact:true})
 await review.getByText('7',{exact:true}).waitFor()
 await review.getByText(narrative,{exact:true}).waitFor()
 const keep=review.getByRole('button',{name:'Keep current candidate',exact:true})
 if(!await keep.isEnabled())throw Error('The initial A review was unexpectedly disabled.')
 phase='B'
 await page.evaluate(()=>{document.dispatchEvent(new Event('visibilitychange'));window.dispatchEvent(new Event('focus'))})
 await review.getByRole('alert').filter({hasText:'The policy or source changed.'}).waitFor({timeout:25_000})
 if(await review.getByText('8',{exact:true}).count()||!await review.getByText('7',{exact:true}).isVisible()||await keep.isEnabled())throw Error('Source B replaced the frozen A review or left saving enabled.')
 await keep.evaluate(button=>button.click())
 if(sourceWrites.length)throw Error('A stale review dispatched a source write.')
 await geometry('frozen A review after B, desktop')
 await screenshot('synthetic-frozen-review-desktop.png')
 await page.setViewportSize({width:420,height:920})
 await geometry('frozen A review after B, 420px')
 await screenshot('synthetic-frozen-review-420.png')
 await review.getByRole('button',{name:'Cancel',exact:true}).click()
 const back=page.locator('#desk-inspector').getByRole('button',{name:'Back to policy',exact:true})
 if(await back.count()&&await back.isVisible())await back.click()
 await page.getByRole('tab',{name:'Source',exact:true}).click()
 await table.getByRole('cell',{name:'8',exact:true}).waitFor()
 await page.locator('[data-page-scroll]').evaluateAll(elements=>elements.forEach(element=>{element.scrollTop=0}))
 await geometry('synthetic source table, 420px')
 await screenshot('synthetic-source-table-420.png')
 const final=await api('/api/policies/'+policyId)
 if(final.revision!==saved.revision||final.document.candidate.narrative!==narrative||final.document.publications.length)throw Error('Browser source inspection changed the real saved policy.')
 if(errors.length||modelRequests.length||externalRequests.length||sourceWrites.length||!sourceReads.includes('B'))throw Error('Unexpected activity: '+JSON.stringify({errors,modelRequests,externalRequests,sourceWrites,sourceReads}))
 const evidence={passed:true,scope:'Real isolated local sign-in and policy creation; source presentation and A-to-B review use explicitly mocked source API responses. Not live Google end-to-end.',policyId,
  actualLocalSignIn:{sessionActive:signIn.sessionActive,localAccess:signIn.localAccess},actualUnsupportedGateway:realUnsupported,syntheticSourceAPI:true,
  sourceUpdatesTable:true,extractionVisible:true,frozenAReviewAfterB:true,staleSaveDisabled:true,noSourceWriteDispatched:true,savedCandidateUnchanged:true,
  policyMutations,sourceReads,sourceWrites,modelRequests,externalRequests,pageErrors:errors,geometries,screenshots}
 await fs.writeFile(out+'/smoke-result.json',JSON.stringify(evidence,null,2)+'\n')
 console.log(JSON.stringify(evidence,null,2))
}catch(error){await page.screenshot({path:out+'/failure.png',fullPage:true});await fs.writeFile(out+'/failure.txt',String(error)+'\n'+await page.locator('body').innerText());throw error}
finally{await browser.close()}
