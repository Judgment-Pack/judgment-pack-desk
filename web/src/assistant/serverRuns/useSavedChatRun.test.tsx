import { act, cleanup, renderHook, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { ChatStore, type Chat } from '../../chat/store'
import { memoryDraftPersistence } from '../../testing/draftPersistence'
import { INITIAL_STATE } from '../../research/initialState'
import { giveThisPageASessionForTesting, forgetSession } from '../../mcp/session'
import { useSavedChatRun } from './useSavedChatRun'
import pending from './fixtures/pending.json'
vi.mock('../useAssistantSlot',()=>({useAssistantSlot:()=>({}),assistantReady:()=>true}))
vi.mock('../target',()=>({selectedAssistant:()=>({model:'model-one',models:['model-one']})}))
vi.mock('../../config/DeskConfigProvider',()=>({useEffectiveConfig:()=>({config:{research:{gateway:null}}})}))
const stores:ChatStore[]=[]
const chat:Chat={id:'chat-one',title:'Chat',pinned:false,archived:false,updatedAt:'2026-10-20T00:00:00Z',composer:'',model:'model-one',mode:'draft',view:'chat',serverRequest:{requestId:'message-one',admissionId:'admission-one'},checkpoint:{state:{...INITIAL_STATE,turns:[{id:'message-one',role:'user',kind:'message',text:'Original request',at:'2026-10-20T00:00:00Z'}]},sources:[]}}
function store(project='/project'){const s=new ChatStore(project,{read:async()=>({project,sha256:'absent',content:{version:1,chats:[chat]}}),write:vi.fn()},memoryDraftPersistence(project));stores.push(s);return s}
beforeEach(()=>giveThisPageASessionForTesting('hook-session'))
afterEach(()=>{cleanup();stores.forEach(s=>s.dispose());stores.length=0;vi.unstubAllGlobals()})
it('unmount unsubscribes without cancel/start and logout clears the displayed transcript',async()=>{
 const fetch=vi.fn().mockImplementation(()=>Promise.resolve(new Response(JSON.stringify({runs:[{...pending,state:'awaiting-input'}]}),{headers:{'Content-Type':'application/json'}})));vi.stubGlobal('fetch',fetch)
 const s=store();await s.load();const h=renderHook(()=>useSavedChatRun(s,chat))
 await waitFor(()=>expect(h.result.current.state.status).toBe('needs-input'))
 expect(h.result.current.state.turns[0].text).toBe('Original request')
 act(()=>forgetSession());expect(h.result.current.state.turns).toEqual([])
 h.unmount();expect(fetch).toHaveBeenCalledOnce();expect(fetch.mock.calls[0][0]).toContain('/owners/chat-one/requests/message-one')
})
it('late observations from the previous project cannot update a replacement owner',async()=>{
 let release!:(value:Response)=>void
 const next={...chat,id:'chat-two',serverRequest:{requestId:'request-two',admissionId:'admission-two'},checkpoint:undefined}
 const fetch=vi.fn().mockImplementationOnce(()=>new Promise<Response>(r=>{release=r})).mockResolvedValueOnce(new Response(JSON.stringify({runs:[{...pending,state:'failed',owner:{...pending.owner,id:next.id},requestId:'request-two',admissionId:'admission-two'}]}),{headers:{'Content-Type':'application/json'}}));vi.stubGlobal('fetch',fetch)
 const first=store(),second=store('/other');await first.load();await second.load()
 const h=renderHook(({s,c})=>useSavedChatRun(s,c),{initialProps:{s:first,c:chat}})
 await waitFor(()=>expect(fetch).toHaveBeenCalledOnce())
 h.rerender({s:second,c:next})
 await waitFor(()=>expect(h.result.current.state.status).toBe('failed'))
 await act(async()=>{release(new Response(JSON.stringify({runs:[{...pending,state:'failed'}]}),{headers:{'Content-Type':'application/json'}}));await Promise.resolve()})
 expect(h.result.current.state.turns).toEqual([]);expect(fetch).toHaveBeenCalledTimes(2)
})
