import { mappingFixture } from '../dialect/__fixtures__/desk-mappings'
import { expect, it } from 'vitest'
import { getBinding, setBinding } from './dialectEditorModel'
import type { ExecutableDialect } from './dialectTypes'
const mapping:ExecutableDialect=mappingFixture('copy-bindings')
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
 const advanced=structuredClone(mapping);advanced.sources![0]!.read.rule={ruleVersion:'1',parameters:{},clauses:[{when:{op:'always'},claim:{facts:[],evidence:{},acquisitionStatus:'unknown'},reason:'unknown'}]}
 expect(getBinding(advanced,'facts','/score').complex).toBe(true)
 expect(()=>setBinding(advanced,'facts','/score','case','/x')).toThrow()
 const duplicate=structuredClone(mapping);duplicate.sources![0]!.read.copy!.facts!.push({target:'/score',source:'/score'})
 expect(getBinding(duplicate,'facts','/score').complex).toBe(true)
})

it.each([3,'2.0.0','3.0.0'] as const)('preserves strict omission spelling when reassigning %s targets',format=>{
 const prior:ExecutableDialect={...(format===3?{version:3 as const}:{formatVersion:format,id:'urn:test:editing',version:'1.0.0'}),case:{facts:[],evidence:[]},unmapped:['/score']}
 const assigned=setBinding(prior,'facts','/score','case','/score')
 expect(assigned).not.toHaveProperty('unmapped')
 expect(assigned).not.toHaveProperty('sources')
 expect(assigned.case).toEqual({facts:[{target:'/score',source:'/score'}],evidence:[]})
 expect(setBinding(assigned,'facts','/score','omitted','').unmapped).toEqual(['/score'])
 expect(prior.unmapped).toEqual(['/score'])
})
