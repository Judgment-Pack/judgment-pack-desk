import assert from 'node:assert/strict'
import {readFileSync} from 'node:fs'
import {chromium} from '../../web/node_modules/playwright-core/index.mjs'
import {readPrivate,writePrivate,control,report,original,candidate,prompt,digest,stages,saveMessages,readOnlyCodes,projectionReady,dirtyObservation} from './persisted-proposal-report.mjs'
let c
try{c=control(readPrivate(process.argv[2]))}catch{process.stderr.write('offline proposal control refused\n');process.exit(1)}
const started=Date.now(),deadline=started+60000,left=()=>{assert(Date.now()<deadline);return deadline-Date.now()},pause=()=>new Promise(r=>setTimeout(r,100))
let browser,page,bearer='',starts=0,writes=0,checks=0,errors=0,unexpected=0,runId=null
let r={kind:'offline-persisted-proposal-browser',caseId:c.caseId,state:'pending',phase:'bootstrap',errorName:null,runId:null,chatId:null,proposal:null,operations:null,freshChecks:null,settings:null,file:null,counts:null,ui:{applied:false,dirtyRefused:false,duplicateBlocked:false,reloadStable:false},elapsedMs:0}
r.diagnostic={stages:Object.fromEntries(stages.map(k=>[k,null])),click:'not-started',button:'unknown',messages:[],requests:[],readOnly:'none',exceptions:[],stop:'none',terminal:[],dirtySteps:[]}
const mark=k=>{r.diagnostic.stages[k]??=Date.now()-started}
mark('bootstrap')
const category=(url,method)=>{const p=new URL(url).pathname;return p==='/api/file'?(method==='GET'?'file-read':method==='PUT'?'file-write':null):p==='/api/pack-tests'&&method==='GET'?'suite-read':/^\/api\/assistant-runs\/arun_[a-f0-9]+\/pack-checks$/.test(p)?'check':/^\/api\/assistant-runs\/owners\/[a-zA-Z0-9-]{1,80}\/requests\/[A-Za-z0-9-]{1,100}$/.test(p)&&method==='GET'?'discover':/^\/api\/assistant-runs\/arun_[a-f0-9]+$/.test(p)&&method==='GET'?'run-read':null}
const observeStatus=(category,status)=>{if(!category)return;const rows=r.diagnostic.requests,row=rows.find(v=>v.category===category&&v.status===status);if(row)row.count=Math.min(4096,row.count+1);else if(rows.length<64)rows.push({category,status,count:1})}

let actionCard
const observeUI=async()=>{if(!actionCard)return;try{const button=actionCard.getByRole('button',{name:/^(Save proposal|Saving proposal…)$/});r.diagnostic.button=await button.count()===0?'absent':await button.getAttribute('aria-busy')==='true'?'saving':await button.isDisabled()?'disabled':'enabled';for(const text of await actionCard.getByRole('status').allTextContents()){const code=saveMessages[text.trim()];if(code&&!r.diagnostic.messages.includes(code))r.diagnostic.messages.push(code)}}catch{}}
const observeDirty=async(step,edge,locator)=>{
 let state=null,dirty=null
 try{if(locator)state=await locator.evaluateAll(nodes=>({count:Math.min(8,nodes.length),enabled:nodes.length===1?!nodes[0].matches(':disabled,[aria-disabled="true"]'):null,readOnly:nodes.length===1?nodes[0].hasAttribute('readonly'):null}));dirty=await page.evaluate(()=>document.title.startsWith('* '))}catch{}
 r.diagnostic.dirtySteps.push(dirtyObservation({step,edge,atMs:Date.now()-started,locator:state,dirty}))
}
const dirtyStep=async(step,locator,action)=>{await observeDirty(step,'before',locator);await action();await observeDirty(step,'after',locator)}
writePrivate(c.reportPath,r)
const timer=setTimeout(()=>void browser?.close(),60000)
const api=async(path,options={})=>{
 assert(bearer);assert(/^\/api\/(?:conversations|pack-tests|file\?path=one\.json|assistant-runs\/arun_[a-f0-9]+)$/.test(path))
 const res=await page.request.fetch(c.origin+path,{...options,headers:{Authorization:bearer,...options.headers},timeout:left()});observeStatus(category(c.origin+path,options.method??'GET'),res.status());assert(res.ok());const text=await res.text();assert(Buffer.byteLength(text)<=4*1024*1024);return JSON.parse(text)
}
const file=async()=>{const v=await api('/api/file?path=one.json');const code=v.readOnlyReason??'none';r.diagnostic.readOnly=readOnlyCodes.includes(code)?code:'other';return v}
const suiteValue={version:1,suites:{one:{revision:1,cases:[{id:'existing',name:'Authored original',revision:1,row:{id:'existing',facts:{n:1},expectedDisposition:'proceed'},sources:[],sourceMappings:{},origin:'manual',rationale:'Original requirement: proceed without changing policy.'}],runs:[],recovered:[]}}}
const wait=async f=>{while(true){await observeUI();left();assert.equal(errors,0);assert.equal(unexpected,0);if(await f())return;await pause()}}
try{
 browser=await chromium.launch({executablePath:c.browserBin,args:['--no-sandbox'],timeout:left()})
 const context=await browser.newContext({viewport:{width:1600,height:1000},locale:'en-CA'});page=await context.newPage();page.setDefaultTimeout(15000)
 await context.route('**/*',async route=>{const u=new URL(route.request().url());if(u.origin!==c.origin||/\/api\/model-providers\/.*\/(?:login|install|logout)/.test(u.pathname)){unexpected++;await route.abort()}else await route.continue()})
 page.on('pageerror',()=>errors++)
 page.on('request',q=>{const p=new URL(q.url()).pathname;if(p==='/api/conversations')void q.headerValue('authorization').then(v=>{bearer=v??bearer});if(p==='/api/assistant-runs'&&q.method()==='POST'){starts++;if(starts>1)unexpected++}if(p==='/api/file'&&q.method()==='PUT')writes++;if(/^\/api\/assistant-runs\/arun_[a-f0-9]+\/pack-checks$/.test(p)&&q.method()==='POST')checks++})
 page.on('response',res=>{observeStatus(category(res.url(),res.request().method()),res.status());if(new URL(res.url()).pathname==='/api/assistant-runs'&&res.request().method()==='POST')void res.json().then(v=>{if(res.status()!==201||!/^arun_[a-f0-9]+$/.test(v.id))unexpected++;else runId=v.id}).catch(()=>unexpected++)})
 await page.goto(c.origin,{timeout:left()});await page.getByRole('button',{name:'Set up sign-in',exact:true}).click();await page.getByLabel('Owner setup code',{exact:true}).fill(c.setupCode);await page.getByRole('button',{name:'Continue',exact:true}).click();await page.getByRole('button',{name:'Set password',exact:true}).waitFor();await page.goto(c.origin,{timeout:left()});await wait(()=>!!bearer)
 // Only a handwritten suite is seeded through its ordinary authenticated API.
 // No chat, run, candidate, checks or execution checkpoint is inserted.
 const empty=await api('/api/pack-tests');await api('/api/pack-tests',{method:'PUT',headers:{'If-Match':empty.sha256,'Content-Type':'application/json'},data:suiteValue})
 const suiteBefore=readFileSync(c.suitePath,'utf8');assert(Buffer.byteLength(suiteBefore)<=65536)
 assert.deepEqual(JSON.parse(suiteBefore),suiteValue)
 assert.deepEqual((await api('/api/pack-tests')).content,suiteValue)
 const before=await file();assert.equal(before.content,original)
 r.file={before:before.content,afterAction:null,final:null,reloaded:null,suiteBefore,suiteAfter:null}
 const native=c.caseId!=='api-save'
 mark('settings');r.phase='settings'
 await page.goto(c.origin+'/packs/one'+(native?'?edit=1&shape=json&builder=source':''),{timeout:left()})
 const pane=page.locator('section[data-chat-id][data-placement="pane"]').first()
 const composer=pane.getByPlaceholder('Ask about this pack…');await composer.waitFor()
 await pane.getByRole('combobox',{name:'Task tools',exact:true}).click();await page.getByRole('option',{name:'Chat',exact:true}).click()
 if(native){await pane.getByRole('button',{name:/^Model: gpt-6-sol/}).click();const slider=page.getByRole('slider',{name:'Reasoning',exact:true});await slider.press('Home');await slider.press('ArrowRight');assert.equal(await slider.getAttribute('aria-valuetext'),'Medium');await page.keyboard.press('Escape')}
 await pane.getByRole('button',{name:'Assistant settings',exact:true}).click();await page.getByRole('combobox',{name:'Research',exact:true}).click();await page.getByRole('option',{name:'Provided sources only',exact:true}).click();await page.getByRole('combobox',{name:'Adversarial review',exact:true}).selectOption('off');await page.keyboard.press('Escape')
 if(native){const raw=page.getByRole('textbox',{name:"The document's bytes",exact:true});assert.equal(await raw.inputValue(),original);await page.getByRole('navigation',{name:'Pack builder',exact:true}).getByRole('button',{name:'Build',exact:true}).click()}
 mark('dispatch');r.phase='dispatch';await composer.fill(prompt);await composer.press('Enter');await wait(()=>!!runId);r.runId=runId
 mark('terminal');r.phase='terminal';let run
 await wait(async()=>{run=await api('/api/assistant-runs/'+runId);return projectionReady(run,r.diagnostic.terminal,Date.now()-started)})
 assert.equal(run.state,'complete');assert.equal(run.chatProjection?.acknowledged,true);r.chatId=run.owner.id
 mark('proposal');r.phase='proposal'
 const saved=await api('/api/conversations'),chat=saved.content.chats.find(x=>x.id===r.chatId);assert(chat);assert.equal(chat.pack.path,'one.json');assert.equal(chat.model,'gpt-6-sol');assert.equal(chat.critic,'off');assert.equal(chat.researchMode,'provided');assert.equal(chat.mode,'draft');assert([undefined,'work'].includes(chat.interaction));if(native)assert.equal(chat.reasoning.effort,'medium')
 r.settings={model:chat.model,effort:native?chat.reasoning.effort:null,thinking:native?null:'off',critic:chat.critic,research:chat.researchMode,mode:chat.mode,phase:chat.interaction??'work',phaseInput:{state:Object.hasOwn(chat,'interaction')?'value':'absent',value:chat.interaction??null},criticInput:{state:Object.hasOwn(chat,'critic')?'value':'absent',value:chat.critic??null}}
 const proposed=chat.checkpoint.state.candidates.at(-1);assert(proposed);r.proposal={text:proposed.text,digest:digest(proposed.text),basis:proposed.packReview.basis,check:proposed.packReview.check};assert.equal((await file()).content,original)
 r.operations=run.operations.map(o=>({id:o.id,callId:o.callId,kind:o.kind,name:o.name,state:o.state,intentSha256:digest(Buffer.from(o.intent,'base64').toString('utf8')),resultSha256:o.result===null?null:digest(Buffer.from(o.result,'base64').toString('utf8'))}))
 const proposal=pane.getByRole('region',{name:'Proposed pack update',exact:true})
 mark('action');r.phase='action'
 if(c.caseId==='api-save'){
  await page.goto(c.origin+'/chats/'+r.chatId,{timeout:left()});mark('navigation');const card=page.getByRole('region',{name:'Proposed pack update',exact:true});actionCard=card;await observeUI();mark('click-start');r.diagnostic.click='pending';try{await card.getByRole('button',{name:'Save proposal',exact:true}).click();r.diagnostic.click='returned';mark('click-return')}catch(e){r.diagnostic.click='failed';throw e}await wait(async()=>(await file()).content===candidate);mark('file-observed');r.file.afterAction=(await file()).content;await wait(async()=>await page.getByRole('button',{name:'Save proposal',exact:true}).count()===0);r.ui.duplicateBlocked=true
 }else if(c.caseId==='native-apply-save'){
  await proposal.getByRole('button',{name:'Apply to draft',exact:true}).click();await wait(async()=>await proposal.getByRole('button',{name:'Applied to draft',exact:true}).isDisabled());await page.getByRole('navigation',{name:'Pack builder',exact:true}).getByRole('button',{name:'Source',exact:true}).click();const raw=page.getByRole('textbox',{name:"The document's bytes",exact:true});await wait(async()=>(await raw.inputValue())===candidate);r.ui.applied=true;r.file.afterAction=(await file()).content;assert.equal(r.file.afterAction,original);assert.equal(writes,0);assert.equal(await proposal.getByRole('button',{name:'Applied to draft',exact:true}).isDisabled(),true);r.ui.duplicateBlocked=true;await page.getByRole('button',{name:'Save',exact:true}).click();await wait(async()=>(await file()).content===candidate)
 }else{
  const source=page.getByRole('navigation',{name:'Pack builder',exact:true}).getByRole('button',{name:'Source',exact:true}),raw=page.getByRole('textbox',{name:"The document's bytes",exact:true}),build=page.getByRole('navigation',{name:'Pack builder',exact:true}).getByRole('button',{name:'Build',exact:true}),apply=proposal.getByRole('button',{name:'Apply to draft',exact:true}),save=proposal.getByRole('button',{name:'Save proposal',exact:true})
  await dirtyStep('source',source,()=>source.click());await dirtyStep('original',raw,async()=>assert.equal(await raw.inputValue(),original));await dirtyStep('fill',raw,()=>raw.fill(original.replace('A minimal pack','An unsaved edit')));await dirtyStep('build',build,()=>build.click());await dirtyStep('apply-disabled',apply,()=>wait(async()=>await apply.isDisabled()));await dirtyStep('save-disabled',save,async()=>assert.equal(await save.isDisabled(),true));await dirtyStep('flags',null,async()=>{r.ui.dirtyRefused=true;r.ui.duplicateBlocked=true});await dirtyStep('after-file',null,async()=>{r.file.afterAction=(await file()).content});await dirtyStep('zero-writes',null,async()=>assert.equal(writes,0))
  // Discard only this journey's manual edit using the ordinary editor action.
  const discard=page.getByRole('button',{name:'Discard',exact:true}),dialog=page.getByRole('dialog',{name:'Unsaved changes',exact:true}),confirmText=dialog.getByLabel('Type An unsaved edit to confirm.',{exact:true}),confirmDiscard=dialog.getByRole('button',{name:'Discard changes',exact:true})
  await dirtyStep('discard',discard,()=>discard.click());await dirtyStep('confirm-text',confirmText,()=>confirmText.fill('An unsaved edit'));await dirtyStep('confirm-discard',confirmDiscard,()=>confirmDiscard.click());await dirtyStep('restore-source',source,()=>source.click());await dirtyStep('restore-original',raw,()=>wait(async()=>(await raw.inputValue())===original))
 }
 mark('readback');r.phase='readback';r.file.final=(await file()).content;r.file.suiteAfter=readFileSync(c.suitePath,'utf8');assert.deepEqual((await api('/api/pack-tests')).content,suiteValue)
 const after=await api('/api/assistant-runs/'+runId);r.freshChecks=after.packChecks??[]
 const heldCounts={starts,writes,checks}
 mark('reload');r.phase='reload';await page.goto(c.origin+'/packs/one?edit=1&shape=json&builder=source&chat='+r.chatId,{timeout:left()});const raw=page.getByRole('textbox',{name:"The document's bytes",exact:true});await raw.waitFor();assert.equal(await raw.inputValue(),r.file.final);r.file.reloaded=(await file()).content
 assert.deepEqual({starts,writes,checks},heldCounts);const reloaded=await api('/api/assistant-runs/'+runId);assert.deepEqual(reloaded.operations,after.operations);r.ui.reloadStable=true
 r.counts={starts,models:run.operations.filter(o=>o.kind==='model').length,writes,checks,browserErrors:errors,unexpected};mark('complete');r.phase='complete';r.state='complete';r.elapsedMs=Date.now()-started;report(r)
}catch(e){await observeUI();r.state='incomplete';r.errorName=['Error','TimeoutError','AssertionError','TypeError','RangeError'].includes(e?.name)?e.name:'Other';r.counts={starts,models:null,writes,checks,browserErrors:errors,unexpected}}
finally{clearTimeout(timer);try{await browser?.close()}catch{r.state='incomplete';r.errorName='Error'};r.elapsedMs=Date.now()-started;try{report(r);writePrivate(c.reportPath,r)}catch{r={...r,state:'incomplete',proposal:null,operations:null,freshChecks:null,settings:null,file:null};try{report(r);writePrivate(c.reportPath,r)}catch{process.stderr.write('offline proposal report refused\n')};process.exitCode=1}}
if(r.state!=='complete'){process.stderr.write('offline proposal incomplete: '+r.phase+'\n');process.exitCode=1}else process.stdout.write('offline proposal complete\n')
