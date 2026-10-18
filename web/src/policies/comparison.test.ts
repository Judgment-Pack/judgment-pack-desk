import {afterEach,expect,it,vi} from 'vitest'
import wire from '../../../internal/desk/testdata/policy-source-observation.json'
import {resolvePolicyComparison,validPolicyComparisonRequest,type PolicyComparisonRequest} from './comparison'
import {canonicalize,parseJsonText} from '../research/verify/canon'
import {sha256Hex} from '../research/verify/receipt'
import type {PolicySourceObservation} from './client'
import {giveThisPageASessionForTesting} from '../mcp/session'
const policyId='policy_'+'a'.repeat(48),oldId='revision_'+'b'.repeat(48),newId='revision_'+'c'.repeat(48),publicationId='publication_'+'d'.repeat(48)
const pair:PolicyComparisonRequest={before:{kind:'publication',publicationId},after:{kind:'revision',revisionId:newId}}
afterEach(()=>vi.unstubAllGlobals())
function saved(revisionId=oldId,narrative='Raw\r\nUnicode e\u0301 🐾',title='Old title',digest='e'.repeat(64)){
 const source={version:1,policyId,revisionId,candidateDigest:digest,content:{sha256:digest,bytes:200},title,narrative,artifactVersion:'1.0.0'}
 const revision={sequence:1,at:'2026-10-18T00:00:00Z',actor:{kind:'admin'},candidate:{revisionId,digest,content:source.content,title,narrative,artifactVersion:source.artifactVersion},workflow:{independentReviewRequired:false},workflowDigest:'f'.repeat(64)}
 return {source,revision}
}
function endpoint(old=saved(),next=saved(newId)){
 const fetch=vi.fn(async(input:RequestInfo|URL,init?:RequestInit)=>{
  expect(init?.method??'GET').toBe('GET');expect(new Headers(init?.headers).get('Authorization')).toMatch(/^Bearer /)
  const url=String(input),root='/api/policies/'+policyId
  if(url===root+'/authoring-source?publicationId='+publicationId)return Response.json({...old.source,publicationId})
  if(url===root+'/authoring-source?revisionId='+oldId)return Response.json(old.source)
  if(url===root+'/authoring-source?revisionId='+newId)return Response.json(next.source)
  if(url===root+'/revisions/'+oldId)return Response.json(old.revision)
  if(url===root+'/revisions/'+newId)return Response.json(next.revision)
  throw Error('Unexpected read '+url)
 });vi.stubGlobal('fetch',fetch);return fetch
}
async function observation(document=structuredClone(wire.document),overrides:Partial<PolicySourceObservation>={}){
 const canonical=canonicalize(parseJsonText(JSON.stringify(document))),documentRef={sha256:await sha256Hex(canonical),bytes:canonical.length}
 const retained={...wire,id:wire.observationId,name:document.document.name,retainedAt:wire.observedAt,semanticDigest:'1'.repeat(64),original:{sha256:wire.content.bytesDigest.slice(7),bytes:wire.content.byteLength},document:documentRef,extraction:{...wire.extraction,content:{...wire.extraction.content,bytesDigest:'sha256:'+documentRef.sha256,byteLength:documentRef.bytes}},...overrides} as PolicySourceObservation
 return {observation:retained,document}
}
it('resolves an old publication without the mutable head and preserves raw CRLF and Unicode',async()=>{
 const old=saved(),next=saved(newId,'Raw\nUnicode é 🐾','New title','0'.repeat(64)),fetch=endpoint(old,next),result=await resolvePolicyComparison(policyId,pair)
 expect(fetch).toHaveBeenCalledTimes(4);expect(fetch.mock.calls.every(([url])=>String(url)!=='/api/policies/'+policyId)).toBe(true)
 expect(result.before.identity).toMatchObject({revisionId:oldId,publicationId,candidateDigest:old.source.candidateDigest});expect(result.before.text).toBe(old.source.narrative)
 expect(result.after.text).toBe(next.source.narrative);expect(result.differences.map(d=>d.field)).toContain('text');expect(JSON.stringify(result.differences)).not.toContain(old.source.narrative)
})
it('separates metadata and workflow changes from equal narrative and performs fresh reads on repetition',async()=>{
 const old=saved(),next=saved(newId,old.source.narrative,'Changed title','0'.repeat(64));next.revision.workflow.independentReviewRequired=true;next.revision.workflowDigest='2'.repeat(64)
 const fetch=endpoint(old,next),result=await resolvePolicyComparison(policyId,pair)
 expect(result.differences.map(d=>d.field)).toEqual(expect.arrayContaining(['title','workflow','workflowDigest','candidateDigest','content']));expect(result.differences.map(d=>d.field)).not.toContain('text')
 await resolvePolicyComparison(policyId,pair);expect(fetch).toHaveBeenCalledTimes(8)
})
it('rejects malformed selectors and other Policy/path scopes before reads',async()=>{
 const fetch=endpoint()
 for(const request of [null,{}, {...pair,policyId}, {...pair,before:{kind:'revision',revisionId:'latest'}},{...pair,before:{kind:'Revision',revisionId:oldId}},{...pair,before:{kind:'revision',revisionId:oldId,publicationId}},{...pair,before:{kind:'publication',publicationId,extra:null}},{...pair,after:{kind:'observation',observationId:'/etc/passwd'}}]){
  expect(validPolicyComparisonRequest(request)).toBe(false);await expect(resolvePolicyComparison(policyId,request as PolicyComparisonRequest)).rejects.toThrow()
 }
 for(const id of ['policy-other','../'+policyId,'https://example.com/'+policyId])await expect(resolvePolicyComparison(id,pair)).rejects.toThrow()
 expect(fetch).not.toHaveBeenCalled()
})
it('rejects substituted publication/revision identities and inconsistent digest/length',async()=>{
 for(const changed of ['publication','revision','length','digest'] as const){
  const old=saved(),fetch=endpoint(old)
  if(changed==='revision')old.revision.candidate.revisionId=newId
  if(changed==='length')old.revision.candidate.content={...old.source.content,bytes:199}
  if(changed==='digest')old.revision.candidate.digest='3'.repeat(64)
  if(changed==='publication')fetch.mockImplementation(async()=>Response.json({...old.source,publicationId:'publication_'+'0'.repeat(48)}))
  await expect(resolvePolicyComparison(policyId,pair)).rejects.toThrow()
 }
})
it('refuses the whole pair on second-side revocation and preserves cancellation',async()=>{
 const fetch=endpoint(),normal=fetch.getMockImplementation()!
 fetch.mockImplementation(async(input,init)=>String(input).includes('revisionId='+newId)?Response.json({code:'forbidden'},{status:403}):normal(input,init))
 await expect(resolvePolicyComparison(policyId,pair)).rejects.toMatchObject({code:'forbidden'})
 const abort=new AbortController();fetch.mockImplementation(async(input,init)=>{const response=await normal(input,init);if(String(input).endsWith('/revisions/'+oldId))abort.abort();return response})
 await expect(resolvePolicyComparison(policyId,pair,abort.signal)).rejects.toMatchObject({name:'AbortError'})
 expect(fetch.mock.calls.filter(([input])=>String(input).includes('revisionId='+newId))).toHaveLength(1)
})
it('distinguishes same original/different extraction with equal visible text and strips base64',async()=>{
 const first=await observation(),doc=structuredClone(wire.document);doc.processing.durationMs=1
 const second=await observation(doc,{id:'2'.repeat(64)})
 second.observation.extraction.configurationDigest='sha256:'+'3'.repeat(64)
 vi.stubGlobal('fetch',vi.fn(async(input)=>Response.json(String(input).endsWith(first.observation.id)?first:second)))
 const result=await resolvePolicyComparison(policyId,{before:{kind:'observation',observationId:first.observation.id},after:{kind:'observation',observationId:second.observation.id}})
 expect(result.before.text).toBe(wire.document.content.pages[0]!.text);expect(result.differences.map(d=>d.field)).toContain('extraction');expect(result.differences.map(d=>d.field)).not.toContain('original')
 expect(result.limitations).toEqual(expect.arrayContaining(['snapshot-only','equal-extracted-text']));expect(JSON.stringify(result)).not.toContain(wire.document.original.bytes)
})
it('keeps partial and failed extraction distinct from empty/deleted text',async()=>{
 for(const failed of [false,true]){
  const doc=structuredClone(wire.document)
  if(failed){doc.content.pages=[];doc.content.pageCount=0;doc.content.chars=0;doc.content.extraction='none' as never}
  else doc.content.truncated=true
  doc.processing.status=(failed?'failed':'partial') as never;doc.processing.errors=[{code:'timeout',message:'Stopped',page:null}] as never
  const source=await observation(doc);vi.stubGlobal('fetch',vi.fn(async()=>Response.json(source)))
  const result=await resolvePolicyComparison(policyId,{before:{kind:'observation',observationId:source.observation.id},after:{kind:'observation',observationId:source.observation.id}})
  expect(result.before.metadata.completeness).toBe(failed?'unavailable':'partial');expect(result.before.text).toBe(failed?null:wire.document.content.pages[0]!.text)
  expect(result.limitations).toContain(failed?'unavailable-extraction':'incomplete-extraction')
 }
})
it('refuses mismatched source identity, extraction digests and original bytes',async()=>{
 for(const mutate of [(r:Awaited<ReturnType<typeof observation>>)=>{r.observation.id='0'.repeat(64)},(r:Awaited<ReturnType<typeof observation>>)=>{r.observation.document.sha256='0'.repeat(64)},(r:Awaited<ReturnType<typeof observation>>)=>{r.observation.source.resourceId='different-resource'},(r:Awaited<ReturnType<typeof observation>>)=>{r.document.original.bytes='ZmFrZQ=='}]){
  const source=await observation();mutate(source);vi.stubGlobal('fetch',vi.fn(async()=>Response.json(source)))
  await expect(resolvePolicyComparison(policyId,{before:{kind:'observation',observationId:wire.observationId},after:{kind:'revision',revisionId:newId}})).rejects.toThrow()
 }
})

it('accepts a retained empty original upload without treating an unchanged empty narrative as unavailable',async()=>{
 const old=saved(oldId,''),next=saved(newId,'')
 Object.assign(old.revision.candidate,{source:{origin:'user-upload',name:'empty.txt',mediaType:'text/plain',content:{sha256:'0'.repeat(64),bytes:0}}})
 endpoint(old,next)
 const result=await resolvePolicyComparison(policyId,pair)
 expect(result.before.metadata.original?.bytes).toBe(0);expect(result.before.text).toBe('');expect(result.before.metadata.completeness).toBe('complete')
 expect(result.differences.map(d=>d.field)).not.toContain('text')
})

it('refuses a session change between the exact retained sides',async()=>{
 const fetch=endpoint(),normal=fetch.getMockImplementation()!
 fetch.mockImplementation(async(input,init)=>{const reply=await normal(input,init);if(String(input).endsWith('/revisions/'+oldId))giveThisPageASessionForTesting('changed-session');return reply})
 await expect(resolvePolicyComparison(policyId,pair)).rejects.toThrow()
 expect(fetch).toHaveBeenCalledTimes(2)
})
it('freezes the selected pair before asynchronous authenticated reads',async()=>{
 const selected=structuredClone(pair),fetch=endpoint(),normal=fetch.getMockImplementation()!
 fetch.mockImplementation(async(input,init)=>{const reply=await normal(input,init);if(String(input).includes('publicationId='))selected.after={kind:'revision',revisionId:'revision_'+'0'.repeat(48)};return reply})
 const result=await resolvePolicyComparison(policyId,selected)
 expect(result.after.selector).toEqual(pair.after);expect(result.after.identity.revisionId).toBe(newId)
})
