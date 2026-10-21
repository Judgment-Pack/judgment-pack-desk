// Hand-authored expectations over the actual Go producer and terminal writer.
import assert from 'node:assert/strict'
import {readFile} from 'node:fs/promises'
import {ChatStore} from '../web/src/chat/store.ts'
import {decodeCheckpoint} from '../web/src/chat/checkpoint.ts'
import {validPolicyProposal} from '../web/src/policies/chat.ts'
import {validPolicyInterpretationProposal} from '../web/src/policies/interpretation.ts'
const [passedPath,stalePath]=process.argv.slice(2)
assert(passedPath&&stalePath,'passed and stale Policy producer outputs required')
for(const [path,status,interpretations] of [[passedPath,'passed',2],[stalePath,'stale',0]]){
 const document=JSON.parse(await readFile(path,'utf8'))
 let writes=0
 const persistence={read:async()=>({project:'fixture',sha256:'fixture',content:document}),write:async()=>{writes++;throw Error('Reader must not save')}}
 const drafts={read:async()=>({project:'fixture',sha256:'fixture',content:{version:1,drafts:[],deleted:[]}}),write:async()=>{writes++;throw Error('Reader must not save')}}
 const store=new ChatStore('fixture',persistence,drafts)
 await store.load()
 assert.equal(store.getSnapshot().error,'')
 const chat=store.getSnapshot().chats.find(chat=>chat.id==='chat-one')
 assert(chat)
 assert(decodeCheckpoint(chat.checkpoint))
 assert.equal(chat.policyProposals.length,1)
 const proposal=chat.policyProposals[0]
 assert(validPolicyProposal(proposal))
 assert.equal(proposal.policyId,chat.policy.id)
 assert.equal(proposal.revision,chat.policyRequest.revision)
 assert.deepEqual(proposal.before,chat.policyRequest.candidate)
 assert.equal(proposal.candidate.narrative,'Preserve missing, false, zero, 1.20 and "50" exactly.\r\n中文 <record>.')
 assert.equal(proposal.check.scope,'draft-shape-and-revision')
 assert.equal(proposal.check.status,status)
 assert.equal(proposal.save,undefined)
 assert.equal((chat.policyInterpretations??[]).length,interpretations)
 for(const handoff of chat.policyInterpretations??[]){assert(validPolicyInterpretationProposal(handoff));assert.equal(handoff.policyId,chat.policy.id);assert.equal(handoff.openedChatId,undefined)}
 if(interpretations){assert.deepEqual(chat.policyInterpretations.map(p=>p.artifactKind),['pack','graph']);assert(chat.policyInterpretations[0].publicationId);assert.equal(chat.policyInterpretations[1].publicationId,undefined)}
 assert.equal(store.getSnapshot().chats.length,1)
 assert.equal(writes,0)
 store.dispose()
}
console.log('Policy shared readers: exact passed/stale update and pack/graph handoff cards passed.')
