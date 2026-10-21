// Actual Go producer observations enter the existing chat/portable-action readers.
import assert from 'node:assert/strict'
import {readFile} from 'node:fs/promises'
import {ChatStore} from '../web/src/chat/store.ts'
import {validDialectChatAction,applyDialectRecipeAction} from '../web/src/dialects/chatActions.ts'
const [chatPath,sourcePath,recordsPath,activityPath]=process.argv.slice(2)
assert(chatPath&&sourcePath&&recordsPath&&activityPath,'fresh actual producer observations required')
const [document,source,records,activity]=await Promise.all([chatPath,sourcePath,recordsPath,activityPath].map(async path=>JSON.parse(await readFile(path,'utf8'))))
let writes=0
const persistence={read:async()=>({project:'fixture',sha256:'fixture',content:document}),write:async()=>{writes++;throw Error('Reader must not save')}}
const drafts={read:async()=>({project:'fixture',sha256:'fixture',content:{version:1,drafts:[],deleted:[]}}),write:async()=>{writes++;throw Error('Reader must not save')}}
const store=new ChatStore('fixture',persistence,drafts)
try{
 await store.load();assert.equal(store.getSnapshot().error,'')
 const chat=store.getSnapshot().chats.find(c=>c.id==='chat-one');assert(chat)
 const actions=chat.inputActions
 assert.equal(actions.length,6)
 assert.deepEqual(actions.map(a=>a.kind),['update-recipe','remove-recipe','research','reprocess','stop-research','evaluate'])
 for(const action of actions){assert(validDialectChatAction(action));assert.equal(action.revision,source.sha256);assert.equal(action.recipeVersion,2);assert.equal(action.afterTurnId,chat.checkpoint.state.turns.find(t=>t.role==='user').id);assert.equal(action.applied,undefined)}
 const updated=applyDialectRecipeAction(source,actions[0])
 assert.equal(updated.recipe.intent,'Review exact <record>& 中文')
 assert.deepEqual(updated.recipe.research.fields,[{name:'flag',description:'Boolean, not supplied text',type:'boolean'}])
 assert.deepEqual(updated.recipe.research.criteria,[])
 assert.deepEqual(updated.recipe.research.execution,{approach:'standard',concurrency:0})
 assert.deepEqual(updated.recipe.research.ai,source.document.recipe.research.ai)
 assert.deepEqual(updated.recipe.qualification,source.document.recipe.qualification)
 assert.equal(applyDialectRecipeAction(source,actions[1]).recipe.qualification,undefined)
 assert.deepEqual(actions[3].recordIds,[source.document.records[1].id]);assert.deepEqual(actions[5].recordIds,[source.document.records[1].id])
 assert.equal(actions[4].runId,'research_11111111111111111111111111111111')
 assert.throws(()=>applyDialectRecipeAction({...source,sha256:'a'.repeat(64)},actions[0]))
 assert.equal(records.total,2);assert.equal(records.nextOffset,null);assert.equal(records.items[0].class,'asserted');assert.equal(records.items[1].class,'generated')
 const generated=records.items[1]
 assert.deepEqual(generated.values.value,{text:'00123',truncated:false})
 assert.equal(generated.research.fields.flag.value,false);assert.equal(generated.research.fields.zero.value,0);assert.equal(generated.research.fields.empty.value,'')
 assert.equal(generated.research.fields.flag.sources[0].quote.length,200);assert.equal(generated.research.fields.flag.sources[0].truncated,true)
 assert.equal(generated.review.current,true);assert.equal(generated.research.recipeVersion,1)
 assert.equal(activity.research.length,1);assert.equal(activity.research[0].state,'running');assert.equal(activity.research[0].stopRequested,false)
 assert.equal(activity.evaluations.length,1);assert.equal(activity.evaluations[0].state,'complete');assert.equal(activity.evaluations[0].recordCount,1)
 assert.equal(writes,0)
}finally{store.dispose()}
console.log('Dialect records/actions shared readers: bounded original values, real activity and 6 exact review-only actions passed.')
