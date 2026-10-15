import {beforeEach,expect,it,vi} from 'vitest'
import {dialectActionChatTools} from './chatActionTools'
import {applyDialectRecipeAction,validDialectChatAction,type DialectChatAction} from './chatActions'
import {dialectAPI,type PreparationReply} from './client'
vi.mock('./client',async original=>({...await original<typeof import('./client')>(),dialectAPI:vi.fn()}))
const inputId='prep_'+'a'.repeat(32),revision='a'.repeat(64),rowId='row_'+'b'.repeat(64)
const saved:PreparationReply={sha256:revision,document:{version:1,id:inputId,name:'Companies',recipeVersion:2,createdAt:'2026-01-01',updatedAt:'2026-01-01',recipe:{intent:'Find companies',research:{version:1,target:'companies',fields:[{name:'sector',description:'Sector',type:'string'}],criteria:[],limits:{records:3,searches:1,pages:3,modelCalls:10},ai:{connection:'user-owned',revision:'sensitive-revision',model:'model'},search:{connection:'user-search',revision:'search-revision'}}},imports:[],activity:[],records:[{id:rowId,name:'Example',sourceId:'source',row:1,class:'asserted',status:'provided',values:{sector:'Technology'}}]}}
const makeAction=(patch:Partial<DialectChatAction>={}):DialectChatAction=>({version:1,id:'action-1',createdAt:'2026-01-01',inputId,name:'Companies',revision,recipeVersion:2,kind:'update-recipe',patch:{intent:'Updated instructions'},...patch})
const add=vi.fn(),tools=dialectActionChatTools({addAction:add,afterTurnId:()=> 'turn-1'})
const call=(name:string,args:Record<string,unknown>,signal=new AbortController().signal)=>tools.find(tool=>tool.name===name)!.execute(args,signal)
beforeEach(()=>{vi.resetAllMocks();vi.mocked(dialectAPI).mockResolvedValue(structuredClone(saved))})
it('retains a revision-bound proposal using only a read, without saving or starting work',async()=>{
 await call('propose_dialect_action',{id:inputId,revision,kind:'update-recipe',patch:{intent:'New instructions'}})
 expect(add).toHaveBeenCalledOnce();expect(validDialectChatAction(add.mock.calls[0][0])).toBe(true)
 expect(add.mock.calls[0][0]).toMatchObject({inputId,revision,recipeVersion:2,afterTurnId:'turn-1'})
 expect(vi.mocked(dialectAPI).mock.calls).toEqual([['/'+inputId]])
})
it('merges portable changes while preserving chosen connections, records and prior versions',()=>{
 const action=makeAction({patch:{research:{fields:[{name:'employees',type:'number',description:'Employee count'}]}}})
 const next=applyDialectRecipeAction(saved,action)
 expect(next.recipe.research?.ai).toEqual(saved.document.recipe.research?.ai)
 expect(next.recipe.research?.search).toEqual(saved.document.recipe.research?.search)
 expect(saved.document.recipe.research?.fields[0].name).toBe('sector')
 expect(saved.document.records).toHaveLength(1)
 expect(applyDialectRecipeAction(saved,makeAction({kind:'remove-recipe',patch:undefined,section:'all'}))).toEqual({name:'Companies',recipe:{intent:''}})
 expect(saved.document.records[0].class).toBe('asserted')
 expect(()=>applyDialectRecipeAction({...saved,sha256:'b'.repeat(64)},action)).toThrow('Dialects changed')
})
it('rejects injected authority, review decisions, extra fields, invalid limits and stale proposals',async()=>{
 const base={id:inputId,revision,kind:'update-recipe'}
 for(const patch of [{research:{ai:{connection:'injected'}}},{review:{decision:'accepted'}},{research:{fields:[]}},{research:{limits:{records:100,searches:0,pages:0,modelCalls:1}}},{research:{fields:[{name:'constructor',type:'string',description:'Bad'}]}}])await expect(call('propose_dialect_action',{...base,patch})).rejects.toThrow()
 await expect(call('propose_dialect_action',{...base,patch:{intent:'New'},start:true})).rejects.toThrow()
 await expect(call('propose_dialect_action',{...base,revision:'b'.repeat(64),patch:{intent:'New'}})).rejects.toThrow('Dialects changed')
 expect(add).not.toHaveBeenCalled()
})
it('refuses generated or missing record evaluation and never downgrades generated origin',async()=>{
 const current=structuredClone(saved);current.document.records[0].class='generated';vi.mocked(dialectAPI).mockResolvedValue(current)
 await expect(call('propose_dialect_action',{id:inputId,revision,kind:'evaluate',recordIds:[rowId]})).rejects.toThrow('Ready for evaluation')
 await expect(call('propose_dialect_action',{id:inputId,revision,kind:'evaluate',recordIds:['row_'+'c'.repeat(64)]})).rejects.toThrow('no longer available')
 expect(add).not.toHaveBeenCalled();expect(current.document.records[0].class).toBe('generated')
})
it('requires an active exact run before offering Stop and does not stop it automatically',async()=>{
 const runId='research_'+'d'.repeat(32)
 vi.mocked(dialectAPI).mockImplementation(async path=>(path?.endsWith('/research')?{items:[{id:runId,state:'running'}]}:saved) as any)
 await call('propose_dialect_action',{id:inputId,revision,kind:'stop-research',runId})
 expect(add).toHaveBeenCalledOnce();expect(vi.mocked(dialectAPI).mock.calls.every(([,method])=>!method)).toBe(true)
 await expect(call('propose_dialect_action',{id:inputId,revision,kind:'stop-research',runId:'research_'+'e'.repeat(32)})).rejects.toThrow('no longer active')
})
it('does not keep a proposal after cancellation',async()=>{
 const controller=new AbortController();vi.mocked(dialectAPI).mockImplementation(async()=>{controller.abort();return saved as any})
 await expect(call('propose_dialect_action',{id:inputId,revision,kind:'research'},controller.signal)).rejects.toThrow()
 expect(add).not.toHaveBeenCalled()
})
it('reads bounded record values without leaking connections or claiming verification',async()=>{
 const current=structuredClone(saved);current.document.records[0].values.sector='x'.repeat(2000);vi.mocked(dialectAPI).mockResolvedValue(current)
 const result=await call('read_dialect_records',{id:inputId,limit:1})
 const body=JSON.parse((result as any).content[0].text)
 expect(body.items[0].values.sector).toEqual({text:'x'.repeat(500),truncated:true})
 expect(JSON.stringify(body)).not.toMatch(/sensitive-revision|user-owned|user-search/)
 expect(body.items[0].class).toBe('asserted');expect(body.guidance).toContain('not independently verified')
 await expect(call('read_dialect_records',{id:inputId,limit:11})).rejects.toThrow()
})
it('offers evaluation of current human-reviewed research through a confirmation card only',async()=>{
 const current=structuredClone(saved)
 current.document.recipe.qualification={kind:'pack',key:'fixture',digest:'sha256:'+'a'.repeat(64),pack:'{}',mapping:{version:2,sources:[]}}
 const record=current.document.records[0];record.class='generated';record.review={recordId:record.id,recordDigest:'a'.repeat(64),recipeVersion:current.document.recipeVersion,decision:'accepted',note:'Reviewed',at:'2026-10-12T10:00:00Z',reviewer:{kind:'local-access'}}
 vi.mocked(dialectAPI).mockResolvedValue(current)
 await call('propose_dialect_action',{id:inputId,revision,kind:'evaluate',recordIds:[rowId]})
 expect(add).toHaveBeenCalledOnce();expect(add.mock.calls[0][0].kind).toBe('evaluate')
 expect(vi.mocked(dialectAPI).mock.calls).toEqual([['/'+inputId]])
 expect(record.class).toBe('generated')
 record.review.recipeVersion--
 await expect(call('propose_dialect_action',{id:inputId,revision,kind:'evaluate',recordIds:[rowId]})).rejects.toThrow('Ready for evaluation')
 expect(add).toHaveBeenCalledOnce()
})
it('preserves user retry policy and does not enable retries for an existing legacy recipe',()=>{
 const action=makeAction({patch:{research:{fields:[{name:'size',type:'number',description:'Size'}]}}})
 expect(applyDialectRecipeAction(saved,action).recipe.research?.retries).toBeUndefined()
 const configured=structuredClone(saved);configured.document.recipe.research!.retries={mode:'temporary',maxRetries:1,searchTimeouts:true}
 expect(applyDialectRecipeAction(configured,action).recipe.research?.retries).toEqual(configured.document.recipe.research!.retries)
 const empty=structuredClone(saved);delete empty.document.recipe.research
 expect(applyDialectRecipeAction(empty,action).recipe.research?.retries).toEqual({mode:'temporary',maxRetries:2,searchTimeouts:false})
 expect(validDialectChatAction(makeAction({patch:{research:{retries:{mode:'temporary',maxRetries:3,searchTimeouts:true}} as any}}))).toBe(false)
})


it.each([null,undefined,[]])('reads generated records with empty citations (%s) while retaining quoted evidence',async empty=>{
 const current=structuredClone(saved),row=current.document.records[0]
 row.class='generated';row.status='researched'
 row.research={runId:'run',recipeVersion:2,entity:{id:'entity',kind:'company',name:row.name,url:'https://example.com',fields:{
  employee_count:{status:'missing',sources:empty,note:'No public employee count was retained.'},
  sector:{value:'Technology',status:'supported',sources:[{sourceId:'source',quote:'Technology company.'}]}
 }}}
 const before=JSON.stringify(current)
 vi.mocked(dialectAPI).mockResolvedValue(current)
 const result=await call('read_dialect_records',{id:inputId,limit:1})
 const body=JSON.parse((result as any).content[0].text)
 expect(body.items[0].class).toBe('generated')
 expect(body.items[0].research.fields.employee_count).toEqual({status:'missing',note:'No public employee count was retained.',sources:[]})
 expect(body.items[0].research.fields.sector.sources).toEqual([{id:'source',quote:'Technology company.',truncated:false}])
 expect(JSON.stringify(current)).toBe(before)
 expect(vi.mocked(dialectAPI).mock.calls).toEqual([['/'+inputId]])
})
it('offers selected reprocessing as a review-only action in either chat surface',async()=>{
 const data=structuredClone(saved)
 data.document.records[0]={...data.document.records[0],class:'generated',research:{runId:'research_'+'c'.repeat(32),recipeVersion:2,entity:{id:'entity',kind:'company',name:'Example',url:'https://example.com',fields:{}}}}
 vi.mocked(dialectAPI).mockResolvedValue(data)
 await call('propose_dialect_action',{id:inputId,revision,kind:'reprocess',recordIds:[rowId]})
 expect(add).toHaveBeenCalledOnce();expect(add.mock.calls[0][0]).toMatchObject({kind:'reprocess',recordIds:[rowId]})
 expect(validDialectChatAction(add.mock.calls[0][0])).toBe(true)
 expect(vi.mocked(dialectAPI).mock.calls.every(([,method])=>!method||method==='GET')).toBe(true)
 await expect(call('propose_dialect_action',{id:inputId,revision,kind:'reprocess',recordIds:['row_'+'d'.repeat(64)]})).rejects.toThrow('No records were skipped')
})
