import {act,cleanup,renderHook,waitFor} from '@testing-library/react'
import {QueryClient,QueryClientProvider} from '@tanstack/react-query'
import type {ReactNode} from 'react'
import {afterEach,beforeEach,expect,it,vi} from 'vitest'
import {useDialectDraft} from './useDialectDraft'
import {dialectDraftAPI,type DialectDraftReply,type DialectDraftValues} from './drafts'
import {UnsavedChangesProvider,useDeskChangeState} from '../shell/UnsavedChanges'
vi.mock('./drafts',async original=>({...await original<typeof import('./drafts')>(),dialectDraftAPI:vi.fn()}))
const initial:DialectDraftReply={sha256:'original',document:{version:1,id:'draft_one',name:'Original',recipe:{intent:'Find records'},createdAt:'2026-10-15',updatedAt:'2026-10-15'}}
const reply=(value:DialectDraftValues,sha256='next'):DialectDraftReply=>({sha256,document:{...initial.document,...value}})
beforeEach(()=>{vi.mocked(dialectDraftAPI).mockReset()})
afterEach(cleanup)
function show(){const client=new QueryClient();return renderHook(()=>({editor:useDialectDraft(initial),status:useDeskChangeState()}),{wrapper:({children}:{children:ReactNode})=><QueryClientProvider client={client}><UnsavedChangesProvider>{children}</UnsavedChangesProvider></QueryClientProvider>})}
it('autosaves incomplete fields without marking normal persistence dirty',async()=>{
 vi.mocked(dialectDraftAPI).mockImplementation(async(_path,_method,body)=>reply(body as DialectDraftValues) as never)
 const hook=show();act(()=>hook.result.current.editor.setValue({name:'',recipe:{intent:'Unfinished'}}))
 expect(hook.result.current.status.pending).toBe(true);expect(hook.result.current.status.dirty).toBe(false)
 await waitFor(()=>expect(hook.result.current.editor.dirty).toBe(false))
 expect(dialectDraftAPI).toHaveBeenCalledWith('/draft_one','PUT',{name:'',recipe:{intent:'Unfinished'}},'original')
 await waitFor(()=>expect(hook.result.current.status.pending).toBe(false))
})
it('serializes writes and retains edits made while the first save is in flight',async()=>{
 let finish!:(value:unknown)=>void
 vi.mocked(dialectDraftAPI).mockImplementationOnce(()=>new Promise(resolve=>{finish=resolve})).mockImplementation(async(_path,_method,body)=>reply(body as DialectDraftValues,'second') as never)
 const hook=show();act(()=>hook.result.current.editor.setValue({name:'First',recipe:{intent:'First'}}))
 let saving!:Promise<DialectDraftReply>;act(()=>{saving=hook.result.current.editor.flush()})
 act(()=>hook.result.current.editor.setValue({name:'Second',recipe:{intent:'Second'}}))
 expect(dialectDraftAPI).toHaveBeenCalledTimes(1)
 await act(async()=>{finish(reply({name:'First',recipe:{intent:'First'}},'first'));await saving})
 expect(vi.mocked(dialectDraftAPI).mock.calls[1]).toEqual(['/draft_one','PUT',{name:'Second',recipe:{intent:'Second'}},'first'])
 expect(hook.result.current.editor.value.name).toBe('Second');expect(hook.result.current.editor.dirty).toBe(false)
})
it('retains local edits on a stale write and reloads only on explicit request',async()=>{
 vi.mocked(dialectDraftAPI).mockRejectedValue(Error('The dialect draft changed.'))
 const hook=show();act(()=>hook.result.current.editor.setValue({name:'My edits',recipe:{intent:'Keep these'}}))
 await act(async()=>{await hook.result.current.editor.flush().catch(()=>{})})
 expect(hook.result.current.editor.value.name).toBe('My edits');expect(hook.result.current.status.dirty).toBe(true)
 await new Promise(resolve=>setTimeout(resolve,600));expect(dialectDraftAPI).toHaveBeenCalledTimes(1)
 vi.mocked(dialectDraftAPI).mockResolvedValue(reply({name:'Other window',recipe:{intent:'Current saved'}},'concurrent') as never)
 await act(async()=>{await hook.result.current.editor.reload()})
 expect(hook.result.current.editor.value.name).toBe('Other window');expect(hook.result.current.status.dirty).toBe(false)
})
it('refreshes a clean draft but never replaces pending edits with a background snapshot',()=>{
 const client=new QueryClient()
 const hook=renderHook(({saved})=>useDialectDraft(saved),{initialProps:{saved:initial},wrapper:({children}:{children:ReactNode})=><QueryClientProvider client={client}>{children}</QueryClientProvider>})
 const newer=reply({name:'Updated elsewhere',recipe:{intent:'Latest'}},'newer')
 hook.rerender({saved:newer});expect(hook.result.current.value.name).toBe('Updated elsewhere')
 act(()=>hook.result.current.setValue({name:'My pending change',recipe:{intent:'Keep'}}))
 hook.rerender({saved:reply({name:'Another change',recipe:{intent:'New'}},'latest')})
 expect(hook.result.current.value.name).toBe('My pending change')
})

it('finishes one write when the server reorders and normalizes its saved JSON',async()=>{
 let calls=0
 vi.mocked(dialectDraftAPI).mockImplementation(async(_path,_method,body)=>{
  if(++calls>1)throw Error('Repeated the acknowledged write')
  const submitted=body as DialectDraftValues
  return reply({...submitted,recipe:{intent:submitted.recipe.intent,template:submitted.recipe.template}}) as never
 })
 const hook=show()
 act(()=>hook.result.current.editor.setValue({name:'Edited',recipe:{template:{id:'fixture',name:'Fixture',license:'Apache-2.0',digest:'sha256:'+'a'.repeat(64)},intent:'New instructions'}}))
 await act(async()=>{await hook.result.current.editor.flush()})
 expect(dialectDraftAPI).toHaveBeenCalledTimes(1)
 expect(hook.result.current.editor.dirty).toBe(false)
 expect(hook.result.current.editor.busy).toBe(false)
 expect(hook.result.current.editor.value.recipe.intent).toBe('New instructions')
})
