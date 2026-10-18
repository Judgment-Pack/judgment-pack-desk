import records from '../../../internal/desk/testdata/graph-draft-records.json'
import source from '../../../internal/desk/testdata/policy-authoring-source.json'
import {afterEach,expect,it,vi} from 'vitest'
import {deskFetch} from '../files/client'
import {decodeGraphDrafts,retainGraphDraft,deleteGraphDrafts,readGraphDrafts,retainedGraphDrafts,type GraphDraft,type GraphDraftDocument} from './drafts'
import {savedGraphPolicySources} from './SavedGraphSources'
vi.mock('../files/client',async original=>({...await original(),deskFetch:vi.fn()}))
const draft:GraphDraft={draftId:'draft-one',originChatId:'chat-one',id:'flow',path:'flow.graph.json',content:'{}',createdAt:'2026-10-18T12:00:00Z',policySources:[source as import('../policies/authoringSource').PolicyAuthoringSource]}
afterEach(()=>vi.clearAllMocks())
function server(initial:GraphDraftDocument={version:1,drafts:[],deleted:[]}){
 let stored=structuredClone(initial),revision=0
 vi.mocked(deskFetch).mockImplementation(async(_url,init)=>{
  if(init?.method==='PUT'){if(new Headers(init.headers).get('If-Match')!==String(revision))return Response.json({error:'stale'},{status:409});stored=structuredClone(JSON.parse(init.body as string));revision++}
  return Response.json({project:'fixture',sha256:String(revision),content:structuredClone(stored)})
 })
 return {get:()=>structuredClone(stored),edit:(doc:GraphDraftDocument)=>{stored=structuredClone(doc);revision++}}
}
it.each(records)('shares private record validation with Go: $name',test=>{
 if(test.valid)expect(decodeGraphDrafts(test.document)).toEqual(test.document)
 else expect(()=>decodeGraphDrafts(test.document)).toThrow()
})
it('retains independent graph bytes and exact Policy basis after deleting their chat, then finalizes privately',async()=>{
 server();const saved=await retainGraphDraft(draft)
 expect(retainedGraphDrafts(saved.content,[])).toEqual([draft])
 const reloaded=await readGraphDrafts(),held=reloaded.content.drafts[0]!
 const edited={...held,content:'{"nodes":{"step":{"pack":"saved-pack"}},"edges":[]}'}
 const next=await retainGraphDraft(edited,held)
 const finalized=await retainGraphDraft({...edited,saved:true},next.content.drafts[0])
 expect(savedGraphPolicySources(finalized.content,'flow','flow.graph.json',edited.content)).toEqual([source])
 expect(vi.mocked(deskFetch).mock.calls.every(([path])=>String(path)==='/api/draft-graphs')).toBe(true)
 const bodies=vi.mocked(deskFetch).mock.calls.filter(([,init])=>init?.method==='PUT').map(([,init])=>JSON.parse(init!.body as string))
 expect(bodies.at(-1).drafts[0]).toMatchObject({saved:true,policySources:[source]})
})
it('refuses a stale frozen graph and never replays uncertain writes',async()=>{
 const fixture=server({version:1,drafts:[draft],deleted:[]})
 fixture.edit({version:1,drafts:[{...draft,content:'{"independent":true}'}],deleted:[]})
 await expect(retainGraphDraft({...draft,content:'{"mine":true}'},draft)).rejects.toThrow('changed')
 expect(vi.mocked(deskFetch).mock.calls.some(([,init])=>init?.method==='PUT')).toBe(false)
 vi.mocked(deskFetch).mockRejectedValueOnce(Error('interrupted'))
 await expect(retainGraphDraft(draft)).rejects.toThrow('interrupted')
})
it('scopes duplicate draft ids to their original conversation and tombstones only the selected origin',async()=>{
 const other={...draft,originChatId:'chat-two'};server({version:1,drafts:[draft,other],deleted:[]})
 const deleted=await deleteGraphDrafts([{originChatId:'chat-one',draftId:draft.draftId}])
 expect(deleted.content.drafts).toEqual([other])
 expect(retainedGraphDrafts(deleted.content,[{id:'chat-one',graphDrafts:[draft]}])).toEqual([other])
 await expect(retainGraphDraft(draft)).rejects.toThrow('deleted')
})
it('treats old chat graph snapshots as related history after independent edits',()=>{
 const current={...draft,content:'{"latest":true}'}
 expect(retainedGraphDrafts({version:1,drafts:[current],deleted:[]},[{id:'chat-one',graphDrafts:[draft]}])).toEqual([current])
 expect(JSON.stringify(current.content)).not.toContain(source.narrative)
})

it('resolves Policy basis only for exact saved graph bytes, retaining older versions as history',()=>{
 const old={...draft,saved:true},current={...draft,draftId:'revision-two',content:'{ "nodes": {} }',saved:true,policySources:[]}
 const document={version:1 as const,drafts:[old,current],deleted:[]}
 expect(savedGraphPolicySources(document,'flow',draft.path,old.content)).toEqual([source])
 expect(savedGraphPolicySources(document,'flow',draft.path,current.content)).toEqual([])
 expect(savedGraphPolicySources(document,'flow',draft.path,'{ "external": true }')).toEqual([])
})
it('ignores nested private graph source fields when projecting old conversation history',()=>{
 const nested=retainedGraphDrafts(undefined,[{id:'actual-owner',graphDrafts:[draft]}])
 expect(nested[0]?.originChatId).toBe('actual-owner');expect(nested[0]?.policySources).toBeUndefined()
})
