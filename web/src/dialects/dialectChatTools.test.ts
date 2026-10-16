import {checkDialect,DialectCheckError} from '../dialect/authoring'
vi.mock('../dialect/authoring',async original=>({...await original<object>(),checkDialect:vi.fn()}))
import {beforeEach,expect,it,vi} from 'vitest'
import {dialectChatTools} from './dialectChatTools'
import {buildDialect,validDialectProposal,type DialectProposal} from './dialect'
import {validDialectChatAction,type DialectChatAction} from './chatActions'
import {dialectAPI,type PreparationReply} from './client'
import {packBinding} from './qualification'
import {readFile} from '../files/client'
vi.mock('./client',async original=>({...await original<typeof import('./client')>(),dialectAPI:vi.fn()}))
vi.mock('../files/client',async original=>({...await original<typeof import('../files/client')>(),readFile:vi.fn()}))
const pack=JSON.stringify({id:'fit',version:'1.0.0',decision:{},evidenceRequirements:[{id:'source',description:'Source'}],rules:[{when:{op:'all',conditions:[{op:'fact',path:'/size',operator:'greater-than',value:'50'},{op:'fact',path:'/active',operator:'equals',value:true},{op:'fact',path:'/country',operator:'equals',value:'CA'}]}}]})
const id='prep_'+'a'.repeat(32),revision='a'.repeat(64),rowId='row_'+'b'.repeat(64)
const base:PreparationReply={sha256:revision,document:{version:1,id,name:'Companies',recipeVersion:1,recipe:{intent:'Keep original policy'},createdAt:'2026-10-14',updatedAt:'2026-10-14',imports:[],activity:[],records:[{id:rowId,name:'Company',sourceId:'original',row:1,class:'generated',status:'researched',values:{'size/~estimate':'50',active:'yes',evidence:'unknown'}}]}}
let saved:PreparationReply,packText:string,actions:DialectChatAction[],tools:ReturnType<typeof dialectChatTools>,proposal:DialectProposal
const signal=()=>new AbortController().signal
const call=(name:string,args:Record<string,unknown>,s=signal())=>tools.find(t=>t.name===name)!.execute(args,s)
const context=()=>call('read_dialect',{id,packKey:'fit'})
const propose=()=>call('propose_dialect',{id,revision,...proposal})
beforeEach(async()=>{
 vi.resetAllMocks();vi.mocked(checkDialect).mockResolvedValue({status:'complete',digest:'sha256:checked',diagnostics:[]});saved=structuredClone(base);packText=pack;actions=[];tools=dialectChatTools({afterTurnId:()=> 'turn',addAction:a=>actions.push(a)})
 proposal={packKey:'fit',packDigest:(await packBinding('fit',pack)).digest,facts:[{target:'/size',column:'size/~estimate',conversion:{kind:'decimal'}},{target:'/active',column:'active',conversion:{kind:'boolean',trueValue:'yes',falseValue:'no'}}],evidence:[{requirement:'source',column:'evidence'}],summary:'Use size as a decimal and explicit yes/no values; country remains unknown.'}
 vi.mocked(readFile).mockImplementation(async path=>({content:path==='jpack.json'?JSON.stringify({packs:{fit:{path:'fit.pack.json'}}}):packText}) as any)
 vi.mocked(dialectAPI).mockImplementation(async(_path,method)=>method==='POST'?{rows:[{id:rowId,prepared:{'/size':'50','/active':true},issues:[]}]} as any:structuredClone(saved) as any)
})
it('reads pack requirements and proposes a previewed dialect without saving or evaluating',async()=>{
 const read=await context(),body=JSON.parse((read as any).content[0].text)
 expect(body.facts.map((f:any)=>f.path)).toEqual(['/size','/active','/country'])
 expect(body.columns.find((c:any)=>c.name==='active').samples[0].origin).toBe('generated')
 expect(body.guidance).toContain('decimal strings')
 await propose();expect(actions).toHaveLength(1);expect(validDialectChatAction(JSON.parse(JSON.stringify(actions[0])))).toBe(true)
 expect(actions[0]).toMatchObject({kind:'update-dialect',afterTurnId:'turn',revision,inputId:id,dialect:proposal})
 const posts=vi.mocked(dialectAPI).mock.calls.filter(([,method])=>method)
 expect(posts).toHaveLength(1);expect(posts[0].slice(0,2)).toEqual(['/'+id+'/conversion-preview','POST'])
 const binding=(posts[0][2] as any).qualification
 expect(binding.mapping.case.facts[0]).toEqual({target:'/size',source:'/values/size~1~0estimate'})
 expect(binding.mapping.unmapped).toEqual(['/country']);expect(binding.conversions['/size'].kind).toBe('decimal')
 expect(saved).toEqual(base)
})
it('requires a fresh read and refuses stale collection or pack proposals',async()=>{
 await expect(propose()).rejects.toThrow('read_dialect')
 await context();saved.sha256='b'.repeat(64);await expect(propose()).rejects.toThrow('read_dialect')
 saved=structuredClone(base);await context();packText=pack.replace('1.0.0','1.0.1');await expect(propose()).rejects.toThrow('read_dialect')
 expect(actions).toHaveLength(0)
})
it('rejects guessed columns, unknown targets, duplicate bindings and injected authority',async()=>{
 await context()
 for(const replacement of [{...proposal,facts:[{...proposal.facts[0],column:'invented'}]},{...proposal,facts:[{...proposal.facts[0],target:'/invented'}]},{...proposal,facts:[proposal.facts[0],proposal.facts[0]]},{...proposal,evidence:[{requirement:'invented',column:'evidence'}]}])await expect(call('propose_dialect',{id,revision,...replacement})).rejects.toThrow()
 await expect(call('propose_dialect',{id,revision,...proposal,enable:true})).rejects.toThrow()
 expect(validDialectProposal({...proposal,facts:[{...proposal.facts[0],conversion:{kind:'decimal',script:'return 1'}}]})).toBe(false)
 expect(validDialectProposal({...proposal,facts:[{target:'/active',column:'active',conversion:{kind:'boolean',trueValue:'yes',falseValue:'yes'}}]})).toBe(false)
 expect(actions).toHaveLength(0)
})
it('keeps preview issues visible without inventing or approving facts',async()=>{
 vi.mocked(dialectAPI).mockImplementation(async(_path,method)=>method==='POST'?{rows:[{id:rowId,issues:[{target:'/size',message:'Not a decimal'}]}]} as any:saved as any)
 await context();const r=await propose();expect(JSON.parse((r as any).content[0].text)).toMatchObject({recordsWithIssues:1,issues:[{recordId:rowId,issues:[{target:'/size',message:'Not a decimal'}]}]})
 expect(actions).toHaveLength(1);expect(saved.document.records[0].review).toBeUndefined()
})
it('does not retain proposals after preview failure, cancellation or a concurrent change',async()=>{
 await context()
 vi.mocked(dialectAPI).mockImplementation(async(_path,method)=>{if(method==='POST')throw Error('Preview failed');return structuredClone(saved) as any})
 await expect(propose()).rejects.toThrow('Preview failed')
 vi.mocked(dialectAPI).mockImplementation(async(_path,method)=>{if(method==='POST'){saved.sha256='c'.repeat(64);return {rows:[]} as any}return structuredClone(saved) as any})
 await expect(propose()).rejects.toThrow('changed during preview')
 const controller=new AbortController();controller.abort();await expect(call('read_dialect',{id,packKey:'fit'},controller.signal)).rejects.toThrow()
 expect(actions).toHaveLength(0)
})
it('keeps planned research columns explicit without creating record values',async()=>{
 saved.document.recipe.research={version:1,target:'companies',fields:[{name:'country',type:'string',description:'Country'}],criteria:[],limits:{records:10,searches:10,pages:20,modelCalls:40},ai:{connection:'private',revision:'secret',model:'model'}}
 const r=await context(),body=JSON.parse((r as any).content[0].text)
 expect(body.columns.find((c:any)=>c.name==='country')).toEqual({name:'country',planned:true,samples:[]})
 expect(JSON.stringify(body)).not.toMatch(/private|secret/)
 const built=buildDialect(saved,{...proposal,facts:[{target:'/country',column:'country',conversion:{kind:'text'}}]},await packBinding('fit',pack))
 expect(built.mapping.case?.facts).toEqual([{target:'/country',source:'/values/country'}])
 expect(saved.document.records[0].values.country).toBeUndefined()
})

it('exposes host company lineage before people research without sample data',async()=>{
 saved.document.records=[]
 saved.document.recipe.research={version:1,target:'people-at-companies',fields:[],criteria:[],limits:{records:10,searches:0,pages:20,modelCalls:40},ai:{connection:'private',revision:'secret',model:'model'}}
 const r=await context(),body=JSON.parse((r as any).content[0].text)
 for(const name of ['company_id','company_name','company_url'])expect(body.columns.find((c:any)=>c.name===name)).toEqual({name,planned:true,samples:[]})
 expect(saved.document.records).toEqual([])
})
it('uses authoritative Runtime diagnostics before proposing and never saves a refused candidate',async()=>{
 await context()
 const report={status:'refused',diagnostics:[{code:'JPS-DIALECT-FORM',instancePath:'/case/facts',message:'Duplicate target.'}]}
 vi.mocked(checkDialect).mockRejectedValue(new DialectCheckError(report))
 await expect(propose()).rejects.toThrow('Duplicate target')
 expect(actions).toHaveLength(0)
 expect(vi.mocked(dialectAPI).mock.calls.filter(([,method])=>method==='POST')).toHaveLength(0)
})
