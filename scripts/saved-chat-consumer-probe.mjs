import assert from 'node:assert/strict'
import { SavedChatRun } from '../web/src/assistant/serverRuns/controller.ts'
import { serverRunClient } from '../web/src/assistant/serverRuns/client.ts'
import { reconcileChats } from '../web/src/chat/reconcile.ts'
import { withPackChecks } from '../web/src/assistant/serverRuns/presentation.ts'
import { INITIAL_STATE } from '../web/src/research/initialState.ts'
import { encodeWireChat } from '../web/src/chat/wire.ts'
import { decodeSnapshot } from '../web/src/assistant/serverRuns/decode.ts'
import pending from '../web/src/assistant/serverRuns/fixtures/pending.json'
import approval from '../web/src/assistant/serverRuns/fixtures/approval.json'
const chat={id:'chat-one',title:'Chat',model:'fixture',mode:'draft',view:'chat',composer:'',pinned:false,archived:false,updatedAt:'2026-10-20T00:00:00Z'}
let prepared=0,posts=0,start
const response=()=>({...pending,state:'failed',admissionId:start.admissionId,requestId:start.requestId})
globalThis.fetch=async(path,init)=>{
 if(path==='/api/assistant-runs'){
  posts++;start=JSON.parse(init.body)
  if(posts===1)throw Error('uncertain delivery')
  return Response.json(response())
 }
 return Response.json({runs:[response()]})
}
const run=new SavedChatRun(chat.id,chat,{project:'/project',client:serverRunClient,prepare:async()=>{prepared++;return 'a'.repeat(64)},reconcile:async()=>chat,pin(){},problem(){}})
run.send('Prepared original input','Original display')
for(let i=0;i<30&&run.getSnapshot().status==='running';i++)await new Promise(r=>setTimeout(r,0))
assert.equal(prepared,1,'original request must be saved before admission')
assert.equal(posts,1,'lost acknowledgement must never repeat POST')
assert.equal(run.canRetryResponse,false,'terminal without acknowledgement must stay pinned')
const binding={owner:pending.owner,requestId:pending.requestId}
assert.throws(()=>decodeSnapshot({...pending,admissionId:undefined},binding),'admission identity must remain required')
assert.throws(()=>decodeSnapshot({...approval,approvalReference:undefined},binding),'asked approval must carry its exact reference')
assert.throws(()=>reconcileChats([{...chat,composer:'base'}],[{...chat,composer:'local'}],[{...chat,composer:'remote'}]),'same-field conflict must retain local draft without write')
assert.equal(encodeWireChat({id:'one',n:9007199254740992},'{"id":"one","n":9007199254740993}',new Set(['n'])),'{"id":"one","n":9007199254740992}','an edited numeric field must not reuse old raw bytes')

const candidate={text:'{}',document:{},digest:'e'.repeat(64),revision:1,producedBy:'conversation',packReview:{basis:{packId:'one',path:'one.json',text:'{}'},check:{documentDigest:'e'.repeat(64),valid:true}}}
const latest={documentDigest:candidate.digest,suiteKey:'[]',at:'2026-10-23T00:00:00Z',valid:false,diagnostics:[],totalCases:0}
assert.equal(withPackChecks({...INITIAL_STATE,candidates:[candidate]},[latest]).candidates[0].packReview.check.valid,false,'latest failed check must replace older pass')
