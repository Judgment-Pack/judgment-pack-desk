import {describe,it,expect} from 'vitest'
import type {Candidate,ImportRecord} from '../exchange/client'
import {researchDraftFromPreset} from './presetResearchDraft'
const digest='sha256:'+'a'.repeat(64)
const record={id:'b'.repeat(64),entryId:'example/research',tag:'v1.0.0'} as ImportRecord
const fields=[{name:'description',type:'text',required:true},{name:'count',type:'integer',description:'Count'},{name:'amount',type:'decimal',description:'Exact amount'},{name:'approved',type:'boolean',description:'Approved'},{name:'tags',type:'text-list',description:'Tags'},{name:'size',type:'range',description:'Size range'}]
const document=()=>({version:3,case:{facts:[],evidence:[],parameters:{request:{type:'string'}}},sources:[{name:'research',kind:'research',model:'model',question:{$param:'request'},constraints:{region:'Ontario'},evidence:['Public sources'],fields,capabilities:['answer'],read:{copy:{facts:[],evidence:[]}}}]})
function candidate(doc:unknown=document()):Candidate{return {row:{id:record.entryId,entryDigest:digest,dialectDigest:digest},entry:{kind:'preset',title:'Research',description:'Portable research',licence:'Apache-2.0',author:'Example',sourceKinds:['research']},files:{},document:JSON.stringify(doc),digest}}
describe('Exchange research authoring fork',()=>{
 it('preserves every requested type, instructions and immutable origin without importing authority',()=>{
  const preset=candidate(),original=structuredClone(preset),result=researchDraftFromPreset(preset,record,'Find public procurements')
  expect(result.template.intent).toContain('Find public procurements')
  expect(result.template.intent).toContain('Ontario')
  expect(result.template.intent).toContain('Public sources')
  expect(result.template.intent).toContain('"required": true')
  expect(result.template.research!.fields.map(f=>f.type)).toEqual(['text','integer','decimal','boolean','text-list','range'])
  expect(result.template.research!.fields[0].description).toBe('description')
  expect(result.origin).toEqual({id:'exchange-'+record.id,name:'Research',digest,license:'Apache-2.0'})
  expect(result.template.research).not.toHaveProperty('ai')
  expect(result.template.research).not.toHaveProperty('approved')
  result.template.research!.fields[0].description='Edited'
  expect(preset).toEqual(original)
 })
 it('retains literal questions rather than replacing them with an unrelated purpose',()=>{
  const doc=document();doc.sources[0].question='Classify the supplied dataset' as any
  expect(researchDraftFromPreset(candidate(doc),record,'Other purpose').template.intent).toMatch(/^Classify the supplied dataset/)
 })
 it('refuses multi-source, computed or unresolved parameter templates instead of silently degrading them',()=>{
  const multiple=document();multiple.sources.push({...multiple.sources[0],name:'second'})
  const parameter=document();parameter.sources[0].constraints={region:{$param:'request'}} as any
  const text=document();text.sources[0].question={$text:'Research ${request}'} as any
  const dependency=document();dependency.case.parameters.request={type:'string',from:{source:'other',pointer:'/value'}} as any
  for(const doc of [multiple,parameter,text,dependency])expect(()=>researchDraftFromPreset(candidate(doc),record,'Find records')).toThrow(/Choose a research preset/)
 })
 it('refuses oversized fields and instructions without truncating the imported document',()=>{
  const doc=document();doc.sources[0].fields=Array.from({length:31},(_,i)=>({name:'field'+i,type:'text',description:'Field'}))
  expect(()=>researchDraftFromPreset(candidate(doc),record,'Find records')).toThrow(/editor limits/)
  expect(()=>researchDraftFromPreset(candidate(),record,'x'.repeat(12001))).toThrow(/editor limits/)
 })

 it('checks UTF-8 byte bounds before creating an unsavable fork',()=>{
  const title=candidate();title.entry.title='会'.repeat(60)
  const license=candidate();license.entry.licence='会'.repeat(60)
  const doc=document();doc.sources[0].fields[0].description='会'.repeat(334)
  for(const preset of [title,license,candidate(doc)])expect(()=>researchDraftFromPreset(preset,record,'Find records')).toThrow(/editor limits/)
  expect(()=>researchDraftFromPreset(candidate(),record,'会'.repeat(4000))).toThrow(/editor limits/)
 })
 it('refuses a different import identity or an executable dialect entry',()=>{
  expect(()=>researchDraftFromPreset(candidate(),{...record,entryId:'other'},'Purpose')).toThrow()
  expect(()=>researchDraftFromPreset(candidate(),{...record,id:'unsafe'},'Purpose')).toThrow()
  const entry=candidate();entry.entry.kind='dialect'
  expect(()=>researchDraftFromPreset(entry,record,'Purpose')).toThrow()
 })
})
