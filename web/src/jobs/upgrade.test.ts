import {expect,it,vi} from 'vitest'
import {jobsAPI,type Job,type Release} from './client'
import {upgradeDraft,upgradeDraftState,upgradePackKey} from './upgrade'
import {deskFetch} from '../files/client'
vi.mock('../files/client',()=>({deskFetch:vi.fn()}))
it('seeds the actual upgrade entry by artifact identity and preserves exact facts without replay authority',async()=>{
 vi.mocked(deskFetch).mockResolvedValue(new Response('{"job":{"id":"job","name":"Review","releaseId":"release","revision":7},"release":{"packId":"policy","sample":{"facts":{"large":9007199254740993,"ratio":0.1234567890123456789,"no":false,"zero":0,"null":null}}}}'))
 const base=await jobsAPI<{job:Job;release:Release}>('jobs/job')
 const key=upgradePackKey(base,[{id:'project-key',packId:'policy'}])
 expect(key).toBe('project-key')
 expect(upgradePackKey(base,[{id:'first',packId:'policy'},{id:'second',packId:'policy'}])).toBe('')
 const draft=upgradeDraft(base,key)
 expect(draft.facts).toBe('{"large":9007199254740993,"ratio":0.1234567890123456789,"no":false,"zero":0,"null":null}')
 expect(draft.supplied).toBe(false)
 expect(draft.upgrade).toEqual({jobId:'job',baseReleaseId:'release',baseRevision:7})
 expect(draft).not.toHaveProperty('reviewed');expect(draft).not.toHaveProperty('trigger')
 expect(upgradeDraftState(base,draft)).toBe('current')
 const attempted={...draft,upgrade:{...draft.upgrade!,submittedReleaseId:'next'}}
 expect(upgradeDraftState({...base,job:{...base.job,revision:8,releaseId:'next'}},attempted)).toBe('completed')
 expect(upgradeDraftState({...base,job:{...base.job,revision:9,releaseId:'later'}},attempted)).toBe('changed')
})
it('copies named configuration and case text but excludes grants, responses and approvals',async()=>{
 const mapping={formatVersion:'3.0.0',id:'example',version:'1.0.0',case:{facts:[],evidence:[]},sources:[]}
 vi.mocked(deskFetch).mockResolvedValue(new Response(JSON.stringify({job:{id:'job',name:'Research',releaseId:'release',revision:2},release:{packId:'policy',inputMapping:mapping,sample:{source:{mapping,case:{zero:0,no:false,unknown:null},sources:{private:{grant:'secret',response:{receipt:'old'}}}}}}})))
 const draft=upgradeDraft(await jobsAPI('jobs/job'),'pack')
 expect(JSON.parse(draft.mapped!.text)).toEqual(mapping)
 expect(draft.mapped!.caseText).toBe('{"zero":0,"no":false,"unknown":null}')
 expect(JSON.stringify(draft)).not.toContain('secret');expect(JSON.stringify(draft)).not.toContain('receipt')
})

it('requires a new reviewed generated sample and never persists its consumed handoff grant',async()=>{
 const mapping={formatVersion:'3.0.0',id:'urn:example',version:'1.0.0',case:{facts:[],evidence:[]},sources:[{profile:'desk-reviewed-research-v1'}]}
 const imported={dialect:{...mapping,sources:[]},binding:{dialect:'sha256:published',sources:{}}}
 vi.mocked(deskFetch).mockResolvedValue(new Response(JSON.stringify({job:{id:'job',releaseId:'r',revision:1},release:{inputMapping:mapping,sample:{source:{mapping,import:imported,case:{grant:'consumed',digest:'snapshot'}}}}})))
 const draft=upgradeDraft(await jobsAPI('jobs/job'),'pack')
 expect(draft.mapped?.caseText).toBe('{}')
 expect(JSON.parse(draft.mapped!.importText!)).toEqual(imported)
 expect(JSON.stringify(draft)).not.toContain('consumed')
})
