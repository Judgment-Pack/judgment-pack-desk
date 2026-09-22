import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import { ConnectionsPane } from './ConnectionsPane'
import { genericConnection } from '../testing/genericConnection'
import { parseConnectionCatalog } from './catalog'

const mocks = vi.hoisted(() => ({call:vi.fn(),authorize:vi.fn(),catalog:vi.fn()}))
vi.mock('./client',async original=>({...await original<typeof import('./client')>(),connectionCall:mocks.call,authorizeDrive:mocks.authorize}))
vi.mock('./catalog',async original=>({...await original<typeof import('./catalog')>(),useConnections:mocks.catalog}))
vi.mock('../config/DeskConfigProvider',()=>({useEffectiveConfig:()=>({desk:{localGateway:{status:'ready'}},config:{research:{documents:{enabled:true},gateway:{url:'http://127.0.0.1:8888'}}}})}))
vi.mock('../chat/ChatProvider',()=>({useChats:()=>({store:{},chats:[],drafts:[],bindings:new Map()})}))
vi.mock('../chat/useChatAttachments',()=>({useChatAttachments:()=>({reading:false,isReading:()=>false,cancel:vi.fn(),error:'',clearError:vi.fn()})}))
afterEach(()=>{cleanup();vi.resetAllMocks()})

it('allows OAuth sign-in after successfully saving required form registration',async()=>{
 const descriptor = {...genericConnection,auth:'oauth' as const,operations:[...genericConnection.operations,'connect','poll','cancel'],authorizationEndpoints:['https://auth.example.com/authorize']}
 expect(parseConnectionCatalog({version:3,sources:[],providers:[descriptor]}).providers).toHaveLength(1)
 let state = 'setup-required'
 mocks.catalog.mockImplementation(()=>({entries:[{descriptor,status:{data:{state}}}],loading:false,isError:false}))
 mocks.call.mockResolvedValue({saved:true})
 const client=new QueryClient({defaultOptions:{queries:{retry:false}}})
 const view=()=> <QueryClientProvider client={client}><ConnectionsPane request={{provider:'fixture-files',opener:null}} target={document.body} onProvider={()=>{}} onClose={()=>{}} onBusy={()=>{}} /></QueryClientProvider>
 const ui=render(view())
 fireEvent.change(screen.getByLabelText('Folder'),{target:{value:'team-a'}})
 fireEvent.change(screen.getByLabelText('Access key'),{target:{value:'synthetic-client-secret'}})
 fireEvent.click(screen.getByRole('button',{name:'Save'}))
 await waitFor(()=>expect(mocks.call).toHaveBeenCalledWith('configure',{folder:'team-a',key:'synthetic-client-secret'},expect.any(AbortSignal),'fixture-files'))
 await waitFor(()=>expect((screen.getByLabelText('Access key') as HTMLInputElement).value).toBe(''))
 state='not-connected';ui.rerender(view())
 expect(screen.queryByLabelText('Access key')).toBeNull()
 const button=screen.getByRole('button',{name:'Continue with Fixture files'}) as HTMLButtonElement
 expect(button.disabled).toBe(false)
 fireEvent.click(button)
 await waitFor(()=>expect(mocks.authorize).toHaveBeenCalledWith('connect',expect.any(AbortSignal),'fixture-files',descriptor.authorizationEndpoints))
})
