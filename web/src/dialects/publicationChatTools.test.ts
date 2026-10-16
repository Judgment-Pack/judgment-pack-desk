import {beforeEach,expect,it,vi} from 'vitest'
import {publicationChatTools} from './publicationChatTools'
import {planningTool} from '../assistant/planning/policy'
import {validDialectChatAction} from './chatActions'
import {publicationAction,publicationId as id,publicationRevision as revision} from '../testing/publication'
import {dialectAPI} from './client'
vi.mock('./client',async original=>({...await original<object>(),dialectAPI:vi.fn()}))
let retain:ReturnType<typeof vi.fn<(action:import('./chatActions').DialectChatAction,signal:AbortSignal)=>Promise<void>>>,tools:ReturnType<typeof publicationChatTools>
const call=(name:string,args:Record<string,unknown>)=>tools.find(t=>t.name===name)!.execute(args,new AbortController().signal)
const request={id,revision,variant:'asserted',version:'1.0.0',impact:{classification:'initial',rationale:'First reviewed mapping'}}
beforeEach(()=>{
 vi.resetAllMocks();retain=vi.fn(async()=>{});tools=publicationChatTools({retainAction:retain,afterTurnId:()=> 'turn'})
 vi.mocked(dialectAPI).mockImplementation(async(path,method)=>method==='POST'?publicationAction().publication!.answer as any:path?.includes('publications')?{items:[]} as any:{sha256:revision,document:{id,name:'Records',recipeVersion:1,recipe:{qualification:{mapping:{case:{facts:[]}}}}}} as any)
})
it('requires current read, checks through the canonical endpoint, and retains a recoverable proposal without publication',async()=>{
 await expect(call('propose_dialect_publication',request)).rejects.toThrow('Read the current')
 await call('read_dialect_publication',{id,variant:'asserted'})
 await call('propose_dialect_publication',request)
 const action=retain.mock.calls[0][0]
 expect(validDialectChatAction(JSON.parse(JSON.stringify(action)))).toBe(true)
 expect(action).toMatchObject({kind:'publish-dialect',afterTurnId:'turn',revision,publication:{version:'1.0.0'}})
 const writes=vi.mocked(dialectAPI).mock.calls.filter(([,method])=>method==='POST')
 expect(writes).toHaveLength(1);expect(writes[0][2]).toEqual({action:'check',...Object.fromEntries(Object.entries(request).filter(([k])=>k!=='id'))})
 expect(writes[0][2]).not.toHaveProperty('reviewed')
})
it('Plan exposes publication inspection but never proposals or writes',()=>{
 expect(tools.filter(planningTool).map(t=>t.name)).toEqual(['read_dialect_publication'])
 expect(tools.some(t=>/apply|grant|publish_version/.test(t.name))).toBe(false)
})
it('rejects stale and injected authority, and never retains incomplete checks',async()=>{
 await call('read_dialect_publication',{id,variant:'asserted'})
 await expect(call('propose_dialect_publication',{...request,reviewed:true})).rejects.toThrow()
 await expect(call('propose_dialect_publication',{...request,revision:'c'.repeat(64)})).rejects.toThrow()
 vi.mocked(dialectAPI).mockResolvedValueOnce({sha256:'b'.repeat(64)} as any)
 await expect(call('propose_dialect_publication',request)).rejects.toThrow('changed')
 vi.mocked(dialectAPI).mockResolvedValueOnce({sha256:revision,document:{}} as any).mockResolvedValueOnce({...publicationAction().publication!.answer,review:undefined} as any)
 await expect(call('propose_dialect_publication',request)).rejects.toThrow('incomplete')
 expect(retain).not.toHaveBeenCalled()
})
it('a checkpoint failure never publishes or returns a successful proposal',async()=>{
 await call('read_dialect_publication',{id,variant:'asserted'});retain.mockRejectedValue(Error('Storage unavailable'))
 await expect(call('propose_dialect_publication',request)).rejects.toThrow('Storage unavailable')
 expect(vi.mocked(dialectAPI).mock.calls.filter(([,method])=>method==='POST').map(c=>(c[2] as any).action)).toEqual(['check'])
})
