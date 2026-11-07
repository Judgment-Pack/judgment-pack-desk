// Finite offline proposal journey contract. No provider or authority inference.
import assert from 'node:assert/strict'
export {readPrivate,writePrivate,digest} from './replay-report.mjs'
import {keys,digest} from './replay-report.mjs'
export const cases=['api-save','native-apply-save','native-dirty-refusal']
export const original='{"specVersion":"0.2.0-draft","id":"https://example.invalid/judgment-packs/minimal","version":"0.1.0","title":"A minimal pack","decision":{"intent":"Decide the one thing this pack decides.","question":"Does this request proceed?"},"outcomes":[{"id":"proceed","label":"Proceed"},{"id":"hold","label":"Hold"}],"rules":[{"id":"always","description":"Proceed.","when":{"op":"literal","value":true},"outcome":"proceed","onUnknown":"escalate"}]}\n'
export const candidate=original.replace('"version":"0.1.0"','"version":"0.1.1"')
export const prompt='Propose the linked saved pack with version 0.1.1 only. Keep all saved test expectations and policy unchanged. Use propose_saved_pack once, then finish.'
export function control(c){
 keys(c,['kind','caseId','origin','setupCode','browserBin','reportPath','suitePath','original','candidate','prompt'])
 assert.equal(c.kind,'offline-persisted-proposal-control');assert(cases.includes(c.caseId));assert.equal(c.original,original);assert.equal(c.candidate,candidate);assert.equal(c.prompt,prompt)
 const u=new URL(c.origin);assert.equal(u.origin,c.origin);assert.equal(u.protocol,'http:');assert.equal(u.hostname,'127.0.0.1');assert(u.port);assert(!u.username&&!u.password)
 assert(/^[A-Za-z0-9_-]{20,128}$/.test(c.setupCode));for(const p of [c.browserBin,c.reportPath,c.suitePath])assert(p.startsWith('/'))
 return c
}
function checked(c){
 keys(c,['documentDigest','suiteKey','at','valid','diagnostics','totalCases','report'])
 assert.equal(c.documentDigest,digest(candidate));assert.equal(typeof c.suiteKey,'string');assert(c.suiteKey.length<=8192);assert.equal(typeof c.at,'string');assert(c.at.length<=64);assert.equal(c.valid,true);assert.deepEqual(c.diagnostics,[]);assert.equal(c.totalCases,1)
 assert.deepEqual(c.report,{status:'passed',summary:{total:1,passed:1},packs:[{rows:[{id:'existing',expected:'proceed',actual:'proceed',status:'passed'}],coverage:[]}]})
}
export const stages=['bootstrap','settings','dispatch','terminal','proposal','action','navigation','click-start','click-return','file-observed','readback','reload','complete']
export const saveMessages={
 'Switch to Work before saving a proposal.':'plan',
 'The original document is unavailable. Request a new proposal.':'basis',
 'Save or discard your editor changes before saving this proposal.':'dirty',
 'Connect the runtime to check and save this proposal.':'runtime',
 'Check the proposal before saving.':'unchecked',
 'Resolve the validation findings and test mismatches before saving this proposal.':'findings',
 'The proposal or editor changed. Review it again before saving.':'changed',
 'Reconnect the current proposal before checking it.':'reconnect',
 'The project changed during the checks.':'project',
 'The pack changed before saving. Refresh and review the current version.':'stale',
 'The save could not be completed or verified. Refresh the pack before trying again.':'generic',
 'The pack changed since this request. Ask for a proposal against the current version.':'basis-stale',
 'This pack is read-only.':'read-only',
 'The saved test suite changed. Check the proposal and review the results again.':'suite-changed',
 'The proposal could not be applied completely. No write was attempted.':'apply',
 'The save could not be verified. Refresh the pack before trying again.':'verify'
}
export const readOnlyCodes=['none','signing-custody','runtime-lock','audit-record','file-permissions','other']
export const exceptionFunctions=['checkProposal','readTests','savePackProposal','usePackProposalSave','ServerRunClient']
export function exceptionObservation(v){
 keys(v,['atMs','reason','exceptionClass','frames','projection','guards'])
 assert(Number.isSafeInteger(v.atMs)&&v.atMs>=0&&v.atMs<=180000);assert(['exception','promiseRejection','other'].includes(v.reason));assert(['RunRequestError','Error','TypeError','RangeError','DOMException','Other'].includes(v.exceptionClass))
 assert(Array.isArray(v.frames)&&v.frames.length<=8)
 for(const f of v.frames){keys(f,['script','function','line','column']);assert.equal(f.script,'web/dist/assets/index-0KiWpcLq.js');assert(exceptionFunctions.includes(f.function));for(const k of ['line','column'])assert(Number.isSafeInteger(f[k])&&f[k]>=0&&f[k]<=2000000)}
 assert(['none','complete','unavailable','timed-out'].includes(v.projection))
 if(v.guards!==null){keys(v.guards,['snapshotExists','snapshotHistorical','ended','busy','stateIsRunning','packReview']);for(const x of Object.values(v.guards))assert(typeof x==='boolean');assert.equal(v.projection,'complete')}
 else assert.notEqual(v.projection,'complete')
 return v
}
const terminalStates=['complete','failed','interrupted','stopped','paused-budget','awaiting-input','awaiting-client']
// Completion is persisted before projection preparation, write and acknowledgement.
// Observe only finite terminal state/ack categories; never retain server prose.
export function projectionReady(run,observations,atMs){
 if(!terminalStates.includes(run.state))return false
 const acknowledged=typeof run.chatProjection?.acknowledged==='boolean'?run.chatProjection.acknowledged:null
 const previous=observations.find(v=>v.state===run.state&&v.acknowledged===acknowledged)
 if(previous)previous.count=Math.min(4096,previous.count+1)
 else observations.push({state:run.state,acknowledged,atMs,count:1})
 assert.equal(run.state,'complete')
 return acknowledged===true
}
export const dirtySteps=['source','original','fill','build','apply-disabled','save-disabled','flags','after-file','zero-writes','discard','confirm-text','confirm-discard','restore-source','restore-original']
export function dirtyObservation(v){
 keys(v,['step','edge','atMs','locator','dirty'])
 assert(dirtySteps.includes(v.step));assert(['before','after'].includes(v.edge));assert(Number.isSafeInteger(v.atMs)&&v.atMs>=0&&v.atMs<=180000)
 assert(v.dirty===null||typeof v.dirty==='boolean')
 if(v.locator!==null){keys(v.locator,['count','enabled','readOnly']);assert(Number.isSafeInteger(v.locator.count)&&v.locator.count>=0&&v.locator.count<=8);for(const k of ['enabled','readOnly'])assert(v.locator[k]===null||typeof v.locator[k]==='boolean')}
 return v
}
export function diagnostic(d){
 keys(d,['stages','click','button','messages','requests','readOnly','exceptions','stop',...(Object.hasOwn(d,'terminal')?['terminal']:[]),...(Object.hasOwn(d,'dirtySteps')?['dirtySteps']:[])])
 if(Object.hasOwn(d,'dirtySteps')){assert(Array.isArray(d.dirtySteps)&&d.dirtySteps.length<=28);for(const v of d.dirtySteps)dirtyObservation(v);assert.equal(new Set(d.dirtySteps.map(v=>v.step+':'+v.edge)).size,d.dirtySteps.length)}
 if(Object.hasOwn(d,'terminal')){
  assert(Array.isArray(d.terminal)&&d.terminal.length<=21)
  for(const v of d.terminal){keys(v,['state','acknowledged','atMs','count']);assert(terminalStates.includes(v.state));assert(v.acknowledged===null||typeof v.acknowledged==='boolean');assert(Number.isSafeInteger(v.atMs)&&v.atMs>=0&&v.atMs<=180000);assert(Number.isSafeInteger(v.count)&&v.count>0&&v.count<=4096)}
  assert.equal(new Set(d.terminal.map(v=>v.state+':'+v.acknowledged)).size,d.terminal.length)
 }
 keys(d.stages,stages);for(const v of Object.values(d.stages))assert(v===null||Number.isSafeInteger(v)&&v>=0&&v<=180000)
 assert(['not-started','pending','returned','failed'].includes(d.click));assert(['unknown','absent','enabled','disabled','saving'].includes(d.button))
 assert(Array.isArray(d.messages)&&d.messages.length<=16);for(const v of d.messages)assert(Object.values(saveMessages).includes(v))
 assert(['none','guard-observed','save-error-observed'].includes(d.stop))
 assert(Array.isArray(d.exceptions)&&d.exceptions.length<=24);for(const v of d.exceptions)exceptionObservation(v)
 assert(readOnlyCodes.includes(d.readOnly));assert(Array.isArray(d.requests)&&d.requests.length<=64)
 for(const v of d.requests){keys(v,['category','status','count']);assert(['file-read','file-write','suite-read','check','discover','run-read'].includes(v.category));assert(Number.isSafeInteger(v.status)&&v.status>=100&&v.status<=599);assert(Number.isSafeInteger(v.count)&&v.count>0&&v.count<=4096)}
 return d
}
export function report(r){
 keys(r,['kind','caseId','state','phase','errorName','runId','chatId','proposal','operations','freshChecks','settings','file','counts','ui','elapsedMs','diagnostic'])
 if(r.diagnostic!==null)diagnostic(r.diagnostic)
 assert.equal(r.kind,'offline-persisted-proposal-browser');assert(cases.includes(r.caseId));assert(['pending','incomplete','complete'].includes(r.state));assert(['bootstrap','settings','dispatch','terminal','proposal','action','readback','reload','complete'].includes(r.phase));assert([null,'Error','TimeoutError','AssertionError','TypeError','RangeError','Other'].includes(r.errorName));assert(Number.isSafeInteger(r.elapsedMs)&&r.elapsedMs>=0&&r.elapsedMs<=180000)
 if(r.runId!==null)assert(/^arun_[a-f0-9]+$/.test(r.runId));if(r.chatId!==null)assert(/^[a-zA-Z0-9_-]{1,128}$/.test(r.chatId))
 if(r.proposal!==null){keys(r.proposal,['text','digest','basis','check']);assert.equal(r.proposal.text,candidate);assert.equal(r.proposal.digest,digest(candidate));assert.deepEqual(r.proposal.basis,{packId:'one',path:'one.json',text:original});checked(r.proposal.check)}
 if(r.operations!==null){assert(Array.isArray(r.operations)&&r.operations.length<=32);for(const o of r.operations){keys(o,['id','callId','kind','name','state','intentSha256','resultSha256']);for(const k of ['id','callId','kind','name','state'])assert.equal(typeof o[k],'string');assert(/^[a-f0-9]{64}$/.test(o.intentSha256));assert(o.resultSha256===null||/^[a-f0-9]{64}$/.test(o.resultSha256))}}
 if(r.freshChecks!==null){assert(Array.isArray(r.freshChecks)&&r.freshChecks.length<=1);for(const c of r.freshChecks){checked(c)}}
 if(r.settings!==null){keys(r.settings,['model','effort','thinking','critic','research','mode','phase','phaseInput','criticInput']);assert.equal(r.settings.model,'gpt-6-sol');assert.equal(r.settings.critic,'off');assert.equal(r.settings.research,'provided');assert.equal(r.settings.mode,'draft');assert.equal(r.settings.phase,'work');assert.equal(r.settings.effort,r.caseId==='api-save'?null:'medium');assert.equal(r.settings.thinking,r.caseId==='api-save'?'off':null);for(const k of ['phaseInput','criticInput']){keys(r.settings[k],['state','value']);assert(['absent','value'].includes(r.settings[k].state));assert.equal(r.settings[k].value,r.settings[k].state==='absent'?null:k==='phaseInput'?'work':'off')}}
 if(r.file!==null){keys(r.file,['before','afterAction','final','reloaded','suiteBefore','suiteAfter']);for(const k of Object.keys(r.file))assert(r.file[k]===null||typeof r.file[k]==='string');assert.equal(r.file.before,original);if(r.file.suiteAfter!==null)assert.equal(r.file.suiteBefore,r.file.suiteAfter)}
 if(r.counts!==null){keys(r.counts,['starts','models','writes','checks','browserErrors','unexpected']);assert(Object.entries(r.counts).every(([k,v])=>k==='models'&&v===null&&r.state!=='complete'||Number.isSafeInteger(v)&&v>=0&&v<=64))}
 if(r.ui!==null){keys(r.ui,['applied','dirtyRefused','duplicateBlocked','reloadStable']);assert(Object.values(r.ui).every(v=>typeof v==='boolean'))}
 if(r.state==='complete'){
  if(r.diagnostic!==null)assert.equal(r.diagnostic.stop,'none')
  assert.equal(r.phase,'complete');assert.equal(r.errorName,null);assert(r.runId&&r.chatId&&r.proposal&&r.operations&&r.settings&&r.file&&r.ui)
  assert.deepEqual(r.counts,{starts:1,models:r.caseId==='api-save'?2:1,writes:r.caseId==='native-dirty-refusal'?0:1,checks:r.caseId==='api-save'?1:0,browserErrors:0,unexpected:0})
  assert.equal(r.file.final,r.caseId==='native-dirty-refusal'?original:candidate);assert.equal(r.file.reloaded,r.file.final);assert.equal(r.file.afterAction,r.caseId==='api-save'?candidate:original);assert.equal(r.ui.reloadStable,true);assert.equal(r.ui.duplicateBlocked,true);assert.equal(r.ui.applied,r.caseId==='native-apply-save');assert.equal(r.ui.dirtyRefused,r.caseId==='native-dirty-refusal');assert.equal(r.freshChecks.length,r.caseId==='api-save'?1:0)
  assert.equal(r.operations.filter(o=>o.name==='propose_saved_pack'&&o.kind==='host'&&o.state==='completed').length,1)
 }
 assert(Buffer.byteLength(JSON.stringify(r))<=65536);return r
}
