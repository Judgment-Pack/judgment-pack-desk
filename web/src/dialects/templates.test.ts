import {describe,it,expect} from 'vitest'
import {dialectTemplates,readDialectTemplate,templateOrigin} from './templates'
describe('Desk authoring templates',()=>{
 it('round trips all presets, including generic records and non-research domains',()=>{
  for(const t of dialectTemplates())expect(readDialectTemplate(JSON.stringify(t))).toEqual(t)
  expect(dialectTemplates()[0].research).toBeUndefined()
  expect(dialectTemplates().filter(t=>t.research).every(t=>t.research!.target==='records')).toBe(true)
  expect(dialectTemplates().find(t=>t.id==='work-items')!.research).toBeUndefined()
 })
 it.each([{connection:'secret'},{records:[{}]},{script:'run()'},{approved:true},{qualification:{kind:'pack'}}])('rejects execution authority or data in templates: %j',extra=>{
  expect(()=>readDialectTemplate(JSON.stringify({...dialectTemplates()[0],...extra}))).toThrow()
 })
 it('rejects invalid budgets, fields, URLs masquerading as IDs and prototype fields',()=>{
  const t=dialectTemplates()[1]
  for(const r of [{...t,id:'https://example.org/template'}, {...t,research:{...t.research,ai:{connection:'other'}}},{...t,research:{...t.research,limits:{...t.research!.limits,records:1000}}},{...t,research:{...t.research,fields:[{name:'__proto__',type:'string',description:'unsafe'}]}}])expect(()=>readDialectTemplate(JSON.stringify(r))).toThrow()
  expect(()=>readDialectTemplate('x'.repeat(65537))).toThrow(/64 KB/)
 })
 it('pins template content and makes editable instantiations independent',async()=>{
  const t=dialectTemplates()[1],copy=readDialectTemplate(JSON.stringify(t))
  expect(await templateOrigin(copy)).toEqual(await templateOrigin(t))
  copy.research!.fields[0].description='changed'
  expect((await templateOrigin(copy)).digest).not.toBe((await templateOrigin(t)).digest)
  expect(t.research!.fields[0].description).not.toBe('changed')
 })
})
