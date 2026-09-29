import { expect, it } from 'vitest'
import type { PackDocument } from '../mcp/types'
import { decodePresentation, decisionColor, emptyPresentation, lookupAppearance, outcomeAppearances, type OutcomeAppearances } from './decisionAppearance'
const pack={id:'https://example.test/policy',outcomes:[{id:'hold',label:'Hold'},{id:'approve',label:'Approve'},{id:'review',label:'Review'}]} as PackDocument
it('assigns distinct category colors without interpreting names as business meaning',()=>{
 const colors=outcomeAppearances(pack)
 expect(new Set(Object.values(colors).map(a=>a.color)).size).toBe(3)
 expect(Object.values(colors).every(a=>a.meaning==='categorical')).toBe(true)
 expect(outcomeAppearances({...pack,outcomes:[...pack.outcomes].reverse().map(a=>({...a,label:'Renamed'}))})).toEqual(colors)
})
it('preserves saved assignments as outcomes are added and applies only declared meanings',()=>{
 const saved={...outcomeAppearances(pack),approve:{color:'blue',meaning:'hold'}} as OutcomeAppearances
 const next=outcomeAppearances({...pack,outcomes:[{id:'aaa',label:'New'},...pack.outcomes]},saved)
 expect(next.approve).toEqual(saved.approve);expect(next.hold).toEqual(saved.hold)
 expect(decisionColor(next.approve!)).toBe('rose')
})
it('treats prototype-like IDs as ordinary data without inheriting mappings',()=>{
 const doc={...pack,id:'__proto__',outcomes:[{id:'__proto__',label:'One'},{id:'constructor',label:'Two'}]}
 const colors=outcomeAppearances(doc)
 expect(lookupAppearance({},'constructor')).toBeUndefined()
 expect(lookupAppearance(colors,'__proto__')?.meaning).toBe('categorical')
 const decoded=decodePresentation(JSON.parse('{"version":1,"packs":{"__proto__":{"outcomes":{"constructor":{"color":"blue","meaning":"categorical"}}}}}'))
 expect(Object.hasOwn(decoded.packs,'__proto__')).toBe(true)
 expect(({} as Record<string,unknown>).outcomes).toBeUndefined()
})
it('refuses unsupported presentation files instead of replacing them',()=>{
 expect(decodePresentation(emptyPresentation())).toEqual(emptyPresentation())
 for(const value of [{version:2,packs:{}},{version:1,packs:[],extra:true},{version:1,packs:{x:{outcomes:{y:{color:'red',meaning:'proceed'}}}}},{version:1,packs:{x:{outcomes:{y:{color:'blue',meaning:'approved'}}}}}])expect(()=>decodePresentation(value)).toThrow()
})
