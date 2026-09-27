import { expect, it } from 'vitest'
import { getBinding, setBinding } from './mappingEditorModel'
import type { MappingV2 } from './mappingTypes'
const mapping:MappingV2={version:2,case:{facts:[{target:'/score',source:'/facts/score'},{target:'/name',source:'/facts/name'}],evidence:[]},sources:[{name:'records',kind:'selected-file',provider:'local-file',read:{copy:{facts:[],evidence:[]}}}]}
it('reassigns only the selected target and preserves the source mapping',()=>{
 const next=setBinding(mapping,'facts','/score','records','/score')
 expect(getBinding(next,'facts','/score')).toMatchObject({owner:'records',path:'/score'})
 expect(next.case?.facts).toEqual([{target:'/name',source:'/facts/name'}])
 expect(mapping.case?.facts).toHaveLength(2)
 const omitted=setBinding(next,'facts','/score','omitted','')
 expect(omitted.unmapped).toContain('/score')
 expect(omitted.sources?.[0]?.read.copy?.facts).toEqual([])
 expect(setBinding(omitted,'facts','/score','case','/facts/score').unmapped).not.toContain('/score')
})
it('does not rewrite advanced derivation rules or competing writers',()=>{
 const advanced=structuredClone(mapping);advanced.sources![0]!.read.rule={version:1}
 expect(getBinding(advanced,'facts','/score').complex).toBe(true)
 expect(()=>setBinding(advanced,'facts','/score','case','/x')).toThrow()
 const duplicate=structuredClone(mapping);duplicate.sources![0]!.read.copy!.facts.push({target:'/score',source:'/score'})
 expect(getBinding(duplicate,'facts','/score').complex).toBe(true)
})
