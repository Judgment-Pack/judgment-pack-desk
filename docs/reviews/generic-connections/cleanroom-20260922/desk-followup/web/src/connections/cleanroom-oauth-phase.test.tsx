import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { ConnectionsPane } from './ConnectionsPane'
import { ConnectionRequestError } from './client'
import type { ConnectionDescriptor } from './catalog'
import { genericConnection } from '../testing/genericConnection'

const mocks=vi.hoisted(()=>({call:vi.fn(),authorize:vi.fn(),catalog:vi.fn()}))
vi.mock('./client',async original=>({...await original<typeof import('./client')>(),connectionCall:mocks.call,authorizeDrive:mocks.authorize}))
vi.mock('./catalog',async original=>({...await original<typeof import('./catalog')>(),useConnections:mocks.catalog}))
vi.mock('../config/DeskConfigProvider',()=>({useEffectiveConfig:()=>({desk:{localGateway:{status:'ready'}},config:{research:{documents:{enabled:true},gateway:{url:'http://127.0.0.1:8888'}}}})}))
vi.mock('../chat/ChatProvider',()=>({useChats:()=>({store:{},chats:[{id:'retained-chat',attachments:[]}],drafts:[],bindings:new Map()})}))
vi.mock('../chat/useChatAttachments',()=>({useChatAttachments:()=>({reading:false,isReading:()=>false,cancel:vi.fn(),error:'',clearError:vi.fn()})}))

let state:string, descriptor:ConnectionDescriptor, client:QueryClient
beforeEach(()=>{
 state='not-connected'
 descriptor={...structuredClone(genericConnection),auth:'oauth',operations:[...genericConnection.operations,'connect','poll','cancel'],authorizationEndpoints:['https://auth.example.com/authorize']}
 client=new QueryClient({defaultOptions:{queries:{retry:false}}})
 mocks.catalog.mockImplementation(()=>({entries:[{descriptor,status:{data:{state,account:{id:'stable-account',name:'Test account'}}}}],loading:false,isError:false}))
 mocks.authorize.mockResolvedValue([])
 mocks.call.mockResolvedValue({saved:true})
})
afterEach(()=>{cleanup();vi.resetAllMocks()})
const view=()=> <QueryClientProvider client={client}><ConnectionsPane request={{provider:descriptor.id,chatId:'retained-chat',opener:null}} target={document.body} onProvider={()=>{}} onClose={()=>{}} onBusy={()=>{}} /></QueryClientProvider>
const fill=()=>{
 fireEvent.change(screen.getByLabelText('Folder'),{target:{value:'team-a'}})
 fireEvent.change(screen.getByLabelText('Access key'),{target:{value:'synthetic-registration-secret'}})
}

it('opens a previously configured OAuth provider directly into enabled consent',async()=>{
 render(view())
 expect(screen.queryByLabelText('Access key')).toBeNull()
 const consent=screen.getByRole('button',{name:'Continue with Fixture files'}) as HTMLButtonElement
 expect(consent.disabled).toBe(false)
 fireEvent.click(consent)
 await waitFor(()=>expect(mocks.authorize).toHaveBeenCalledWith('connect',expect.any(AbortSignal),descriptor.id,descriptor.authorizationEndpoints))
 expect(mocks.call).not.toHaveBeenCalled()
})

it('reconnects an OAuth form provider without demanding its saved registration again',async()=>{
 state='connected'
 mocks.call.mockRejectedValue(new ConnectionRequestError('reconnect-required',descriptor.id))
 render(view())
 const reconnect=await screen.findByRole('button',{name:'Reconnect'}) as HTMLButtonElement
 expect(screen.queryByLabelText('Access key')).toBeNull()
 expect(reconnect.disabled).toBe(false)
 expect(mocks.authorize).not.toHaveBeenCalled()
 fireEvent.click(reconnect)
 await waitFor(()=>expect(mocks.authorize).toHaveBeenCalledWith('connect',expect.any(AbortSignal),descriptor.id,descriptor.authorizationEndpoints))
 expect(mocks.call.mock.calls.some(call=>call[0]==='configure')).toBe(false)
})

it('returns to required configuration if an expired registration changes status to setup-required',async()=>{
 state='connected'
 mocks.call.mockRejectedValue(new ConnectionRequestError('registration-expired',descriptor.id))
 const ui=render(view())
 await screen.findByRole('button',{name:'Reconnect'})
 state='setup-required';ui.rerender(view())
 expect(screen.getByLabelText('Access key')).toBeTruthy()
 expect((screen.getByRole('button',{name:'Reconnect'}) as HTMLButtonElement).disabled).toBe(true)
 mocks.call.mockResolvedValue({saved:true})
 fill()
 fireEvent.click(screen.getByRole('button',{name:'Reconnect'}))
 await waitFor(()=>expect(mocks.call).toHaveBeenCalledWith('configure',{folder:'team-a',key:'synthetic-registration-secret'},expect.any(AbortSignal),descriptor.id))
 expect(mocks.authorize).not.toHaveBeenCalled()
 await waitFor(()=>expect((screen.getByLabelText('Access key') as HTMLInputElement).value).toBe(''))
 state='not-connected';ui.rerender(view())
 const consent=screen.getByRole('button',{name:'Continue with Fixture files'}) as HTMLButtonElement
 expect(consent.disabled).toBe(false)
 fireEvent.click(consent)
 await waitFor(()=>expect(mocks.authorize).toHaveBeenCalledTimes(1))
})

it.each(['credentials','oauth'] as const)('keeps required %s form fields enforced while configuring',async auth=>{
 descriptor={...descriptor,auth}
 state=auth==='oauth'?'setup-required':'not-connected'
 render(view())
 const action=screen.getByRole('button',{name:auth==='oauth'?'Save':'Connect'}) as HTMLButtonElement
 expect(action.disabled).toBe(true)
 fireEvent.click(action)
 expect(mocks.call).not.toHaveBeenCalled()
 fireEvent.change(screen.getByLabelText('Folder'),{target:{value:'team-a'}})
 expect(action.disabled).toBe(true)
 fill();expect(action.disabled).toBe(false)
 fireEvent.click(action)
 await waitFor(()=>expect(mocks.call).toHaveBeenCalledWith('configure',{folder:'team-a',key:'synthetic-registration-secret'},expect.any(AbortSignal),descriptor.id))
 expect(mocks.authorize).not.toHaveBeenCalled()
})
