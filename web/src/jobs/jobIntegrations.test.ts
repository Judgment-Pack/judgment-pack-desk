import {expect,it} from 'vitest'
import {canChangeIntegration,matchesJobGateway,setSourceIntegration,supportsJobProfile} from './jobIntegrations'
import type {MappingV2,ProfileEntry} from './mappingTypes'
import type {ResearchConfig} from '../config/deskConfig'
const entry:ProfileEntry={digest:'new-digest',profile:{id:'registry',shape:'mcp',source:'registry',class:'record',authority:'gateway',publicKey:'key',adapter:{name:'mcp',version:'1',digest:'digest'},endpoint:null,tools:['read_case']}}
const base:MappingV2={version:2,case:{facts:[{target:'/manual',source:'/manual'}],evidence:[]},sources:[]}
it('requires a supported execution contract and the configured signer/authority',()=>{
 expect(supportsJobProfile(entry)).toBe(true)
 expect(supportsJobProfile({...entry,profile:{...entry.profile,shape:'http'}})).toBe(false)
 expect(supportsJobProfile({...entry,profile:{...entry.profile,tools:[]}})).toBe(false)
 const config={gateway:{authority:'gateway',signer:{public:'key'}}} as ResearchConfig
 expect(matchesJobGateway(entry,config)).toBe(true)
 expect(matchesJobGateway({...entry,profile:{...entry.profile,publicKey:'another-key'}},config)).toBe(false)
})
it('allocates a unique source name without mutating the existing mapping',()=>{
 const prior=setSourceIntegration(base),sparse={...prior,sources:[{...prior.sources![0]!,name:'source2'}]}
 const next=setSourceIntegration(sparse,entry)
 expect(next.sources?.map(s=>s.name)).toEqual(['source2','source1'])
 expect(base.sources).toEqual([])
 expect(next.case).toEqual(base.case)
})
it('changing authority resets request parameters and pins while preserving simple target assignments',()=>{
 const original=setSourceIntegration(base,entry),source=original.sources![0]!
 source.arguments={tool:'old-tool',arguments:{account:'old-account'}}
 source.read.copy!.facts=[{target:'/score',source:'/score'}]
 const next=setSourceIntegration(original,{...entry,digest:'other',profile:{...entry.profile,id:'other',tools:['new-tool']}},source.name)
 expect(next.sources![0]).toMatchObject({profile:'other',profileDigest:'other',arguments:{tool:'new-tool',arguments:{}},read:source.read})
 expect(source.arguments).toEqual({tool:'old-tool',arguments:{account:'old-account'}})
})
it('does not silently rewrite derivations or downstream dependencies',()=>{
 const mapping=setSourceIntegration(base,entry)
 mapping.sources![0]!.read={rule:{version:1}}
 expect(canChangeIntegration(mapping,'source1')).toBe(false)
 expect(()=>setSourceIntegration(mapping,undefined,'source1')).toThrow()
 const dependent=setSourceIntegration(setSourceIntegration(base,entry),entry)
 dependent.sources![1]!.parameters={id:{from:'source1',pointer:'/id',type:'string'}}
 expect(canChangeIntegration(dependent,'source1')).toBe(false)
})
it('clears retired Drive picker parameters and never overwrites an unrelated parameter',()=>{
 const drive={...entry,profile:{...entry.profile,id:'drive',source:'drive',shape:'http' as const}}
 const selected=setSourceIntegration(base,drive)
 expect(selected.case?.parameters?.source1File?.pointer).toBe('/selections/source1/fileId')
 const local=setSourceIntegration(selected,undefined,'source1')
 expect(local.case?.parameters).toEqual({})
 expect(local.sources![0]!.profile).toBeUndefined()
 expect(()=>setSourceIntegration({...base,case:{...base.case!,parameters:{source1File:{pointer:'/unrelated',type:'string'}}}},drive)).toThrow()
})

const calculator={...entry,profile:{...entry.profile,id:'fx',tools:['convert'],calculator:{name:'fx-convert',version:'2.1.0'}}}
it('creates a calculator source with empty bindings without guessing inputs, tables or request parameters',()=>{
 const next=setSourceIntegration(base,calculator)
 expect(next.sources![0]).toEqual({name:'source1',kind:'operation',profile:'fx',profileDigest:'new-digest',maxAge:300,arguments:{tool:'convert',arguments:{}},read:{copy:{facts:[],evidence:[]}},calculation:{inputs:{},tables:{}}})
 expect(next.case).toEqual(base.case)
 expect(base.sources).toEqual([])
})
it('replacing an ordinary source with a calculator adds empty bindings and keeps output assignments',()=>{
 const previous=setSourceIntegration(base,entry)
 previous.sources![0]!.read.copy!.facts=[{target:'/converted',source:'/result'}]
 const next=setSourceIntegration(previous,calculator,'source1')
 expect(next.sources![0]!.calculation).toEqual({inputs:{},tables:{}})
 expect(next.sources![0]!.read).toEqual(previous.sources![0]!.read)
 expect(previous.sources![0]).not.toHaveProperty('calculation')
})
it.each(['operation','local-file','google-drive'] as const)('replacing a calculator with %s removes calculation without changing the old mapping',kind=>{
 const prior=setSourceIntegration(base,calculator)
 const replacement=kind==='local-file'?undefined:kind==='google-drive'?{...entry,profile:{...entry.profile,source:'drive',shape:'http' as const}}:entry
 const next=setSourceIntegration(prior,replacement,'source1')
 expect(next.sources![0]).not.toHaveProperty('calculation')
 expect(prior.sources![0]!.calculation).toEqual({inputs:{},tables:{}})
})
it('ordinary operation profiles still generate exactly the existing source shape',()=>{
 expect(setSourceIntegration(base,entry).sources![0]).toEqual({name:'source1',kind:'operation',profile:'registry',profileDigest:'new-digest',maxAge:300,arguments:{tool:'read_case',arguments:{}},read:{copy:{facts:[],evidence:[]}}})
})
it('a calculator MCP profile stays an operation even if its source is named drive',()=>{
 const result=setSourceIntegration(base,{...calculator,profile:{...calculator.profile,source:'drive'}})
 expect(result.sources![0]).toMatchObject({kind:'operation',arguments:{tool:'convert',arguments:{}},calculation:{inputs:{},tables:{}}})
 expect(result.sources![0]).not.toHaveProperty('provider')
})
it('switching calculators resets old bindings rather than guessing bindings for the new tool',()=>{
 const prior=setSourceIntegration({...base,case:{...base.case!,parameters:{amount:{pointer:'/amount',type:'string'}}}},calculator)
 prior.sources![0]!.calculation={inputs:{amount:'amount'},tables:{rates:86400}}
 const next=setSourceIntegration(prior,{...calculator,profile:{...calculator.profile,id:'tax',tools:['tax'],calculator:{name:'tax',version:'1'}}},'source1')
 expect(next.sources![0]!.calculation).toEqual({inputs:{},tables:{}})
 expect(next.sources![0]!.arguments).toEqual({tool:'tax',arguments:{}})
 expect(prior.sources![0]!.calculation).toEqual({inputs:{amount:'amount'},tables:{rates:86400}})
})
