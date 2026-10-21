// Actual Go producer observations enter existing shared chat and review readers.
import assert from 'node:assert/strict'
import {readFile} from 'node:fs/promises'
import {ChatStore} from '../web/src/chat/store.ts'
import {validDialectChatAction} from '../web/src/dialects/chatActions.ts'
import {validDialectCheckCheckpoint} from '../web/src/dialect/checkpoint.ts'
import {validDialectProposal,buildDialect} from '../web/src/dialects/dialect.ts'
import {validPublicationProposal,publicationRequest} from '../web/src/dialects/publicationProposal.ts'
import {reviewMatches} from '../web/src/artifacts/review.ts'
const [chatPath,sourcePath,reviewPath]=process.argv.slice(2)
assert(chatPath&&sourcePath&&reviewPath,'fresh actual producer observations required')
const document=JSON.parse(await readFile(chatPath,'utf8'))
const source=JSON.parse(await readFile(sourcePath,'utf8'))
const review=JSON.parse(await readFile(reviewPath,'utf8'))
let writes=0
const persistence={read:async()=>({project:'fixture',sha256:'fixture',content:document}),write:async()=>{writes++;throw Error('Reader must not save')}}
const drafts={read:async()=>({project:'fixture',sha256:'fixture',content:{version:1,drafts:[],deleted:[]}}),write:async()=>{writes++;throw Error('Reader must not save')}}
const store=new ChatStore('fixture',persistence,drafts)
try{
 await store.load();assert.equal(store.getSnapshot().error,'')
 const chat=store.getSnapshot().chats.find(c=>c.id==='chat-one');assert(chat)
 assert.equal(chat.inputActions.length,2)
 const [mapping,publication]=chat.inputActions
 assert(validDialectChatAction(mapping));assert(validDialectChatAction(publication))
 assert.equal(mapping.kind,'update-dialect');assert.equal(publication.kind,'publish-dialect')
 assert.equal(mapping.revision,source.sha256);assert.equal(publication.revision,source.sha256)
 assert(validDialectProposal(mapping.dialect));assert(validDialectCheckCheckpoint(mapping.dialectCheck))
 assert.equal(mapping.dialect.summary,'Retain exact country and evidence <record>& 中文')
 assert.deepEqual(mapping.dialect.facts,[{target:'/company/country',column:'Country',conversion:{kind:'text'}}])
 const built=buildDialect(source,mapping.dialect,source.document.recipe.qualification)
 assert.deepEqual(JSON.parse(mapping.dialectCheck.document),built.mapping)
 assert(['complete','review-required'].includes(mapping.dialectCheck.report.status))
 assert(Array.isArray(mapping.dialectCheck.report.compatibility.notes))
 assert(validPublicationProposal(publication.publication));assert.equal(publication.publication.answer.saved,false)
 assert.equal(publication.publication.impact.rationale,'Review original asserted fields <record>& 中文')
 const request=publicationRequest(publication)
 assert.equal(request.action,'publish');assert.equal(request.reviewed,true)
 assert.equal(request.basePublication,'');assert.deepEqual(request.review,review.reference)
 assert(reviewMatches(review,`urn:judgmentpack:dialect:${source.document.id}:asserted`,source.sha256))
 assert.equal(review.review.basePublication,'');assert.equal(review.review.proposedVersion,'1.0.0')
 assert.equal(review.review.preview.document,publication.publication.answer.document)
 assert.equal(mapping.applied,undefined);assert.equal(publication.applied,undefined)
 assert.equal(mapping.afterTurnId,chat.checkpoint.state.turns.find(t=>t.role==='user').id)
 assert.equal(writes,0)
}finally{store.dispose()}
console.log('Dialect mapping/publication shared readers: 2 exact checked review actions passed.')
