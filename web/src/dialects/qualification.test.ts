import {expect,it} from 'vitest'
import {recordColumns} from './qualification'
import type {DialectRecord} from './client'
const row:DialectRecord={id:'row-one',name:'Example',sourceId:'source-one',row:2,class:'asserted',status:'provided',values:{'Country/region':'CA','Internal~id':'00123'}}
it('offers escaped paths for exact CSV column names without guessing types',()=>{expect(recordColumns([row])).toEqual([{value:'/values/Country~1region',label:'Country/region'},{value:'/values/Internal~0id',label:'Internal~id'}])})

it('offers planned fields and selected-company links without fabricating records',()=>{
 const research={target:'people-at-companies',fields:[{name:'role',type:'string',description:'Role'}]} as import('./research').ResearchRecipe
 expect(recordColumns([],research).map(c=>c.label)).toEqual(['company_id','company_name','company_url','name','role','url','website'])
 expect(recordColumns([],{...research,target:'records'}).map(c=>c.label)).toEqual(['name','role','url','website'])
 expect(recordColumns([])).toEqual([])
})
