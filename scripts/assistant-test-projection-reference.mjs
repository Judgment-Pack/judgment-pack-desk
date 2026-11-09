// Actual suite/chat/proposal consumers over fresh scripted Go producer output.
// This proves Desk's consumer contract; Runtime v0.37.0 support is blocked by #582.
import assert from 'node:assert/strict'
import {readFile} from 'node:fs/promises'
import React from 'react'
import {renderToStaticMarkup} from 'react-dom/server'
import {MemoryRouter} from 'react-router-dom'
import {ChatStore} from '../web/src/chat/store.ts'
import {decodeStore} from '../web/src/packs/test-workspace/model.ts'
import {proposalCases,saveReviewedCases} from '../web/src/packs/test-workspace/proposals.ts'
import {testReviewHref} from '../web/src/packs/test-workspace/chatTools.ts'
import {jsonIdentity} from '../web/src/research/checkCandidate.ts'
import {RelatedWorkLink} from '../web/src/chat/RelatedWork.tsx'
const paths=process.argv.slice(2)
assert.equal(paths.length,5,'actual saved/draft chat, suite and draft-store outputs required')
async function probe(chatPath,suitePath,owner,draftPath){
const [chatBytes,suiteBytes]=await Promise.all([readFile(chatPath,'utf8'),readFile(suitePath,'utf8')])
const original=JSON.parse(suiteBytes),suites=decodeStore(original)
const suite=suites.suites[owner]
assert.equal(suite.cases.length,1,'proposal must not save cases')
assert.equal(suite.proposals.length,1)
const proposal=suite.proposals[0]
assert.equal(proposal.chatId,'chat-one')
assert.equal(proposal.state,'ready')
assert.equal(proposal.contractVersion,'1')
assert.deepEqual(proposal.caseSnapshots,{existing:jsonIdentity(suite.cases[0])},'consumer identity must match integer-key/Unicode/number ordering')
assert(suiteBytes.includes('"n":1.20'),'original numeric spelling retained in storage')
assert(suiteBytes.includes('"Amount"')&&suiteBytes.includes('"amount"'))
const cases=proposalCases(proposal)
assert.equal(cases.length,3)
const [replacement,wrong,missing]=cases
assert.equal(replacement.id,'existing')
assert.equal(replacement.row.facts.amount,'50')
assert.equal(replacement.row.facts.Amount,'51')
assert.equal(replacement.row.facts.flag,false)
assert.equal(replacement.row.facts.zero,0)
assert.equal(replacement.row.facts.null,null)
assert.equal(Object.hasOwn(replacement.row.facts,'missing'),false)
assert.equal(replacement.sourceMappings['/amount'],'requirement')
assert.equal(wrong.row.facts.amount,0)
assert.equal(wrong.intentionalFactTypes[0].caseId,'wrong-type')
assert.equal(wrong.row.expectedErrorClass,'type-error')
assert.deepEqual(missing.row.facts,{})
assert.equal(missing.row.expectedErrorClass,'missing-fact')
const accepted=saveReviewedCases(suite,proposal,cases,proposal.packDigest)
assert.equal(accepted.cases.length,3)
assert.equal(accepted.cases[0].revision,2,'existing-case replacement must pass actual reviewed-save guard')
const stale=structuredClone(suite)
stale.cases[0].row.facts.amount='changed after request'
assert.throws(()=>saveReviewedCases(stale,proposal,cases,proposal.packDigest),/case changed after the request/)
assert.throws(()=>saveReviewedCases(suite,proposal,cases,'foreign-pack'),/pack changed/)
let writes=0
const persistence={read:async()=>({project:'fixture',sha256:'fixture',content:JSON.parse(chatBytes)}),write:async()=>{writes++;throw Error('Reader must not persist')}}
const draftDocument=draftPath?JSON.parse(await readFile(draftPath,'utf8')):{version:1,drafts:[],deleted:[]}
const drafts={read:async()=>({project:'fixture',sha256:'fixture',content:draftDocument}),write:async()=>{writes++;throw Error('Reader must not persist')}}
const store=new ChatStore('fixture',persistence,drafts)
await store.load()
assert.equal(store.getSnapshot().error,'')
const chat=store.getSnapshot().chats.find(chat=>chat.id==='chat-one')
assert(chat)
const link=chat.checkpoint.state.turns.find(turn=>turn.testDesign?.proposalId===proposal.id)
assert.deepEqual(link.testDesign,{owner,proposalId:proposal.id})
const draft=store.getSnapshot().packDrafts.some(d=>d.id===owner)
assert.equal(draft,!!draftPath)
if(draft){const target=store.getSnapshot().packDrafts.find(d=>d.id===owner);assert.equal(target.finalized,undefined);assert(target.checkpoint.state.candidates.at(-1).text.includes('1.20'))}
const href=testReviewHref({owner,draft},chat.id,proposal.id)
const rendered=renderToStaticMarkup(React.createElement(MemoryRouter,null,React.createElement(RelatedWorkLink,{href},'Review cases')))
assert(rendered.includes('Review cases'))
assert(rendered.includes((draft?'/packs/drafts/'+owner:'/packs/'+owner+'/matrix')+'?chat=chat-one&amp;testProposal='+proposal.id))
assert.equal(writes,0)
assert.equal(suite.cases.length,1,'pure reviewed-save probe must not mutate persisted suite')
assert.equal(proposal.savedCases,undefined)
store.dispose()
}
await probe(paths[0],paths[1],'one')
await probe(paths[2],paths[3],'draft-tests',paths[4])
console.log('Test design shared readers: exact saved/draft proposals, existing-case review/stale guards and linked review actions passed (scripted Runtime; #582 remains blocked).')
