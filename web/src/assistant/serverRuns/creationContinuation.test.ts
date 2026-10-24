import {afterEach,expect,it,vi} from 'vitest'
import {readFileSync} from 'node:fs'
import {sha256} from '@judgment-pack/harness/records'
import {decodeCheckpoint} from '../../chat/checkpoint'
import {artifactCheckpoint} from '../../packs/drafts/model'
import {caseStatements,statementTurns} from '../../research/run'
import {ChatStore} from '../../chat/store'
import {conversationWire,encodeWireValue,wireField,wireJSON} from '../../chat/wire'
import {memberOf,parseJsonText} from '../../research/verify/canon'
import {canCreateDraft,type RunState} from '../../research/run'
import {decodeSnapshot,parseRunJSON} from './decode'
import {readCreation,withCreation} from './creation'

const outputs=process.env.JPS_CREATION_OUTPUTS
const stores:ChatStore[]=[]
afterEach(()=>{stores.forEach(store=>store.dispose());stores.length=0;vi.unstubAllGlobals()})
const field=(raw:string,key:string)=>wireField(raw,key)!
const chatRow=(document:string)=>conversationWire('{"content":'+document+'}')['chat-one']!
const stateWire=(row:string)=>field(field(row,'checkpoint'),'state')
const candidateDigests=(state:RunState):RunState=>({...state,candidates:state.candidates.map(c=>({...c,digest:sha256(c.text)}))})

const handwrittenFacts='{"large":9007199254740993,"decimal":1.20,"negativeZero":-0,"flag":false,"nil":null,"numeric":"1","nested":{"amount":2.500}}'
const acceptedFacts=(issues:string)=>{
 const tree=parseJsonText(issues)
 if(tree.kind!=='array')throw Error('issue fixture must be an array')
 return new Map(tree.items.map(issue=>{const original=memberOf(issue,'original')!,id=memberOf(original,'id')!;if(id.kind!=='string')throw Error('missing fixture ID');return [id.value,wireJSON(memberOf(original,'facts')!,true)]}))
}
it.skipIf(!outputs).each([{order:'first',inject:false},{order:'last',inject:false},{order:'first',inject:true},{order:'last',inject:true}])('continues accepted two-issue data with default writers and reload: $order first, handwritten wire $inject',async({order,inject})=>{
 const records=['02-establish-cases','03-propose-correction','04-approve-correction','05-propose-correction','06-approve-correction'].map(name=>{
  let text=readFileSync(`${outputs}/${order}/${name}.json`,'utf8')
  if(inject){
   // Separate handwritten accepted-wire fixture. Actual API transport snapshots
   // provide the lifecycle scaffold; this does not claim pre-admission fidelity.
   const first=JSON.parse(readFileSync(`${outputs}/${order}/02-establish-cases.json`,'utf8'))
   for(const original of new Set(acceptedFacts(first.run.creation.issues).values())){
    text=text.split(original).join(handwrittenFacts)
    text=text.split(JSON.stringify(original).slice(1,-1)).join(JSON.stringify(handwrittenFacts).slice(1,-1))
   }
  }
  const parsed=JSON.parse(text)
  return {parsed,before:field(text,'before'),drafts:field(text,'drafts'),source:field(text,'source'),chat:field(text,'chat')}
 })
 let conversation=records[0]!.before,drafts=records[0]!.drafts,revision='a'.repeat(64)
 // The API fixture's retained artifact helper keeps conversation turns. Seed
 // the browser with its existing production artifact projection instead.
 const draftDoc=JSON.parse(drafts),draftWire=conversationWire('{"content":'+drafts+'}','drafts')
 for(const draft of draftDoc.drafts){
  const cp=decodeCheckpoint(draft.checkpoint,field(draftWire[draft.id]!,'checkpoint')),statements=caseStatements(cp.state)
  for(const {id,turn} of statementTurns(cp.state.turns))if(turn.creation)statements.delete(cp.state.inheritedStatements===undefined?id:`message:${turn.id}`)
  draft.checkpoint=artifactCheckpoint({...cp,state:{...cp.state,inheritedStatements:[...statements]}})
 }
 drafts=encodeWireValue(draftDoc)
 const digest=()=>sha256(conversation),draftDigest=()=>sha256(drafts)
 vi.stubGlobal('fetch',vi.fn(async(path:unknown,init?:RequestInit)=>{
  const artifact=String(path)==='/api/draft-packs'
  expect(['/api/draft-packs','/api/conversations']).toContain(String(path))
  if(init?.method==='PUT'){
   expect((init.headers as Record<string,string>)['If-Match']).toBe(artifact?draftDigest():digest())
   if(artifact)drafts=String(init.body)
   else {conversation=String(init.body);revision=sha256(chatRow(conversation))}
  }
  return new Response('{"project":"/project","sha256":'+JSON.stringify(artifact?draftDigest():digest())+',"content":'+(artifact?drafts:conversation)+(artifact?'':',"chatRevisions":{"chat-one":"'+revision+'"}')+'}',{headers:{'Content-Type':'application/json'}})
 }))
 const load=async()=>{const store=new ChatStore('/project');stores.push(store);await store.load();expect(store.getSnapshot().error).toBe('');return store}
 let store=await load(),state=candidateDigests(store.getSnapshot().chats[0]!.checkpoint!.state)
 for(const [index,record] of records.entries()){
  const source=JSON.parse(record.source).chats[0],request=source.checkpoint.state.turns.at(-1)
  expect(request.role).toBe('user')
  await store.prepareServerRequest('chat-one',{...state,turns:[...state.turns,request]},request.id,request.id,new AbortController().signal)
  // These are the exact fields accepted by Go's current artifact/source guard.
  for(const key of ['cases','expectationIssues','heldProposal','unknowns'])expect(field(stateWire(chatRow(conversation)),key),`${index} next ${key}`).toBe(field(stateWire(chatRow(record.source)),key))
  expect(JSON.parse(conversation).chats[0].draftGeneration).toBe(source.draftGeneration)
  const beforeGeneration=JSON.parse(drafts).drafts[0].generation
  const run=decodeSnapshot((parseRunJSON(JSON.stringify(record.parsed.run)) as Record<string,unknown>),{owner:record.parsed.run.owner,requestId:record.parsed.run.requestId})
  expect(run.chatProjection?.acknowledged).toBe(true)
  conversation=record.chat;revision=run.chatProjection!.digest!
  const saved=await store.reconcileServer('chat-one',true,{digest:revision,responseId:run.chatProjection!.responseId,requestId:run.requestId,admissionId:run.admissionId})
  expect(saved).toBeDefined();expect(store.getSnapshot().error).toBe('')
  const evidence=readCreation(run.creation),live=()=>withCreation(candidateDigests(store.getSnapshot().chats[0]!.checkpoint!.state),evidence,true)
  state=live()
  expect(state.expectationIssues.map(i=>i.id)).toEqual(saved!.checkpoint!.state.expectationIssues.map(i=>i.id))
  if(evidence.action==='propose-correction'){
   expect(JSON.parse(drafts).drafts[0].generation).toBe(beforeGeneration)
   expect(state.expectationIssues.find(i=>i.id===evidence.proposal!.issueId&&!i.resolved)?.proposal?.token).toBe(evidence.proposal!.token)
  }
  const pending=state.expectationIssues.filter(i=>!i.resolved).length
  if(pending){expect(state.status).toBe('needs-input');expect(state.readiness).toBe('');expect(canCreateDraft(state,'draft',evidence.sourceDigest)).toBe(false)}
  if(index===2){expect(pending).toBe(1);expect(state.candidates.at(-1)?.check?.cases).toHaveLength(2)}
  if(index===2||index===3){
   store.dispose();store=await load()
   const historical=withCreation(candidateDigests(store.getSnapshot().chats[0]!.checkpoint!.state),evidence,false,false)
   expect(historical.readiness).toBe('');expect(historical.expectationIssues.every(i=>!i.proposal)).toBe(true)
   expect(canCreateDraft(historical,'draft',evidence.sourceDigest)).toBe(false)
   state=live()
  }
  const artifact=JSON.parse(drafts).drafts[0],artifactWire=conversationWire('{"content":'+drafts+'}','drafts')[artifact.id]!
  for(const key of ['cases','expectationIssues','heldProposal','unknowns'])expect(field(stateWire(artifactWire),key)).toBe(field(stateWire(chatRow(conversation)),key))
 }
 expect(state.cases.map(c=>c.id)).toEqual(['first','typed','last'])
 expect(state.expectationIssues.every(i=>i.resolved)).toBe(true)
 const accepted=acceptedFacts(records[0]!.parsed.run.creation.issues)
 for(const row of state.cases.filter(c=>c.id!=='typed'))expect(encodeWireValue(row.facts)).toBe(accepted.get(row.id))
 if(inject)for(const token of ['9007199254740993','1.20','"negativeZero":-0','2.500'])expect(encodeWireValue(state.cases)).toContain(token)
 expect(state.status).toBe('ready');expect(state.readiness).not.toBe('')
 expect(canCreateDraft(state,'draft',state.tracedBasis)).toBe(true)
})
