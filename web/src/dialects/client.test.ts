import {expect,it} from 'vitest'
import {exportDialect,type Preparation} from './client'
it('exports selected rows with lineage without leaking unselected rows through raw source contents',()=>{
 const record={id:'row-one',name:'Chosen',values:{name:'Chosen'},sourceId:'source-one',row:1,class:'asserted' as const,status:'provided' as const}
 const doc={version:1,createdAt:'2026-01-01',updatedAt:'2026-01-01',activity:[],id:'one',name:'Suppliers',recipe:{intent:'Review suppliers'},recipeVersion:2,recipes:[{version:1,name:'Suppliers',recipe:{intent:'Original instructions'},at:'2026-01-01'},{version:2,name:'Suppliers',recipe:{intent:'Review suppliers'},at:'2026-01-02'}],records:[record,{...record,id:'row-two',name:'Private other'}],imports:[{id:'source-one',kind:'csv',name:'source.csv',digest:'sha256:abc',content:'Chosen\nPrivate other',at:'2026-01-01',recipeVersion:1}]} as Preparation
 const exported=exportDialect(doc,[record])
 expect(exported.recipes[0]!.recipe.intent).toBe('Original instructions');expect(exported.records).toEqual([record]);expect(exported.sources[0]!.recipeVersion).toBe(1)
 expect(JSON.stringify(exported)).not.toContain('Private other');expect(JSON.stringify(exported)).not.toContain('content')
})
