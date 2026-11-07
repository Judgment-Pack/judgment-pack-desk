// Consume only fresh observations from the actual Go producer through the
// existing browser store/action reader. This reader never requests research.
import assert from 'node:assert/strict'
import {readFile} from 'node:fs/promises'
import {ChatStore} from '../web/src/chat/store.ts'
import {validDialectChatAction} from '../web/src/dialects/chatActions.ts'
const paths=process.argv.slice(2)
assert.equal(paths.length,5,'review source/started source/review/started/activity observations required')
const [reviewSource,startedSource,review,started,activity]=await Promise.all(paths.map(async p=>JSON.parse(await readFile(p,'utf8'))))
let writes=0
for(const [document,admitted,source] of [[review,false,reviewSource],[started,true,startedSource]]){
 const persistence={read:async()=>({project:'fixture',sha256:'fixture',content:document}),write:async()=>{writes++;throw Error('reader cannot save')}}
 const drafts={read:async()=>({project:'fixture',sha256:'fixture',content:{version:1,drafts:[],deleted:[]}}),write:async()=>{writes++;throw Error('reader cannot save')}}
 const store=new ChatStore('fixture',persistence,drafts)
 try{
  await store.load();assert.equal(store.getSnapshot().error,'')
  const chat=store.getSnapshot().chats.find(c=>c.id==='chat-one');assert(chat)
  assert.equal(chat.inputActions.length,1)
  const action=chat.inputActions[0];assert(validDialectChatAction(action));assert.equal(action.kind,'research')
  assert.equal(action.inputId,source.document.id);assert.equal(action.revision,source.sha256);assert.equal(action.recipeVersion,source.document.recipeVersion)
  assert.equal(action.afterTurnId,chat.checkpoint.state.turns.find(t=>t.role==='user').id)
  if(admitted){assert.equal(action.applied.runId,'research_11111111111111111111111111111111');assert(Number.isFinite(Date.parse(action.applied.at)))}else{assert.equal(action.applied,undefined)}
  assert.equal(action.patch,undefined);assert.equal(action.dialect,undefined);assert.equal(action.publication,undefined)
 }finally{store.dispose()}
}
assert.equal(activity.research.length,1);assert.equal(activity.research[0].id,'research_11111111111111111111111111111111');assert.equal(activity.research[0].state,'queued');assert.equal(activity.research[0].stopRequested,false)
assert.equal(writes,0)
console.log('Dialect research shared readers: exact review/admitted cards and independent queued activity passed (5 observations).')
