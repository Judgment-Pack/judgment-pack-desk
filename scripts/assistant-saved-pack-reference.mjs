// Shared readers and authenticated client over actual Go producer observations.
import assert from 'node:assert/strict'
import {readFile} from 'node:fs/promises'
import {webcrypto} from 'node:crypto'
import {AuthoringRun} from '../web/src/research/run.ts'
import {Ledger} from '../web/src/research/ledger.ts'
import {decodeCheckpoint} from '../web/src/chat/checkpoint.ts'
import {readPackReview,packVersion,pendingPackCandidate,packCheckAllowsAuto} from '../web/src/chat/packProposal.ts'
import {decodeStore} from '../web/src/packs/test-workspace/model.ts'
import {digestOf,jsonIdentity} from '../web/src/research/checkCandidate.ts'
import {ServerRunClient} from '../web/src/assistant/serverRuns/client.ts'
import {giveThisPageASessionForTesting} from '../web/src/mcp/session.ts'
if(!globalThis.crypto)globalThis.crypto=webcrypto
const [chatPath,suitePath,recheckPath,referencePath]=process.argv.slice(2)
assert(chatPath&&suitePath&&recheckPath&&referencePath,'actual Go observations required')
const [chats,suites,checked,reference]=await Promise.all([chatPath,suitePath,recheckPath,referencePath].map(async path=>JSON.parse(await readFile(path,'utf8'))))
const chat=chats.chats.find(c=>c.id==='chat-one')
let work=0
const unavailable=()=>{work++;throw Error('Reload must observe retained checks without Runtime or model calls')}
const run=new AuthoringRun({mode:'draft',ledger:new Ledger('fixture'),turn:unavailable,callTool:unavailable,researchTools:[],authorPrompt:'',maxRevisions:0,seconds:5,gateway:null,seal:unavailable,registry:unavailable,newSession:()=> 'fixture',log:()=>{},savedPackReview:{target:unavailable,check:unavailable}})
await run.restore(decodeCheckpoint(chat.checkpoint).state)
const state=run.getSnapshot()
assert.equal(work,0)
assert.equal(state.candidates.length,2)
for(const candidate of state.candidates){
 const review=readPackReview(candidate.packReview)
 assert(review?.check)
 assert.equal(review.basis.text,'{ "id":"one", "version":"1.0.0", "title":"Unsaved editor", "threshold":"50", "fixtureNumber":1.20 }')
 assert.equal(review.basis.packId,'one');assert.equal(review.basis.path,'one.json')
 assert.equal(review.check.documentDigest,candidate.digest)
 assert.equal(review.check.suiteKey,jsonIdentity(decodeStore(suites).suites.one.cases))
 assert.equal(review.check.totalCases,1)
 assert.equal(candidate.responseId,state.responses[0].id)
}
const latest=pendingPackCandidate(state)
assert.equal(packVersion(latest.text),'1.0.2')
assert.equal(latest.packReview.check.report.status,'passed')
assert.equal(packCheckAllowsAuto(latest.packReview.check),true,'readiness does not itself invoke any write')
assert.equal(state.candidates[0].packReview.check.report.status,'mismatch')
assert.equal(state.execution.used,3)
assert.equal(state.responses.length,1)
// The real Go recheck reference describes its own fixture proposal, independently
// of the API correction fixture above. The browser sends only these references.
let calls=0
giveThisPageASessionForTesting('fixture-session')
globalThis.fetch=async(path,options)=>{
 calls++
 assert.equal(path,`/api/assistant-runs/${reference.id}/pack-checks`)
 assert.deepEqual(JSON.parse(options.body),reference.request)
 assert.equal(options.headers.Authorization,'Bearer fixture-session')
 return new Response(JSON.stringify(checked),{status:200,headers:{'Content-Type':'application/json'}})
}
const client=new ServerRunClient('fixture-session')
const result=await client.recheckPack(reference,reference.request)
assert.equal(result.documentDigest,reference.request.documentDigest)
assert.equal(await digestOf(result.suiteKey),reference.request.suiteDigest)
assert.equal(result.report.status,'mismatch')
assert.equal(calls,1)
console.log('Saved-pack proposal: actual Go correction/basis/check/reload and explicit model-free recheck passed shared browser readers and client.')
