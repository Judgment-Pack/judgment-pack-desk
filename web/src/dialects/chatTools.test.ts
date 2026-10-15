import {expect,it,vi} from 'vitest'
import {dialectRecipeChatTools} from './chatTools'
import {dialectAPI} from './client'
import {recipeFromProposal,validDialectRecipeProposal,type DialectRecipeProposal} from './proposals'
vi.mock('./client',async original=>({...await original<typeof import('./client')>(),dialectAPI:vi.fn()}))
export const content={name:'Decision owners',intent:'Find decision owners at selected companies.',target:'people-at-companies' as const,fields:[{name:'role',description:'Current job title',type:'string' as const}],criteria:[{id:'seniority',description:'Owns a recurring decision'}],limits:{records:25,searches:10,pages:30,modelCalls:30}}
const tools=(addDraft=vi.fn())=>dialectRecipeChatTools({addDraft,afterTurnId:()=> 'turn-1',attribution:()=>({connection:'legacy-codex',model:'test-model'})})
it('retains a portable proposal without saving Dialects, granting a connection, or running research',async()=>{
 const add=vi.fn(),tool=tools(add).find(t=>t.name==='propose_dialect_recipe')!
 await tool.execute(content,new AbortController().signal)
 const draft=add.mock.calls[0][0] as DialectRecipeProposal
 expect(validDialectRecipeProposal(draft)).toBe(true)
 expect(draft).toMatchObject({...content,afterTurnId:'turn-1',attribution:{connection:'legacy-codex',model:'test-model'}})
 expect(recipeFromProposal(draft)).toMatchObject({ai:{connection:'',revision:'',model:''},companyIds:[],limits:{searches:0}})
 expect(dialectAPI).not.toHaveBeenCalled()
})
it('refuses injected authority, invented records, excess limits and ambiguous fields',async()=>{
 const add=vi.fn(),tool=tools(add).find(t=>t.name==='propose_dialect_recipe')!
 for(const bad of [{...content,ai:{connection:'legacy-codex'}},{...content,start:true},{...content,records:[{name:'invented'}]},{...content,limits:{...content.limits,records:500}},{...content,fields:[content.fields[0],content.fields[0]]},{...content,fields:[{...content.fields[0],name:'constructor'}]}])expect(await tool.execute(bad,new AbortController().signal)).toMatchObject({isError:true,structuredContent:{inputRecipeFailure:expect.any(String)}})
 expect(add).not.toHaveBeenCalled()
})
it('does not retain a proposal after the response is cancelled',async()=>{
 const add=vi.fn(),signal=new AbortController();signal.abort()
 await expect(tools(add).find(t=>t.name==='propose_dialect_recipe')!.execute(content,signal.signal)).rejects.toThrow()
 expect(add).not.toHaveBeenCalled()
})
it('reads recipe context without exposing record data or saved connection settings',async()=>{
 vi.mocked(dialectAPI).mockResolvedValueOnce({document:{name:'Existing',recipeVersion:2,recipe:{intent:'Research',research:{...recipeFromProposal({...content,version:1,draftId:'a',createdAt:'2026-01-01'}),ai:{connection:'private-id',revision:'secret',model:'internal'} }},records:[{email:'private@example.org'}]}})
 const result=await tools().find(t=>t.name==='read_dialect_recipe')!.execute({id:'prep_'+'a'.repeat(32)},new AbortController().signal)
 expect(JSON.stringify(result)).not.toMatch(/private@example|private-id|secret|internal/)
})

it('returns a useful validation result and accepts a corrected six-field proposal',async()=>{
 const add=vi.fn(),tool=tools(add).find(t=>t.name==='propose_dialect_recipe')!,signal=new AbortController().signal
 const fields=['business_description','fit_signal','country','employee_count_evidence','intake_channels','routing_complexity_signal'].map(name=>({name,description:'A sourced value; leave missing when unsupported.',type:'string'}))
 const proposal={...content,target:'companies',fields,criteria:[{id:'size',description:'At least 50 employees',qualificationTarget:''}]}
 const refused=await tool.execute(proposal,signal)
 expect(refused).toMatchObject({isError:true,structuredContent:{inputRecipeFailure:'criteria'}})
 expect(JSON.stringify(refused)).toContain('Omit qualificationTarget when unused')
 expect(add).not.toHaveBeenCalled()
 const corrected=await tool.execute({...proposal,criteria:[{id:'size',description:'At least 50 employees'}]},signal)
 expect(corrected.isError).not.toBe(true)
 expect(add).toHaveBeenCalledOnce();expect(add.mock.calls[0][0].fields).toHaveLength(6)
})
it('retains only known proposal failure reasons, never raw persistence errors',async()=>{
 const tool=tools(vi.fn(()=>{throw Error('PRIVATE_SENTINEL')})).find(t=>t.name==='propose_dialect_recipe')!
 const result=await tool.execute(content,new AbortController().signal)
 expect(result).toMatchObject({isError:true,structuredContent:{inputRecipeFailure:'proposal-unavailable'}})
 expect(JSON.stringify(result)).not.toContain('PRIVATE_SENTINEL')
})
