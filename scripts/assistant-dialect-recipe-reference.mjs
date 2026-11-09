// Hand-authored expectations over actual producer, retained draft and chat bytes.
import assert from 'node:assert/strict'
import {readFile} from 'node:fs/promises'
import {ChatStore} from '../web/src/chat/store.ts'
import {validDialectRecipeProposal,recipeFromProposal} from '../web/src/dialects/proposals.ts'
import {dialectDraftQuery,dialectDraftHref} from '../web/src/dialects/drafts.ts'
import {giveThisPageASessionForTesting} from '../web/src/mcp/session.ts'
const [chatPath,draftPath]=process.argv.slice(2)
assert(chatPath&&draftPath,'actual saved chat and independent draft observations required')
const document=JSON.parse(await readFile(chatPath,'utf8'))
const retained=JSON.parse(await readFile(draftPath,'utf8'))
let writes=0
const persistence={read:async()=>({project:'fixture',sha256:'fixture',content:document}),write:async()=>{writes++;throw Error('Reader must not save')}}
const drafts={read:async()=>({project:'fixture',sha256:'fixture',content:{version:1,drafts:[],deleted:[]}}),write:async()=>{writes++;throw Error('Reader must not save')}}
const store=new ChatStore('fixture',persistence,drafts)
await store.load()
assert.equal(store.getSnapshot().error,'')
const chat=store.getSnapshot().chats.find(chat=>chat.id==='chat-one')
assert(chat)
assert.equal(chat.inputDrafts.length,1)
const proposal=chat.inputDrafts[0]
assert(validDialectRecipeProposal(proposal))
assert.equal(validDialectRecipeProposal({...proposal,name:"\ufeff"}),false)
assert.equal(validDialectRecipeProposal({...proposal,name:"\u0085"}),true)
assert.equal(validDialectRecipeProposal({...proposal,fields:[{name:"title",type:"text",description:"\ufeff"}]}),false)
assert.equal(proposal.name,'Portable recipe')
assert.equal(proposal.intent,'Preserve exact fields.\r\n中文 <record>&')
assert.equal(proposal.target,'records')
assert.deepEqual(proposal.fields.map(f=>f.type),['text','integer','decimal','boolean','text-list','range','string','number'])
assert.deepEqual(proposal.limits,{records:5,searches:3,pages:4,modelCalls:6})
assert.deepEqual(proposal.execution,{approach:'delegated',concurrency:2})
assert.equal(proposal.criteria[0].qualificationTarget,'Review separately')
assert.equal(proposal.afterTurnId,chat.checkpoint.state.turns.find(t=>t.role==='user').id)
assert(proposal.attribution.connection&&proposal.attribution.model)
assert.equal(proposal.savedInputId,undefined)
assert.equal(retained.document.origin.chatId,chat.id)
assert.equal(retained.document.origin.draftId,proposal.draftId)
assert.equal(retained.document.createdAt,proposal.createdAt)
const expected=recipeFromProposal(proposal)
// The Go JSON contract omits an empty optional companyIds list.
assert.deepEqual({...retained.document.recipe.research,companyIds:[]},expected)
assert.equal(retained.document.recipe.intent,proposal.intent)
assert.equal(retained.document.savedInputId,undefined)
assert.equal(retained.document.discarded,undefined)
let reads=0
giveThisPageASessionForTesting('recipe-reader-session')
const originalFetch=globalThis.fetch
globalThis.fetch=async(path,init)=>{
 assert.equal(path,'/api/dialect-drafts/'+retained.document.id)
 assert.equal(init.method,'GET');assert.equal(init.body,undefined)
 assert.equal(init.headers.Authorization,'Bearer recipe-reader-session')
 reads++
 return new Response(JSON.stringify(retained),{status:200,headers:{'Content-Type':'application/json'}})
}
try{
 const query=dialectDraftQuery(retained.document.id)
 const reopened=await query.queryFn({signal:new AbortController().signal})
 assert.deepEqual(reopened,retained)
 assert.equal(dialectDraftHref(reopened.document),'/dialects/drafts/'+retained.document.id+'?chat='+chat.id)
}finally{globalThis.fetch=originalFetch;store.dispose()}
assert.equal(reads,1);assert.equal(writes,0)
console.log('Dialect recipe shared readers: exact typed proposal and independent draft readback passed.')
