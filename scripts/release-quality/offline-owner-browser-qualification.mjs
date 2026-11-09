// Explicit offline qualification only: actual owner/capture, supplied peers.
import assert from 'node:assert/strict'
import {spawn} from 'node:child_process'
import {readFileSync,writeFileSync,appendFileSync,mkdirSync,readdirSync,lstatSync,openSync,closeSync,constants} from 'node:fs'
import {resolve,join,dirname,isAbsolute} from 'node:path'
import {fileURLToPath} from 'node:url'
import {createHash} from 'node:crypto'
import {chromium} from '../../web/node_modules/playwright-core/index.mjs'
import {captureLiveSession,privateOwnerIO,validateDescriptor} from './live-browser-replay.mjs'
import {readCanonicalEvidence} from './live-canonical-evidence.mjs'
import {fixedInputs} from './live-comparison-plan.mjs'
import {readPrivate,writePrivate,parseClosed} from './replay-report.mjs'

const here=dirname(fileURLToPath(import.meta.url)),repo=resolve(here,'../..')
const sha=v=>createHash('sha256').update(v).digest('hex')
const hash=path=>sha(readFileSync(path))
const pause=()=>new Promise(resolve=>setTimeout(resolve,50))
const put=(path,value)=>{const fd=openSync(path,constants.O_CREAT|constants.O_EXCL|constants.O_WRONLY|constants.O_NOFOLLOW,0o600);closeSync(fd);writePrivate(path,value)}
export function inventory(root) {
 const rows=[]
 const walk=(dir,prefix='')=>{for(const name of readdirSync(dir).sort()){
  const file=join(dir,name),relative=prefix+name,st=lstatSync(file)
  assert(!st.isSymbolicLink());if(st.isDirectory())walk(file,relative+'/');else{assert(st.isFile());rows.push([relative,hash(file)])}
  assert(rows.length<=20000)
 }}
 walk(root);return {sha256:sha(JSON.stringify(rows)),rows}
}
function sourceInventory(applicationRoot){
 const rows=[]
 for(const base of ['internal','web/src','scripts/release-quality/live-owner']){
  for(const [path,hash] of inventory(join(applicationRoot,base)).rows)if(!path.endsWith('_test.go')&&!/\.test\.[cm]?[jt]sx?$/.test(path))rows.push([base+'/'+path,hash])
 }
 for(const name of ['go.mod','go.sum'])rows.push([name,hash(join(applicationRoot,name))])
 return {sha256:sha(JSON.stringify(rows)),rows}
}
// Closed observations contain fixture step identifiers only, never DOM/auth/error prose.
export function setupObserver(retain) {
 return async (step,category,wait)=>{
  assert(/^[a-z-]+$/.test(step));assert(['presence','readiness'].includes(category))
  const record=state=>retain({kind:'offline-setup-observation',provenance:'offline-qualification',step,category,state})
  record('waiting')
  try{const value=await wait();record('ready');return value}catch(error){record('failed');throw error}
 }
}
export async function signInAndOpenConnections(page,origin,observe) {
 // SignInEntry awaits password/login, keeps its normal session, then reloads.
 // SessionGate acknowledges /api/session and mounts AppShell only after verification.
 // Subscribe before click so neither a fast reload nor its session response is missed.
 await Promise.all([
  observe('sign-in-reload','readiness',()=>page.waitForEvent('framenavigated',{predicate:frame=>frame===page.mainFrame()&&frame.url()===origin+'/',timeout:15000})),
  observe('sign-in-session','readiness',()=>page.waitForResponse(response=>response.url()===origin+'/api/session'&&response.ok(),{timeout:15000})),
  observe('sign-in-submit','readiness',()=>page.getByRole('button',{name:'Sign in',exact:true}).click())
 ])
 await observe('authenticated-shell','presence',()=>page.getByRole('link',{name:'Admin',exact:true}).waitFor({state:'visible'}))
 await observe('connections-navigation','readiness',()=>page.goto(origin+'/admin#connections-ai'))
}
export const baselineIdentity=Object.freeze({commit:'ef378c406f8b96e2c0c8854bc5e4dff40ac841e2',tree:'0034c3623b5b6e9bb8741da900392fd1c29e1508',readerSha256:'2229691d8fc96f5a1a818a9aa53788dceebcc4ab6abbe2e4913f42562922ec53'})
export function validateApplicationRole({role='candidate',applicationRoot=repo,baseCommit,baseTree,baselineReader=null}) {
 assert(isAbsolute(applicationRoot));assert(['candidate','baseline'].includes(role))
 if(role==='candidate'){assert.equal(applicationRoot,repo);assert.equal(baselineReader,null)}
 else {
  assert.notEqual(applicationRoot,repo);assert.equal(baseCommit,baselineIdentity.commit);assert.equal(baseTree,baselineIdentity.tree)
  assert.deepEqual(baselineReader,{path:join(applicationRoot,'scripts/release-quality/baseline-replay-report.mjs'),sha256:baselineIdentity.readerSha256})
  assert.equal(hash(baselineReader.path),baselineReader.sha256)
 }
 return {role,applicationRoot,baselineReader}
}
export function validateCaseEvidence(row,index,evidence,role) {
 if(role==='candidate'){
  const canonical=readCanonicalEvidence(evidence,row.evidence.canonical,{role,case:index+1,caseId:row.input.id,chatId:row.chatId,runId:row.runId})
  assert.equal(canonical.operations.filter(v=>v.kind==='model').length,1)
 }else {
  assert.equal(role,'baseline');assert.equal(row.evidence.canonical,'unavailable-baseline-contract')
  assert.equal(row.evidence.tools.length,index);assert.equal(row.evidence.runtimeTools.length,index)
  for(const native of row.evidence.tools){assert.equal(native.name,'desk_runtime_0');assert.deepEqual(native.args,{});assert.equal(native.structuredState,'unavailable-native-contract');assert.equal(native.isError,false);assert.equal(typeof native.text,'string');assert(native.text.includes('{"schema":"recorded"}'))}
  for(const runtime of row.evidence.runtimeTools){assert.equal(runtime.name,'get_schema');assert.deepEqual(runtime.args,{});assert.equal(runtime.text,'{"schema":"recorded"}');assert.equal(runtime.structuredState,'value');assert.deepEqual(runtime.structured,{schema:'recorded'});assert.equal(runtime.isError,false)}
 }
}
export async function qualifyOffline({out,helper,browserBin,baseCommit,baseTree,publicRoot,role='candidate',applicationRoot=repo,baselineReader=null}) {
 validateApplicationRole({role,applicationRoot,baseCommit,baseTree,baselineReader})
 for(const path of [out,helper,browserBin,publicRoot])assert(isAbsolute(path))
 assert(/^[a-f0-9]{40}$/.test(baseCommit)&&/^[a-f0-9]{40}$/.test(baseTree))
 const work=join(out,'run-1');mkdirSync(work,{mode:0o700})
 let stage='custody',reason='pending',owner,browser,page,ownerExited=false,ownerCode=null,contextClosed=false,timedOut=false
 const evidence=join(work,'evidence'),controlDir=join(work,'control'),projectRoot=join(work,'project'),config=join(work,'config')
 for(const dir of [evidence,controlDir,projectRoot,config])mkdirSync(dir,{mode:0o700})
 const trace=join(evidence,'fixture-lifecycle.jsonl');writeFileSync(trace,'',{flag:'wx',mode:0o600})
 const setupTrace=join(evidence,'setup-observations.jsonl');writeFileSync(setupTrace,'',{flag:'wx',mode:0o600})
 const settingsTrace=join(evidence,'capture-settings-observations.jsonl');writeFileSync(settingsTrace,'',{flag:'wx',mode:0o600})
 const terminalTrace=join(evidence,'capture-terminal-observations.jsonl');writeFileSync(terminalTrace,'',{flag:'wx',mode:0o600})
 const renderedTrace=join(evidence,'capture-rendered-observations.jsonl');writeFileSync(renderedTrace,'',{flag:'wx',mode:0o600})
 const observe=setupObserver(value=>appendFileSync(setupTrace,JSON.stringify(value)+'\n'))
 let status={kind:'offline-owner-browser-qualification',provenance:'offline-qualification',state:'incomplete',stage,reason,ownerPid:null,ownerExited:false,ownerExitCode:null,browserClosed:false,peers:null}
 let timer
 try {
  const peer=join(here,'offline-owner-browser-peer.py')
  const binary=role=>{const path=join(work,role+'-peer');assert(!peer.includes('"')&&!trace.includes('"'));writeFileSync(path,'#!/bin/sh\nexec /usr/bin/python3 "'+peer+'" '+role+' "'+trace+'" "$@"\n',{flag:'wx',mode:0o700});return path}
  const native=binary('native'),runtime=binary('runtime'),assets=join(applicationRoot,'web/dist')
  const sources=sourceInventory(applicationRoot),assetIdentity=inventory(assets)
  const sourceRecord=JSON.stringify({kind:'offline-source-inventory',provenance:'offline-qualification',baseCommit,baseTree,...sources})+'\n'
  assert(Buffer.byteLength(sourceRecord)<=4*1024*1024)
  writeFileSync(join(evidence,'dirty-source-inventory.json'),sourceRecord,{flag:'wx',mode:0o600})
  put(join(evidence,'assets-inventory.json'),{kind:'offline-assets-inventory',provenance:'offline-qualification',...assetIdentity})
  assert.equal(hash(join(publicRoot,'quality/release-slice.json')),'7728e5f53f9539c0343a86d6b4aa9351b1c8aad662acf35cd495dca36c91fe24')
  for(const input of fixedInputs)assert.equal(inventory(join(publicRoot,'corpus',input.id)).sha256,input.corpusSha256)
  const source={commit:baseCommit,tree:baseTree,inventory:sources.sha256,application:hash(helper),assets:assetIdentity.sha256,helper:hash(helper),browser:hash(browserBin),cli:hash(native),cliVersion:'supplied-offline-peer/1',driver:hash(join(here,'live-browser-replay.mjs')),reader:role==='baseline'?baselineReader.sha256:hash(join(here,'replay-report.mjs')),runtimeInput:hash(peer)}
  put(join(evidence,'custody.json'),{kind:'offline-custody',provenance:'offline-qualification',source,launcher:hash(fileURLToPath(import.meta.url)),peer:hash(peer),nativeWrapper:hash(native),runtimeWrapper:hash(runtime),ownerSources:inventory(join(applicationRoot,'scripts/release-quality/live-owner')).sha256,playwright:hash(join(repo,'web/node_modules/playwright-core/package.json')),canonicalEvidence:hash(join(here,'live-canonical-evidence.mjs'))})
  const o={ControlDir:controlDir,ProjectDir:projectRoot,ConfigDir:config,AssetsDir:assets,CodexBin:native,JpackBin:runtime,Model:'gpt-6-sol',Effort:'medium',Case1:fixedInputs[0].id,Case2:fixedInputs[1].id,SetupTimeout:120000000000}
  const control={kind:'live-two-chat-capture-input',role,model:'gpt-6-sol',effort:'medium',connectionId:'pending',connectionName:'Offline qualification',projectRoot,controlDir,evidenceDir:evidence,ownerConfigurationSha256:sha(JSON.stringify(o)),source,inputs:fixedInputs.map(v=>({...structuredClone(v),sliceSha256:'7728e5f53f9539c0343a86d6b4aa9351b1c8aad662acf35cd495dca36c91fe24'})),baselineReader}
  stage='owner-start'
  owner=spawn(helper,['serve','--control-dir',controlDir,'--project-dir',projectRoot,'--config-dir',config,'--assets-dir',assets,'--codex-bin',native,'--jpack-bin',runtime,'--model','gpt-6-sol','--effort','medium','--case1',fixedInputs[0].id,'--case2',fixedInputs[1].id,'--setup-timeout','120s'],{stdio:'ignore'})
  status.ownerPid=owner.pid
  const exit=new Promise(resolve=>{owner.once('exit',(code)=>{ownerExited=true;ownerCode=code;resolve()});owner.once('error',()=>{ownerExited=true;resolve()})})
  const io=privateOwnerIO(controlDir),start=Date.now()
  timer=setTimeout(()=>{timedOut=true;try{io.publish('abort.json',{action:'abort'})}catch{};void browser?.close()},180000)
  let descriptor
  while(!(descriptor=io.read('owner.json'))){assert(!ownerExited&&Date.now()-start<10000);await pause()}
  const {origin}=validateDescriptor(descriptor,control)
  stage='browser-start';browser=await chromium.launch({executablePath:browserBin,args:['--no-sandbox'],timeout:10000})
  const context=await browser.newContext({viewport:{width:1440,height:1000},locale:'en-CA'});context.on('close',()=>{contextClosed=true});page=await context.newPage();page.setDefaultTimeout(15000)
  let forbidden=0,bearer=''
  await context.route('**/*',async route=>{const u=new URL(route.request().url());if(u.origin!==origin||/\/api\/model-providers\/.*\/(?:login|install|logout)/.test(u.pathname)){forbidden++;await route.abort()}else await route.continue()})
  page.on('request',request=>{if(new URL(request.url()).pathname==='/api/ai-connections')void request.headerValue('authorization').then(v=>{bearer=v??bearer})})
  stage='owner-setup';await observe('initial-navigation','readiness',()=>page.goto(origin))
  await observe('setup-open','presence',()=>page.getByRole('button',{name:'Set up sign-in',exact:true}).click())
  await observe('setup-code','presence',()=>page.getByLabel('Owner setup code',{exact:true}).fill(descriptor.setupToken))
  await observe('setup-submit','readiness',()=>page.getByRole('button',{name:'Continue',exact:true}).click())
  await observe('password-open','presence',()=>page.getByRole('button',{name:'Set password',exact:true}).click())
  const password=page.getByRole('dialog',{name:'Set password',exact:true})
  await observe('password-new','presence',()=>password.getByLabel('New password',{exact:true}).fill('Fixture7'))
  await observe('password-confirm','presence',()=>password.getByLabel('Confirm new password',{exact:true}).fill('Fixture7'))
  await observe('password-submit','readiness',()=>password.getByRole('button',{name:'Set password',exact:true}).click())
  stage='owner-sign-in';await observe('sign-in-password','presence',()=>page.getByLabel('Desk password',{exact:true}).fill('Fixture7'))
  await signInAndOpenConnections(page,origin,observe)
  stage='connection-create';await observe('connection-open','presence',()=>page.getByRole('button',{name:'Add connection',exact:true}).click())
  const add=page.getByRole('dialog',{name:'Add AI connection',exact:true})
  await observe('connection-name','presence',()=>add.getByLabel('Name',{exact:true}).fill(control.connectionName))
  await observe('connection-submit','readiness',()=>add.getByRole('button',{name:'Add connection',exact:true}).click())
  stage='connection-models';await observe('connection-connected','presence',()=>page.getByText('Connected to ChatGPT',{exact:true}).waitFor())
  await observe('models-open','presence',()=>page.getByRole('button',{name:'Default models',exact:true}).click())
  await observe('model-select','presence',()=>page.getByRole('checkbox',{name:'gpt-6-sol',exact:true}).check())
  await observe('models-close','readiness',()=>page.keyboard.press('Escape'))
  await observe('effort-open','presence',()=>page.getByRole('combobox',{name:'Reasoning effort',exact:true}).click())
  await observe('effort-select','presence',()=>page.getByRole('option',{name:'medium',exact:true}).click())
  stage='connection-tools';await observe('tools-open','presence',()=>page.locator('summary').filter({hasText:'Allowed pack tools'}).click())
  const toolBox=page.locator('details').filter({has:page.locator('summary').filter({hasText:'Allowed pack tools'})}).getByRole('checkbox')
  const toolCount=await observe('tools-count','presence',()=>toolBox.count())
  for(let i=0;i<toolCount;i++){
   const box=toolBox.nth(i),name=await observe('tool-label','presence',()=>box.evaluate(node=>node.parentElement.textContent))
   if(name.trim()==='get_schema')await observe('tool-select','readiness',()=>box.check())
   else if(await observe('tool-checked','readiness',()=>box.isChecked()))await observe('tool-clear','readiness',()=>box.uncheck())
  }
  await observe('settings-save','readiness',()=>page.getByRole('button',{name:'Save shared settings',exact:true}).click())
  await observe('settings-saved','presence',()=>page.getByText('Assistant settings saved.',{exact:true}).waitFor())
  stage='connection-readback';assert(bearer)
  const response=await observe('connection-readback','readiness',()=>page.request.get(origin+'/api/ai-connections',{headers:{Authorization:bearer},timeout:10000}))
  assert(response.ok())
  const registry=parseClosed(await observe('connection-readback-body','readiness',()=>response.text()))
  assert.equal(registry.connections.length,1);const connection=registry.connections[0];assert.equal(connection.name,control.connectionName);assert.deepEqual(connection.assistant.agent.tools,['get_schema']);assert.equal(connection.assistant.agent.model,'gpt-6-sol');assert.equal(connection.assistant.agent.effort,'medium');control.connectionId=connection.id;bearer=''
  assert.equal(forbidden,0);await observe('capture-navigation','readiness',()=>page.goto(origin))
  stage='two-chat-capture'
  const result=await captureLiveSession({page,control,observeRendered:value=>appendFileSync(renderedTrace,JSON.stringify({provenance:'offline-qualification',...value})+'\n'),observeTerminal:value=>appendFileSync(terminalTrace,JSON.stringify({provenance:'offline-qualification',...value})+'\n'),observeSettings:value=>appendFileSync(settingsTrace,JSON.stringify({provenance:'offline-qualification',...value})+'\n'),retain:value=>{const name=value.state==='case-captured'?'case-'+value.cases.length+'.json':value.state==='complete'?'capture.json':'capture-incomplete.json';put(join(evidence,name),{kind:'offline-owner-browser-observation',provenance:'offline-qualification',capture:value})}})
  contextClosed=true;await exit;assert.equal(ownerCode,0)
  assert.equal(result.state,'complete');assert.deepEqual(result.cases.map(v=>v.accounting.invocation),[1,2]);assert.notEqual(result.cases[0].chatId,result.cases[1].chatId)
  for(const [index,row] of result.cases.entries()){validateCaseEvidence(row,index,evidence,role);assert.equal(row.answers.length,1);assert.equal(row.answers[0].text,'Offline fixture answer.');assert.equal(row.answers[0].renderedText,'Offline fixture answer.');assert.equal(row.answers[0].reloadedText,'Offline fixture answer.')}
  assert.equal(forbidden,0);assert(!timedOut)
  stage='complete';reason='complete';status.state='complete'
 }catch(error){reason=timedOut?'deadline':error?.code==='ENOENT'?'missing-file':error?.code==='EACCES'?'permission':error?.code==='AssertionError'||error?.name==='AssertionError'?'assertion':error?.name==='TimeoutError'?'timeout':'refused'}
 finally{
  clearTimeout(timer)
  if(owner&&!ownerExited){try{privateOwnerIO(controlDir).publish('abort.json',{action:'abort'})}catch{};owner.kill('SIGTERM');const end=Date.now()+10000;while(!ownerExited&&Date.now()<end)await pause()}
  if(browser){try{await browser.close();status.browserClosed=true}catch{reason='browser-cleanup';status.state='incomplete'}}
  const rows=readFileSync(trace,'utf8').trim().split('\n').filter(Boolean).map(line=>parseClosed(line)),pids=[...new Set(rows.map(v=>v.pid))]
  const peers=pids.map(pid=>{let absent=false;try{process.kill(pid,0)}catch(e){absent=e.code==='ESRCH'}return {pid,absent}})
  if(owner&&!ownerExited||peers.some(v=>!v.absent)){status.state='incomplete';reason='cleanup-incomplete'}
  Object.assign(status,{stage,reason,ownerExited,ownerExitCode:ownerCode,contextClosed,peers,traceCounts:{turns:rows.filter(v=>v.kind==='native-turn').length,callbacks:rows.filter(v=>v.kind==='callback').length,schema:rows.filter(v=>v.kind==='schema').length,refused:rows.filter(v=>v.kind==='refused').length}})
  if(status.state==='complete'&&JSON.stringify(status.traceCounts)!==JSON.stringify({turns:2,callbacks:1,schema:1,refused:0})){status.state='incomplete';status.stage='trace';status.reason='trace-mismatch'}
  put(join(evidence,'status.json'),status)
 }
 return status
}
if(process.argv[1]===fileURLToPath(import.meta.url)){
 const [out,helper,browserBin,baseCommit,baseTree,publicRoot]=process.argv.slice(2)
 try{const result=await qualifyOffline({out,helper,browserBin,baseCommit,baseTree,publicRoot});process.stdout.write(JSON.stringify({provenance:result.provenance,state:result.state,stage:result.stage,reason:result.reason})+'\n');if(result.state!=='complete')process.exitCode=1}catch{process.stderr.write('offline qualification custody/setup refused\n');process.exitCode=1}
}
