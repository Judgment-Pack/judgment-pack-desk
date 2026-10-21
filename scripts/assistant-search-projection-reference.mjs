// Read actual emitted Go bytes through the existing browser search/chat owners.
import assert from 'node:assert/strict'
import {readFile} from 'node:fs/promises'
import {join} from 'node:path'
import {ChatStore} from '../web/src/chat/store.ts'
import {validSearchStep,searchReferenceKey} from '../web/src/search/step.ts'
import {verifySearch,validateSearch} from '../web/src/search/results.ts'
import urlVectors from '../internal/desk/testdata/assistant-search-url.json'
import {validWebURL} from '../web/src/documents/record.ts'
import {researchChoice} from '../web/src/chat/researchChoice.ts'
const dir=process.argv[2]
assert(dir,'fresh Go producer directory required')
for(const vector of urlVectors){
 assert.equal(validWebURL(vector.url),vector.browser,vector.name)
 if(vector.host)assert.equal(vector.browser,true,vector.name+' must remain browser-readable')
}
let observations=0,writes=0
for(const engine of ['api','codex','research-child']){
 const [document,object,pin]=await Promise.all([`${engine}.json`,`${engine}-object.json`,`${engine}-pin.json`].map(async name=>JSON.parse(await readFile(join(dir,name),'utf8'))))
 const persistence={read:async()=>({project:'fixture',sha256:'fixture',content:document}),write:async()=>{writes++;throw Error('reader cannot save')}}
 const drafts={read:async()=>({project:'fixture',sha256:'fixture',content:{version:1,drafts:[],deleted:[]}}),write:async()=>{writes++;throw Error('reader cannot save')}}
 const store=new ChatStore('fixture',persistence,drafts)
 try{
  await store.load();assert.equal(store.getSnapshot().error,'')
  const chat=store.getSnapshot().chats.find(c=>c.id==='chat-one');assert(chat)
  assert.equal(chat.searches.length,1)
  const ref=chat.searches[0],response=chat.checkpoint.state.responses.at(-1)
  assert.equal(response.searches.length,1);assert.equal(searchReferenceKey(response.searches[0]),searchReferenceKey(ref))
  const row=response.work.items.find(row=>row.name==='search_sources');assert(row)
  assert(validSearchStep(row.search));assert.equal(row.search.submitted,true);assert.equal(row.status,'complete')
  assert.equal(searchReferenceKey(row.search.reference),searchReferenceKey(ref))
  const verified=await verifySearch(object,ref,pin)
  assert.equal(verified.result.query,'public policy');assert.equal(verified.result.provider,'tavily')
  assert.equal(verified.result.hits[0].url,'https://example.org:443/policy')
  assert.equal(verified.result.signedExtension.retained,true)
  // Saved historical Research choice cannot authorize a later unchosen turn.
  assert.equal(researchChoice(undefined,chat.mode,true),'ask')
  const legacy={...verified.result,retrievedAt:'2026-10-21'}
  assert.doesNotThrow(()=>validateSearch(legacy,ref.request))
  observations++
 }finally{store.dispose()}
}
assert.equal(writes,0)
console.log(`Search shared readers: ${observations} actual API/native/research-child projections and exact sealed originals passed.`)
