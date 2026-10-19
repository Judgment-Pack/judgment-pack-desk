import {expect,it} from 'vitest'
import {dialectTestCase,appendDialectTestCases} from './testSnapshots'
import {emptySuite} from '../packs/test-workspace/model'
import type {PreparationReply} from './client'
const saved:PreparationReply={sha256:'revision',document:{version:1,id:'collection',name:'Companies',recipeVersion:4,recipe:{intent:'Research',qualification:{kind:'pack',key:'pack',pack:'{}',digest:'pack-digest',mapping:{version:2,case:{facts:[],evidence:[]},sources:[]}}},createdAt:'',updatedAt:'',records:[{id:'company',name:'Company',sourceId:'source',row:2,class:'asserted',status:'provided',values:{size:'50'}}],imports:[],activity:[]}}
it('freezes exact prepared facts, origin and mapping without inferred expectations',async()=>{
 const facts={zero:'0',no:false,empty:null,range:{lower:'50',upper:'249'}}
 const c=await dialectTestCase(saved,saved.document.records[0],{input:{facts,evidence:{license:'unknown'}},factsText:'',evidenceText:''})
 facts.range.lower='999';expect(c.row.facts).toEqual({zero:'0',no:false,empty:null,range:{lower:'50',upper:'249'}})
 expect(c.row).not.toHaveProperty('expectedDisposition');expect(c.row).not.toHaveProperty('expectedErrorClass')
 expect(c.inputSnapshot?.source.records[0].values.size).toBe('50');expect(c.inputSnapshot?.packDigest).toBe('pack-digest')
})
it('deduplicates the same revision without overwriting independently reviewed expectations',async()=>{
 const a=await dialectTestCase(saved,saved.document.records[0],{input:{facts:{}},factsText:'',evidenceText:''})
 const b=await dialectTestCase(saved,saved.document.records[0],{input:{facts:{}},factsText:'',evidenceText:''})
 a.row.expectedDisposition={kind:'unresolved'}
 const suite=appendDialectTestCases(appendDialectTestCases(emptySuite(),[a]),[b]);expect(suite.cases).toHaveLength(1);expect(suite.cases[0].row.expectedDisposition).toEqual({kind:'unresolved'})
 const changed=structuredClone(saved);changed.document.records[0].values.size='51'
 const c=await dialectTestCase(changed,changed.document.records[0],{input:{facts:{}},factsText:'',evidenceText:''})
 expect(appendDialectTestCases(suite,[c]).cases).toHaveLength(2)
})

it('keeps legacy research-only collections with null imports usable',async()=>{
 const legacy=structuredClone(saved);legacy.document.imports=null as never
 const c=await dialectTestCase(legacy,legacy.document.records[0],{input:{facts:{}},factsText:'',evidenceText:''})
 expect(c.inputSnapshot?.source.sources).toEqual([])
})
