// Fresh Go output is consumed by the existing chat and canonical Plan owners.
import assert from 'node:assert/strict'
import {readFile} from 'node:fs/promises'
import {join} from 'node:path'
import React from '../web/node_modules/react/index.js'
import {renderToStaticMarkup} from '../web/node_modules/react-dom/server.node.js'
import {ChatStore} from '../web/src/chat/store.ts'
import {PlanCard} from '../web/src/assistant/planning/PlanCard.tsx'
import {configureHarness} from '../web/node_modules/@judgment-pack/harness/dist/host.js'
import {validPlans} from '../web/node_modules/@judgment-pack/harness/dist/planning/model.js'
import {acceptPlan,currentChat,planTools} from '../web/node_modules/@judgment-pack/harness/dist/planning/tools.js'
import vectors from '../internal/desk/testdata/assistant-plan-v1.json'
const dir=process.argv[2]
assert(dir,'fresh Go producer directory required')
for(const key of ['plans','accepted','started'])assert(validPlans(vectors[key]),'handwritten '+key)
assert.equal(planTools({},'unused')[0].effect,'plan')
assert.deepEqual(planTools({},'unused')[0].inputSchema,JSON.parse(await readFile(join(dir,'input-schema.json'),'utf8')))
let sources=0
for(const engine of ['api','codex']){
 let doc=JSON.parse(await readFile(join(dir,engine+'.json'),'utf8')),writes=0,starts=0,stale=false
 const io={read:async()=>({project:'fixture',sha256:'source',content:doc}),write:async content=>{writes++;doc=content;return {project:'fixture',sha256:'saved',content}}}
 const draftIO={read:async()=>({project:'fixture',sha256:'empty',content:{version:1,drafts:[],deleted:[]}}),write:async()=>{throw Error('Plan does not write artifact drafts')}}
 const store=new ChatStore('fixture',io,draftIO)
 try{
  await store.load();assert.equal(store.getSnapshot().error,'')
  const chat=currentChat(store,'chat-one');assert(chat);assert.equal(chat.interaction,'plan')
  assert(validPlans(chat.plans));assert.equal(chat.plans.length,engine==='api'?2:1)
  const plan=chat.plans.at(-1);assert.equal(plan.status,'proposed');assert.equal(plan.objective,'Review exact Δ 💡');assert.equal(plan.references.length,2)
  if(engine==='api'){assert.equal(chat.plans[0].status,'superseded');assert.deepEqual(chat.plans[0].questions,['Which review?'])}
  const html=renderToStaticMarkup(React.createElement(PlanCard,{chat,store,onStart:()=>{starts++;return true},history:true}))
  assert(html.includes('Review exact'));assert(html.includes('Review the Dialect'));assert.equal(starts,0);assert.equal(writes,0)
  configureHarness({readFile:async id=>{const ref=plan.references.find(ref=>ref.kind==='file'&&ref.id===id);assert(ref);return {sha256:stale?'f'.repeat(64):ref.revision}},dialectAPI:async path=>{const ref=plan.references.find(ref=>ref.kind==='dialect'&&path==='/'+ref.id);assert(ref);return {sha256:ref.revision}}})
  stale=true;await assert.rejects(acceptPlan(store,chat,plan,new AbortController().signal),/changed/);assert.equal(currentChat(store,chat.id).interaction,'plan');assert.equal(writes,0)
  stale=false;await acceptPlan(store,chat,plan,new AbortController().signal)
  assert.equal(currentChat(store,chat.id).interaction,'work');assert.equal(currentChat(store,chat.id).plans.at(-1).status,'accepted');assert.equal(starts,0)
  const restored=new ChatStore('fixture',io,draftIO)
  try{await restored.load();assert.equal(restored.getSnapshot().error,'');assert.equal(currentChat(restored,chat.id).plans.at(-1).status,'accepted')}finally{restored.dispose()}
  sources++
 }finally{store.dispose()}
}
console.log(`Plan shared readers: ${sources} actual API/native projections, canonical validPlans/PlanCard/currentChat/checkPlan/acceptPlan; review questions and human Start work boundary passed.`)
