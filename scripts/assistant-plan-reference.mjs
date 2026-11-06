// Fresh Go output is consumed by the existing chat and canonical Plan owners.
import assert from 'node:assert/strict'
import {readFile} from 'node:fs/promises'
import {join} from 'node:path'
import React from '../web/node_modules/react/index.js'
import {renderToStaticMarkup} from '../web/node_modules/react-dom/server.node.js'
import {ChatStore} from '../web/src/chat/store.ts'
import {deskPlanResolver,validDeskPlans} from '../web/src/assistant/planning/deskPlan.ts'
import {parseJsonText} from '../web/src/research/verify/canon.ts'
import {PlanCard} from '../web/src/assistant/planning/PlanCard.tsx'

import {validPlans} from '../web/node_modules/@judgment-pack/harness/dist/planning/model.js'
import {checkPlan} from '../web/node_modules/@judgment-pack/harness/dist/planning/tools.js'
const currentChat=(store,id)=>[...store.getSnapshot().chats,...store.getSnapshot().drafts].find(c=>c.id===id)
import vectors from '../internal/desk/testdata/assistant-plan-v1.json'
const dir=process.argv[2]
assert(dir,'fresh Go producer directory required')
for(const key of ['plans','accepted','started','mixed'])assert(validPlans(vectors[key])&&validDeskPlans(vectors[key]),'handwritten '+key)
// Desk deliberately retains locator tool inputs; output references use the
// current opaque schema. This is a host-specific command, not generic parity.
for(const raw of vectors.invalidReferences){
 let rejected=false
 try{parseJsonText(raw);rejected=!validDeskPlans([{...vectors.mixed[0],references:[JSON.parse(raw)]}])}catch{rejected=true}
 assert(rejected,'handwritten malformed reference must refuse: '+raw)
}
for(const length of [256,257])assert.equal(validDeskPlans([{...vectors.mixed[0],references:[{kind:'artifact',id:'x'.repeat(length),digest:'a'.repeat(64)}]}]),length===256)
assert(!validDeskPlans([{...vectors.mixed[0],references:[vectors.mixed[0].references[2],vectors.mixed[0].references[2]]}]))
globalThis.window={location:{host:'fixture',pathname:'/'}}
const storage=new Map([['jpack-desk-session:fixture','fixture-bearer']])
globalThis.sessionStorage={getItem:key=>storage.get(key)??null,setItem:(key,value)=>storage.set(key,value),removeItem:key=>storage.delete(key)}
globalThis.window.sessionStorage=globalThis.sessionStorage
assert.deepEqual(JSON.parse(await readFile(join(dir,'input-schema.json'),'utf8')),{type:'object',properties:{objective:{type:'string',maxLength:2000},steps:{type:'array',minItems:1,maxItems:20,items:{type:'string',maxLength:1000}},questions:{type:'array',maxItems:10,items:{type:'string',maxLength:1000}},references:{type:'array',maxItems:6,items:{type:'object',properties:{kind:{type:'string',enum:['file','dialect']},id:{type:'string',maxLength:1024}},required:['kind','id'],additionalProperties:false}}},required:['objective','steps','questions','references'],additionalProperties:false})
let sources=0
for(const engine of ['api','codex']){
 let doc=JSON.parse(await readFile(join(dir,engine+'.json'),'utf8')),writes=0,starts=0,stale=false,revision=1
 const savedReply=JSON.parse(await readFile(join(dir,engine+'-reply.json'),'utf8')),lookups=JSON.parse(await readFile(join(dir,engine+'-references.json'),'utf8'))
 let boundPlan,lookupCalls=0
 globalThis.fetch=async(path,init)=>{
  if(path==='/api/auth/status')return Response.json({enabled:true,unavailable:false,sessionActive:true})
  assert.equal(init.headers.Authorization,'Bearer fixture-bearer');assert.equal(init.headers['If-Match'],savedReply.chatRevisions['chat-one']);assert.equal(init.headers['X-Plan-Revision'],String(boundPlan.revision))
  const handle=decodeURIComponent(path.split('/').at(-1));assert.equal(path,`/api/conversations/chat-one/plans/${boundPlan.id}/references/${handle}`)
  const found=lookups[handle];assert(found,'Go endpoint must have resolved saved handle');lookupCalls++
  return Response.json({...found,digest:stale&&found.kind==='artifact'?'f'.repeat(64):found.digest})
 }
 const io={read:async()=>({project:'fixture',sha256:String(revision),content:doc,chatRevisions:savedReply.chatRevisions}),write:async(content,digest)=>{assert.equal(digest,String(revision));writes++;revision++;doc=structuredClone(content);return {project:'fixture',sha256:String(revision),content:doc}}}
 const draftIO={read:async()=>({project:'fixture',sha256:'empty',content:{version:1,drafts:[],deleted:[]}}),write:async()=>{throw Error('Plan does not write artifact drafts')}}
 const store=new ChatStore('fixture',io,draftIO)
 try{
  await store.load();assert.equal(store.getSnapshot().error,'')
  const chat=currentChat(store,'chat-one');assert(chat);assert.equal(chat.interaction,'plan')
  assert(validPlans(chat.plans));assert.equal(chat.plans.length,engine==='api'?2:1)
  const plan=chat.plans.at(-1);boundPlan=plan;assert.equal(plan.status,'proposed');assert.equal(plan.objective,'Review exact Δ 💡');assert.equal(plan.references.length,2)
  if(engine==='api'){assert.equal(chat.plans[0].status,'superseded');assert.deepEqual(chat.plans[0].questions,['Which review?'])}
  const html=renderToStaticMarkup(React.createElement(PlanCard,{chat,store,onStart:()=>{starts++;return true},history:true}))
  assert(html.includes('Review exact'));assert(html.includes('Review the Dialect'));assert.equal(starts,0);assert.equal(writes,0)
  assert(plan.references.every(ref=>['artifact','dialect'].includes(ref.kind)&&ref.id.startsWith('plan-ref-')&&/^[a-f0-9]{64}$/.test(ref.digest)&&!('revision' in ref)))
  const resolver=deskPlanResolver(plan,{chat:chat.id,revision:signal=>store.reviewedPlanRevision(chat.id,plan,signal)})
  await checkPlan(plan,new AbortController().signal,resolver)
  stale=true;await assert.rejects(store.acceptReviewedPlan(chat.id,plan,resolver,new AbortController().signal),/changed/);assert.equal(currentChat(store,chat.id).interaction,'plan');assert.equal(writes,0)
  stale=false;await store.acceptReviewedPlan(chat.id,plan,resolver,new AbortController().signal)
  assert.equal(currentChat(store,chat.id).interaction,'work');assert.equal(currentChat(store,chat.id).plans.at(-1).status,'accepted');assert.equal(starts,0)
  const restored=new ChatStore('fixture',io,draftIO)
  try{await restored.load();assert.equal(restored.getSnapshot().error,'');assert.equal(currentChat(restored,chat.id).plans.at(-1).status,'accepted')}finally{restored.dispose()}
  assert.equal(lookupCalls,5,'production resolver must consume Go endpoint replies during shared checks and durable acceptance')
  sources++
 }finally{store.dispose()}
}
console.log(`Plan shared readers: ${sources} actual API/native projections, closed mixed readers, PlanCard/checkPlan, production saved-CAS resolver over actual Go endpoint replies and Desk durable acceptance; review questions and human Start work boundary passed.`)
