// Import and command execution are inert. Only a separately approved caller may
// invoke runLiveSession. Readiness, source qualification and approval remain
// operational prerequisites, never authority derived from saved plan text.
import assert from 'node:assert/strict'
import {spawn} from 'node:child_process'
import {readFileSync} from 'node:fs'
import {isAbsolute} from 'node:path'
import {fileURLToPath} from 'node:url'
import {createHash} from 'node:crypto'
import {captureLiveSession,privateOwnerIO,validateLiveControl,validateDescriptor} from './live-browser-replay.mjs'
const sha=v=>createHash('sha256').update(v).digest('hex')
const delay=ms=>new Promise(resolve=>setTimeout(resolve,ms))
const bound=async (work,ms)=>{let timer;try{return await Promise.race([work,new Promise((_,reject)=>{timer=setTimeout(()=>reject(new Error('deadline')),ms)})])}finally{clearTimeout(timer)}}
export const disabledManifest=Object.freeze({kind:'live-session-entrypoint',execution:'disabled',approval:'external-operational-prerequisite',accountAccess:'not-attempted'})

export function ownerArguments(o) {
 validateLiveControl(o.control)
 for(const key of ['ownerPath','browserPath','configDir','assetsDir','codexPath','runtimePath'])assert(isAbsolute(o[key]))
 for(const key of ['startupMs','setupMs','cleanupMs'])assert(Number.isSafeInteger(o[key])&&o[key]>0&&o[key]<=3600000)
 assert(o.setupMs<=3600000&&Number.isSafeInteger(o.setupMs*1000000))
 const c=o.control
 const configuration={ControlDir:c.controlDir,ProjectDir:c.projectRoot,ConfigDir:o.configDir,AssetsDir:o.assetsDir,CodexBin:o.codexPath,JpackBin:o.runtimePath,Model:c.model,Effort:c.effort,Case1:c.inputs[0].id,Case2:c.inputs[1].id,SetupTimeout:o.setupMs*1000000}
 assert.equal(sha(JSON.stringify(configuration)),c.ownerConfigurationSha256)
 assert.equal(c.source.application,c.source.helper)
 return ['serve','--control-dir',c.controlDir,'--project-dir',c.projectRoot,'--config-dir',o.configDir,'--assets-dir',o.assetsDir,'--codex-bin',o.codexPath,'--jpack-bin',o.runtimePath,'--model',c.model,'--effort',c.effort,'--case1',c.inputs[0].id,'--case2',c.inputs[1].id,'--setup-timeout',o.setupMs+'ms']
}
function verifyFiles(o) {
 for(const [path,want] of [[o.ownerPath,o.control.source.helper],[o.browserPath,o.control.source.browser],[o.codexPath,o.control.source.cli],[o.runtimePath,o.control.source.runtimeInput],[fileURLToPath(new URL('./live-browser-replay.mjs',import.meta.url)),o.control.source.driver]])assert.equal(sha(readFileSync(path)),want)
 if(o.control.baselineReader)assert.equal(sha(readFileSync(o.control.baselineReader.path)),o.control.baselineReader.sha256)
 else assert.equal(sha(readFileSync(new URL('./replay-report.mjs',import.meta.url))),o.control.source.reader)
}
async function launchBrowser(o){const {chromium}=await import('../../web/node_modules/playwright-core/index.mjs');return chromium.launch({executablePath:o.browserPath,headless:false,timeout:o.startupMs})}

// Operator callbacks receive a live page and AbortSignal, never an exported
// browser session. They must stop when signalled. Do not log their arguments,
// errors, DOM, screenshots or network traffic. No setup payload is retained here.
// `dependencies` exists for handwritten doubles, not an alternate provider path.
export async function runLiveSession(options,operator,dependencies={}) {
 const o=structuredClone(options),args=ownerArguments(o),control=o.control
 for(const key of ['verifyCustody','setupUI','verifyReady'])assert.equal(typeof operator?.[key],'function')
 const io=dependencies.ownerIO??privateOwnerIO(control.controlDir)
 const startOwner=dependencies.spawn??spawn,capture=dependencies.capture??captureLiveSession
 const verify=dependencies.verifyFiles??verifyFiles,openBrowser=dependencies.launchBrowser??launchBrowser
 const signalOwner=new AbortController()
 let owner,browser,context,exited=false,exitCode=null,exit=Promise.resolve(),stage='custody',result=null,reason='incomplete',cleanupComplete=false
 try {
  verify(o)
  // Verify the separately qualified source/assets/corpus and accounting/helper
  // overlays against this exact build before any owner/account activity.
  assert.equal(await bound(Promise.resolve(operator.verifyCustody({options:structuredClone(o),signal:signalOwner.signal})),o.startupMs),true)
  stage='startup'
  owner=startOwner(o.ownerPath,args,{stdio:'ignore'})
  exit=new Promise(resolve=>{owner.once('exit',code=>{exited=true;exitCode=code;resolve()});owner.once('error',()=>{exited=true;resolve()})})
  const descriptor=await bound((async()=>{for(;;){assert(!signalOwner.signal.aborted&&!exited);const d=io.read('owner.json');if(d)return d;await delay(25)}})(),o.startupMs)
  const {origin}=validateDescriptor(descriptor,control);assert.equal(descriptor.setupTimeoutMs,o.setupMs)
  stage='setup'
  // Include browser launch and both callbacks in the separate setup deadline.
  const page=await bound((async()=>{
   browser=await openBrowser(o);if(signalOwner.signal.aborted){await browser.close();throw new Error('setup-ended')}
   context=await browser.newContext({viewport:{width:1440,height:1000},locale:'en-CA'})
   const page=await context.newPage();page.setDefaultTimeout(Math.min(15000,o.setupMs))
   await page.goto(origin)
   const ready=await operator.setupUI({page,origin,setupToken:descriptor.setupToken,connectionName:control.connectionName,model:control.model,effort:control.effort,signal:signalOwner.signal})
   assert(!signalOwner.signal.aborted)
   assert.deepEqual(Object.keys(ready).sort(),['connectionId']);control.connectionId=ready.connectionId;validateLiveControl(control)
   assert.equal(await operator.verifyReady({page,control:structuredClone(control),signal:signalOwner.signal}),true)
   assert(!signalOwner.signal.aborted&&!exited)
   return page
  })(),o.setupMs)
  stage='capture'
  // Capture owns two fresh chats and final owner lifecycle, and retains full
  // private canonical/answer evidence using its existing bounded retainers.
  result=await bound(capture({page,control,ownerIO:io}),600000)
  // The owner enforces setupMs separately; the wrapper watchdog below is not
  // evidence of a token/currency cap or prospective invocation/callback cap.
  assert.equal(result?.state,'complete');assert.equal(result?.cases?.length,2)
  assert.equal(result?.lifecycle?.normalOwnerShutdown,true)
  reason='complete'
 }catch{reason=stage==='custody'?'custody':stage==='startup'?'startup':stage==='setup'?'setup':'capture'}
 finally{
  signalOwner.abort()
  if(owner&&!exited)try{io.publish('abort.json',{action:'abort'})}catch{}
  try{await bound(exit,o.cleanupMs);assert(!owner||exited);const lifecycle=owner?io.read('lifecycle.json'):null;assert(!owner||(lifecycle?.normalOwnerShutdown===true&&lifecycle?.shutdownError===false&&lifecycle?.deadlineCallbacksJoined===true));cleanupComplete=true;if(reason==='complete')assert.equal(exitCode,0)}catch{reason='cleanup'}
  // The real Go owner closes Server and reaps native clients. A timeout never
  // claims cleanup by killing a wrapper; retain the exact owner for inspection.
  try{if(context)await bound(context.close(),o.cleanupMs);if(browser)await bound(browser.close(),o.cleanupMs)}catch{cleanupComplete=false;reason='cleanup'}
 }
 return {state:reason==='complete'&&cleanupComplete?'complete':'incomplete',reason,role:control.role,ownerPid:Number.isSafeInteger(owner?.pid)?owner.pid:null,ownerExited:exited,ownerExitCode:exitCode,cleanupComplete,capture:result,answerChangeDecision:'pending',usageFinality:'unproven',monetaryCost:null}
}
if(process.argv[1]===fileURLToPath(import.meta.url))process.stdout.write(JSON.stringify(disabledManifest)+'\n')
