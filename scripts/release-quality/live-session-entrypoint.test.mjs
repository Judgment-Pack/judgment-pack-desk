import test from 'node:test'
import assert from 'node:assert/strict'
import {EventEmitter} from 'node:events'
import {createHash} from 'node:crypto'
import {spawnSync} from 'node:child_process'
import {fileURLToPath} from 'node:url'
import {runLiveSession,ownerArguments,disabledManifest} from './live-session-entrypoint.mjs'
import {fixedInputs} from './live-comparison-plan.mjs'
const H='a'.repeat(64),secret='DO-NOT-RETAIN-CHALLENGE',sha=v=>createHash('sha256').update(JSON.stringify(v)).digest('hex')
function fixture(role='candidate'){
 const source={commit:role==='baseline'?'ef378c406f8b96e2c0c8854bc5e4dff40ac841e2':'c'.repeat(40),tree:role==='baseline'?'0034c3623b5b6e9bb8741da900392fd1c29e1508':'d'.repeat(40),inventory:H,application:H,assets:H,helper:H,browser:H,cli:H,cliVersion:'supplied-version',driver:H,reader:H,runtimeInput:H}
 const control={kind:'live-two-chat-capture-input',role,model:'explicit-model',effort:'medium',connectionId:'pending',connectionName:'Measured subscription',projectRoot:'/supplied/project',controlDir:'/supplied/control',evidenceDir:'/supplied/evidence',source,inputs:fixedInputs.map(v=>({...structuredClone(v),sliceSha256:'7728e5f53f9539c0343a86d6b4aa9351b1c8aad662acf35cd495dca36c91fe24'})),baselineReader:role==='baseline'?{path:'/supplied/baseline-reader.mjs',sha256:H}:null}
 const options={control,ownerPath:'/supplied/owner',browserPath:'/supplied/chrome',configDir:'/supplied/config',assetsDir:'/supplied/assets',codexPath:'/supplied/codex',runtimePath:'/supplied/runtime',startupMs:100,setupMs:50,cleanupMs:100}
 control.ownerConfigurationSha256=sha({ControlDir:control.controlDir,ProjectDir:control.projectRoot,ConfigDir:options.configDir,AssetsDir:options.assetsDir,CodexBin:options.codexPath,JpackBin:options.runtimePath,Model:control.model,Effort:control.effort,Case1:control.inputs[0].id,Case2:control.inputs[1].id,SetupTimeout:options.setupMs*1000000})
 const log=[],owner=new EventEmitter();owner.pid=1234
 const descriptor={baseUrl:'http://127.0.0.1:8123',setupToken:secret,configurationSha256:control.ownerConfigurationSha256,setupTimeoutMs:50,caseTimeoutMs:120000,aggregateInferenceTimeoutMs:480000}
 let lifecycle=null,ended=false
 const finish=reason=>{if(ended)return;ended=true;lifecycle={normalOwnerShutdown:true,shutdownError:false,deadlineCallbacksJoined:true,reason};owner.emit('exit',0)}
 const io={read:name=>name==='owner.json'?descriptor:lifecycle,publish:(name,value)=>{assert.equal(name,'abort.json');assert.deepEqual(value,{action:'abort'});log.push('abort');finish('aborted')}}
 const page={goto:async url=>{assert.equal(url,descriptor.baseUrl)},setDefaultTimeout:()=>{}}
 const context={newPage:async()=>page,close:async()=>log.push('context-close')}
 const browser={newContext:async()=>{log.push('context-create');return context},close:async()=>log.push('browser-close')}
 const operator={verifyCustody:async()=>true,setupUI:async args=>{assert.equal(args.page,page);assert.equal(args.setupToken,secret);log.push('setup');return {connectionId:'selected-connection'}},verifyReady:async args=>{assert.equal(args.page,page);assert.equal(args.control.model,'explicit-model');log.push('readiness');return true}}
 const deps={ownerIO:io,verifyFiles:()=>{},spawn:(path,args,opts)=>{assert.equal(path,options.ownerPath);assert.deepEqual(args,ownerArguments(options));assert.deepEqual(opts,{stdio:'ignore'});log.push('owner');return owner},launchBrowser:async()=>browser,capture:async args=>{assert.equal(args.page,page);assert.equal(args.ownerIO,io);assert.equal(args.control.connectionId,'selected-connection');log.push('capture');finish('completed');return {state:'complete',cases:[{answers:['full one'],accounting:{invocation:1}},{answers:['full two'],accounting:{invocation:2}}],lifecycle}}}
 return {options,operator,deps,log,finish,descriptor}
}
test('default command and import are inert, even with execution-looking arguments',()=>{
 const path=fileURLToPath(new URL('./live-session-entrypoint.mjs',import.meta.url))
 const result=spawnSync(process.execPath,[path,'serve','--execute'],{encoding:'utf8'})
 assert.equal(result.status,0);assert.deepEqual(JSON.parse(result.stdout),disabledManifest);assert.equal(result.stderr,'')
 const imported=spawnSync(process.execPath,['--input-type=module','-e',`await import(${JSON.stringify(new URL('./live-session-entrypoint.mjs',import.meta.url).href)})`],{encoding:'utf8'})
 assert.equal(imported.status,0);assert.equal(imported.stdout,'');assert.equal(imported.stderr,'')
})
test('explicit inputs and owner options have no model or effort defaults',()=>{
 const {options}=fixture();const args=ownerArguments(options)
 assert.equal(args[args.indexOf('--model')+1],'explicit-model');assert.equal(args[args.indexOf('--effort')+1],'medium')
 for(const key of ['model','effort']){const changed=structuredClone(options);delete changed.control[key];assert.throws(()=>ownerArguments(changed))}
 for(const key of ['ownerPath','browserPath','startupMs','setupMs','cleanupMs']){const changed=structuredClone(options);delete changed[key];assert.throws(()=>ownerArguments(changed))}
 const changed=structuredClone(options);changed.control.inputs.reverse();assert.throws(()=>ownerArguments(changed))
})
for(const role of ['candidate','baseline'])test(role+' retains one setup context and capture sequence; shutdown precedes browser close',async()=>{
 const f=fixture(role),result=await runLiveSession(f.options,f.operator,f.deps)
 assert.equal(result.state,'complete');assert.equal(result.capture.cases[1].accounting.invocation,2);assert.equal(result.capture.cases[0].answers[0],'full one')
 assert.deepEqual(f.log,['owner','context-create','setup','readiness','capture','context-close','browser-close']);assert(!JSON.stringify(result).includes(secret));assert.equal(result.monetaryCost,null);assert.equal(result.answerChangeDecision,'pending')
})
test('startup descriptor failure aborts actual owner control and awaits shutdown',async()=>{
 const f=fixture();f.descriptor.configurationSha256='b'.repeat(64)
 const result=await runLiveSession(f.options,f.operator,f.deps)
 assert.equal(result.state,'incomplete');assert.equal(result.reason,'startup');assert.equal(result.cleanupComplete,true);assert.deepEqual(f.log,['owner','abort'])
})
test('setup errors and secrets remain unretained; capture never starts',async()=>{
 const f=fixture();f.operator.setupUI=async()=>{throw new Error(secret)}
 const result=await runLiveSession(f.options,f.operator,f.deps)
 assert.equal(result.reason,'setup');assert.equal(result.cleanupComplete,true);assert(!f.log.includes('capture'));assert(f.log.includes('abort'));assert(!JSON.stringify(result).includes(secret))
})
test('setup timeout and late callback cannot enter capture',async()=>{
 const f=fixture();f.operator.setupUI=async()=>{await new Promise(resolve=>setTimeout(resolve,80));return {connectionId:'late'}}
 const result=await runLiveSession(f.options,f.operator,f.deps);await new Promise(resolve=>setTimeout(resolve,50))
 assert.equal(result.reason,'setup');assert(!f.log.includes('capture'));assert.equal(result.cleanupComplete,true)
})
test('unverified readiness and missing capture evidence remain incomplete',async()=>{
 const f=fixture();f.operator.verifyReady=async()=>false
 assert.equal((await runLiveSession(f.options,f.operator,f.deps)).state,'incomplete');assert(!f.log.includes('capture'))
 const g=fixture();g.deps.capture=async()=>({state:'complete',cases:[{},{}]})
 const result=await runLiveSession(g.options,g.operator,g.deps);assert.equal(result.state,'incomplete');assert.equal(result.reason,'capture');assert(g.log.includes('abort'))
})
test('missing owner shutdown evidence cannot claim cleanup',async()=>{
 const f=fixture();f.deps.ownerIO.publish=()=>f.finish('aborted');f.deps.ownerIO.read=name=>name==='owner.json'?f.descriptor:null;f.operator.setupUI=async()=>{throw new Error(secret)}
 const result=await runLiveSession(f.options,f.operator,f.deps);assert.equal(result.state,'incomplete');assert.equal(result.cleanupComplete,false);assert.equal(result.reason,'cleanup')
})
