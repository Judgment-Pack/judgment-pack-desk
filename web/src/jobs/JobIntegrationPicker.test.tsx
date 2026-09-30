import {act,cleanup,fireEvent,render,screen,waitFor} from '@testing-library/react'
import {QueryClient,QueryClientProvider} from '@tanstack/react-query'
import {afterEach,beforeEach,expect,it,vi} from 'vitest'
import {JobIntegrationPicker} from './JobIntegrationPicker'
import type {ProfileEntry} from './mappingTypes'
const mock=vi.hoisted(()=>({catalog:vi.fn(),open:vi.fn(),close:vi.fn(),config:{} as any}))
vi.mock('../connections/catalog',()=>({useConnections:mock.catalog}))
vi.mock('../connections/ConnectionPaneContext',()=>({useConnectionsPane:()=>({open:mock.open,close:mock.close})}))
vi.mock('../config/DeskConfigProvider',()=>({useEffectiveConfig:()=>mock.config}))
vi.mock('./client',()=>({jobsAPI:vi.fn().mockResolvedValue({gatewayProfiles:['registry']})}))
const entry:ProfileEntry={digest:'digest',profile:{id:'registry',shape:'mcp',source:'registry',class:'record',authority:'gateway',publicKey:'key',adapter:{name:'mcp',version:'1',digest:'digest'},endpoint:null,tools:['read_case']}}
beforeEach(()=>{
 mock.config={desk:{localGateway:{status:'ready'}},config:{research:{gateway:{authority:'gateway',signer:{public:'key'}},documents:{enabled:true},managedLocal:true}}}
 mock.catalog.mockReturnValue({entries:[{descriptor:{id:'google-drive',registration:'google-desktop',operations:['connect'],selection:'source-search'},status:{data:{state:'not-connected'}}}],loading:false,isError:false,refetch:vi.fn()})
})
afterEach(()=>{cleanup();vi.clearAllMocks()})
function view(profiles=[entry],disabled=false){const onPick=vi.fn();render(<QueryClientProvider client={new QueryClient({defaultOptions:{queries:{retry:false}}})}><JobIntegrationPicker profiles={profiles} disabled={disabled} onPick={onPick}/></QueryClientProvider>);return onPick}
it('uses installed profiles and disables unsupported or mismatched authorities',async()=>{
 const onPick=view([entry,{...entry,profile:{...entry.profile,id:'http-only',shape:'http'}},{...entry,profile:{...entry.profile,id:'wrong-key',publicKey:'other'}}])
 fireEvent.click(screen.getByRole('button',{name:'Add source'}))
 expect((screen.getByRole('button',{name:/http-only/}) as HTMLButtonElement).disabled).toBe(true)
 expect((screen.getByRole('button',{name:/wrong-key/}) as HTMLButtonElement).disabled).toBe(true)
 await screen.findByText('Background access configured')
 fireEvent.click(screen.getByRole('button',{name:'registry'}))
 expect(onPick).toHaveBeenCalledWith(entry)
})
it('shares setup and returns without selecting an unsupported integration automatically',async()=>{
 mock.catalog.mockReturnValue({entries:[{descriptor:{id:'registry',source:{id:'registry',shape:'mcp'},operations:['connect']},status:{data:{state:'not-connected'}}}],loading:false,isError:false})
 const onPick=view()
 fireEvent.click(screen.getByRole('button',{name:'Add source'}))
 fireEvent.click(screen.getByRole('button',{name:'Add integration'}))
 await waitFor(()=>expect(mock.open).toHaveBeenCalled())
 const request=mock.open.mock.calls[0]![0]
 expect(request.purpose).toBe('job-source')
 expect(onPick).not.toHaveBeenCalled()
 act(()=>request.onConnected('google-drive'))
 await screen.findByRole('dialog',{name:'Choose integration'})
 expect(screen.getByRole('status').textContent).toContain('Integration connected')
 expect(onPick).not.toHaveBeenCalled()
 expect(mock.close).toHaveBeenCalledWith({restoreFocus:false})
})
it('does not offer setup when all catalog entries are blocked',()=>{
 mock.catalog.mockReturnValue({entries:[{descriptor:{id:'google-drive'},status:{data:{state:'blocked'}}}],loading:false,isError:false})
 view();fireEvent.click(screen.getByRole('button',{name:'Add source'}))
 expect((screen.getByRole('button',{name:'Add integration'}) as HTMLButtonElement).disabled).toBe(true)
})
it('blocks stale catalog actions on refresh failure while leaving local files usable',()=>{
 mock.catalog.mockReturnValue({entries:[],loading:false,isError:true,refetch:vi.fn()})
 const onPick=view();fireEvent.click(screen.getByRole('button',{name:'Add source'}))
 expect((screen.getByRole('button',{name:'registry'}) as HTMLButtonElement).disabled).toBe(true)
 fireEvent.click(screen.getByRole('button',{name:/Local JSON file/}))
 expect(onPick).toHaveBeenCalledWith(undefined)
})
