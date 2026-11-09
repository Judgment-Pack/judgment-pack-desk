import {describe,expect,it} from 'vitest'
import {DIALECT_RETAINED_COLUMN_DRAFT} from '@judgment-pack/harness/dialects'
import shapes from './testdata/retained-recipe-shapes.json'
import {buildRetainedDialect,readRetainedDialectRules,retainedDialectRules,validRetainedContract} from './dialect'
import type {DialectPackBinding} from './client'

const fixture=()=>({...structuredClone(shapes),conversionContract:structuredClone(DIALECT_RETAINED_COLUMN_DRAFT)})
const pack:DialectPackBinding={kind:'pack',key:'example',pack:'{}',digest:'sha256:fixture',mapping:{version:2,sources:[]}}
describe('closed retained recipe shapes',()=>{
 it('reads the shared handwritten five-kind and availability vector without roles',()=>{
  const binding=fixture(),rules=readRetainedDialectRules(JSON.stringify(binding))
  expect(rules.facts.map(f=>f.conversion.kind)).toEqual(['text','decimal','number','boolean','research-value'])
  expect(rules.evidence).toEqual([{requirement:'license',column:'License'}])
  const built=buildRetainedDialect(pack,rules,DIALECT_RETAINED_COLUMN_DRAFT,['/unused'],['other'])
  expect(built.mapping).toEqual(binding.mapping)
  expect(built.conversions).toEqual(binding.conversions)
  expect(JSON.stringify(built)).not.toContain('roles')
  expect(retainedDialectRules(built)).toEqual(rules)
 })
 it('preserves old omitted members in serialized bindings',()=>{
  const old={...pack,mapping:{version:2,case:{facts:[{target:'/country',source:'/values/Country'}],evidence:[{requirement:'license',source:'/values/License'}]},sources:[]}}
  expect(JSON.stringify(old.mapping)).toBe('{"version":2,"case":{"facts":[{"target":"/country","source":"/values/Country"}],"evidence":[{"requirement":"license","source":"/values/License"}]},"sources":[]}')
  expect(()=>retainedDialectRules(old)).toThrow()
  expect(JSON.stringify(old)).not.toContain('conversionContract')
 })
 const mutations:[string,string][]=[
  ['"conversionContract":{','"ConversionContract":{'],
  ['"conversionContract":{','"conversionContract":{"roles":[],'],
  ['"contract":"retained-column/1"','"contract":"retained-column/1","contract":"retained-column/1"'],
  ['"profile":"base"','"profile":"extended"'],
  ['"schemaSha256":','"SchemaSha256":'],
  ['"contractVersion":"1"','"contractVersion":1'],
  ['"contractVersion":"1"','"contractVersion":"2"'],
  ['"column":"Name"','"Column":"Name"'],
  ['"column":"Name"','"column":"Name","column":"Other"'],
  ['"column":"Name"','"column":null'],
  ['"kind":"text"','"kind":"unsupported"'],
  ['"kind":"text"','"kind":["text"]'],
  ['"mapping":','"unknown":true,"mapping":'],
  ['"kind":"text"','"kind":"text","trueValue":""'],
  ['"trueValue":"YES"','"trueValue":null'],
  ['"trueValue":"YES"','"trueValue":"NO"'],
  [',"falseValue":"NO"',''],
  ['"trueValue":"YES"','"trueValue":"\\u0085"'],
  ['"contractVersion":"1","column":"Name",','"column":"Name",'],
  ['"contractVersion":"1","column":"License"','"column":"License"'],
  ['"column":"License"','"column":""'],
  ['"contract":"retained-column/1",',''],
  ['"spec":{','"spec":{"roles":[],'],
  ['"conversionContract":{','"conversionContract":{"unknown":true,'],
  ['"kind":"text"','"Kind":"text"'],
  ['"availability":{"contractVersion":"1","column":"License"}','"availability":null'],
  ['"availability":{"contractVersion":"1","column":"License"}','"availability":{"contractVersion":"1","column":"License","other":true}'],
  ['"conversion":{"contractVersion":"1","column":"Name","kind":"text"}','"conversion":null'],
  ['"profile":"base","case"','"profile":null,"case"'],
  ['"sources":[]','"sources":null'],
  ['"/text":{"kind":"text"}','"/text":{"kind":"text","trueValue":null}'],
 ]
 it.each(mutations)('refuses the handwritten raw mutation %s → %s',(from,to)=>{
  const raw=JSON.stringify(fixture()),changed=raw.replace(from,to)
  expect(changed).not.toBe(raw)
  expect(()=>readRetainedDialectRules(changed)).toThrow()
 })
 it('refuses null selection and any missing, extra or disagreeing sidecar',()=>{
  expect(validRetainedContract(null)).toBe(false)
  expect(()=>retainedDialectRules({...fixture(),conversionContract:null})).toThrow()
  for(const conversions of [{...shapes.conversions,'/extra':{kind:'text'}},{...shapes.conversions,'/text':{kind:'number'}},{...shapes.conversions,'/boolean':{kind:'boolean',trueValue:'yes',falseValue:'NO'}},Object.fromEntries(Object.entries(shapes.conversions).filter(([key])=>key!=='/text'))])expect(()=>retainedDialectRules({...fixture(),conversions})).toThrow()
 })
 it('refuses duplicate/overlapping logical targets and omitted selectors/leaves',()=>{
  const duplicate=fixture();duplicate.mapping.case.facts.push(duplicate.mapping.case.facts[0]!)
  expect(()=>retainedDialectRules(duplicate)).toThrow()
  const overlap=fixture();overlap.mapping.case.facts[1]!.target='/text/child'
  expect(()=>retainedDialectRules(overlap)).toThrow()
  const duplicateEvidence=fixture();duplicateEvidence.mapping.case.evidence.push(duplicateEvidence.mapping.case.evidence[0]!)
  expect(()=>retainedDialectRules(duplicateEvidence)).toThrow()
  const noProfile=fixture();expect(()=>retainedDialectRules({...noProfile,mapping:{...noProfile.mapping,profile:undefined}})).toThrow()
  const ordinary=fixture();ordinary.mapping.case.facts[0]!.source='/values/Name'
  expect(()=>retainedDialectRules(ordinary)).toThrow()
 })
 it('accepts exact scalar names and checks UTF-8 bounds and Spec whitespace',()=>{
  for(const column of ['é'.repeat(80),' ','__proto__','constructor','😀']){
   const binding=fixture();binding.mapping.case.facts[0]!.conversion.column=column
   expect(()=>retainedDialectRules(binding)).not.toThrow()
  }
  for(const column of ['', 'é'.repeat(81),'\ud800']){
   const binding=fixture();binding.mapping.case.facts[0]!.conversion.column=column
   expect(()=>retainedDialectRules(binding)).toThrow()
  }
  const binding=fixture();binding.mapping.case.facts[3]!.conversion.trueValue='\u0085'
  expect(()=>retainedDialectRules(binding)).toThrow()
 })
})
