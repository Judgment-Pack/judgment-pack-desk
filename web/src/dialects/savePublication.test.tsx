import {cleanup,renderHook} from '@testing-library/react'
import {afterEach,beforeEach,expect,it,vi} from 'vitest'
import {ChatStore,type ChatPersistence} from '../chat/store'
import {INITIAL_STATE} from '../research/run'
import {memoryDraftPersistence} from '../testing/draftPersistence'
import {publicationAction,publishedAnswer} from '../testing/publication'
import {savePublication,retainPublishedAction} from './savePublication'
import {readPublicationResult} from './publicationProposal'
import {usePublishDialectEditing} from './recipeEditing'
import {dialectAPI} from './client'
import {actionApprovalAPI} from '../assistant/approvals/client'
vi.mock('./client',async original=>({...await original<object>(),dialectAPI:vi.fn()}))
vi.mock('../assistant/approvals/client',async original=>({...await original<object>(),actionApprovalAPI:vi.fn()}))
let store:ChatStore,chatId:string,write:ReturnType<typeof vi.fn>,content:unknown,action:ReturnType<typeof publicationAction>,saved:ReturnType<typeof publishedAnswer>|null
const io=():ChatPersistence=>({read:async()=>({project:'/project',sha256:'saved',content:content as any}),write:write as any})
beforeEach(async()=>{
 vi.resetAllMocks();action=publicationAction();saved=null;content={version:1,chats:[]}
 write=vi.fn(async(doc:any)=>{content=JSON.parse(JSON.stringify(doc));return {project:'/project',sha256:'saved',content}})
 store=new ChatStore('/project',io(),memoryDraftPersistence('/project'));await store.load();const chat=store.create();chatId=chat.id
 store.update(chatId,{inputActions:[action],interaction:'work',checkpoint:{state:{...INITIAL_STATE,status:'complete'},sources:[]}})
 vi.mocked(dialectAPI).mockImplementation(async()=>({result:saved}) as any)
 vi.mocked(actionApprovalAPI).mockImplementation(async()=>{saved=publishedAnswer(action);return saved as any})
})
afterEach(()=>{cleanup();store.dispose()})
it.each([undefined,'grant-one'])('checkpoints, dispatches %s once, and reads back exact bytes before marking applied',async grantId=>{
 await savePublication({store,chatId,action,grantId})
 expect(write.mock.invocationCallOrder[0]).toBeLessThan(vi.mocked(actionApprovalAPI).mock.invocationCallOrder[0])
 expect(actionApprovalAPI).toHaveBeenCalledWith(chatId,grantId?'commit':'apply',expect.objectContaining({action:'dialect-publish',target:action.id,revision:action.revision,reviewed:true,...(grantId?{id:grantId}:{})}))
 expect(store.getSnapshot().chats[0].inputActions?.[0].applied).toBeTruthy()
 const reloaded=new ChatStore('/project',io(),memoryDraftPersistence('/project'));await reloaded.load()
 expect(reloaded.getSnapshot().error).toBe('');expect(reloaded.getSnapshot().chats[0].inputActions?.[0]).toMatchObject({publication:action.publication,applied:{at:expect.any(String)}});reloaded.dispose()
})
it('retains review across refresh and reconciles a completed receipt without a second write',async()=>{
 await store.checkpoint(new AbortController().signal);store.dispose();store=new ChatStore('/project',io(),memoryDraftPersistence('/project'));await store.load()
 expect(store.getSnapshot().chats[0].inputActions?.[0]).toEqual(action)
 saved=publishedAnswer(action);await savePublication({store,chatId,action})
 expect(actionApprovalAPI).not.toHaveBeenCalled();expect(store.getSnapshot().chats[0].inputActions?.[0].applied).toBeTruthy()
})
it.each(['plan','question','running','changed','dirty'])('blocks %s at the common save boundary',async kind=>{
 if(kind==='plan')store.update(chatId,{interaction:'plan'})
 if(kind==='question')store.update(chatId,{checkpoint:{state:{...INITIAL_STATE,status:'complete',unknowns:['policy']},sources:[]}})
 if(kind==='running')store.update(chatId,{checkpoint:{state:{...INITIAL_STATE,status:'running'},sources:[]}})
 if(kind==='changed')store.update(chatId,{inputActions:[{...action,name:'Changed'}]})
 if(kind==='dirty')renderHook(()=>usePublishDialectEditing(action.inputId,true))
 await expect(savePublication({store,chatId,action})).rejects.toThrow()
 expect(actionApprovalAPI).not.toHaveBeenCalled()
})
it('rechecks phase after a checkpoint and before dispatch',async()=>{
 write.mockImplementation(async(doc:any)=>{store.update(chatId,{interaction:'plan'});return {project:'/project',sha256:'saved',content:doc}})
 await expect(savePublication({store,chatId,action})).rejects.toThrow()
 expect(actionApprovalAPI).not.toHaveBeenCalled()
})
it('does not retry an uncertain write or mark an unverified reply applied',async()=>{
 vi.mocked(actionApprovalAPI).mockRejectedValue(Error('Disconnected'))
 await expect(savePublication({store,chatId,action,grantId:'one'})).rejects.toThrow('Disconnected')
 expect(actionApprovalAPI).toHaveBeenCalledTimes(1);expect(store.getSnapshot().chats[0].inputActions?.[0].applied).toBeUndefined()
 vi.mocked(actionApprovalAPI).mockResolvedValue(publishedAnswer(action))
 await expect(savePublication({store,chatId,action})).rejects.toThrow('could not be verified')
 expect(store.getSnapshot().chats[0].inputActions?.[0].applied).toBeUndefined()
})
it('rejects a mismatching receipt; recovery itself works in Plan without a mutable recipe read',async()=>{
 saved={...publishedAnswer(action),document:'wrong'};await expect(readPublicationResult(action)).rejects.toThrow('does not match')
 saved=publishedAnswer(action);store.update(chatId,{interaction:'plan'})
 expect(await readPublicationResult(action)).toEqual(saved);await retainPublishedAction(store,chatId,action)
 expect(actionApprovalAPI).not.toHaveBeenCalled();expect(vi.mocked(dialectAPI).mock.calls.every(([path,method])=>path?.includes('?request=')&&method==='GET')).toBe(true)
})
