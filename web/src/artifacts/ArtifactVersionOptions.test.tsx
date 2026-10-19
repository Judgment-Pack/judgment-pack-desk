import {cleanup,fireEvent,render,screen,waitFor} from '@testing-library/react'
import {QueryClient,QueryClientProvider} from '@tanstack/react-query'
import {afterEach,beforeEach,expect,it,vi} from 'vitest'
import {ArtifactVersionOptions,readVersionOptions} from './ArtifactVersionOptions'
import {runtimeToolContextPolicy} from '@judgment-pack/harness/context/toolPolicy'
import {readCapabilities} from '../mcp/capabilities'
const {connection}=vi.hoisted(()=>({connection:{client:{callTool:vi.fn()},status:'ready',known:true,artifactVersionOptionsSupported:true,connectionEpoch:1}}))
vi.mock('../mcp/McpProvider',()=>({useMcp:()=>connection}))
const report=(base='0.1.0')=>({status:'available',scope:'version-arithmetic',base,compatibilityChecked:false,choices:{patch:'0.1.1',minor:'0.2.0',major:'1.0.0'}})
const clients:QueryClient[]=[]
beforeEach(()=>{connection.client.callTool.mockReset();connection.status='ready';connection.artifactVersionOptionsSupported=true;connection.client.callTool.mockResolvedValue({structuredContent:report()})})
afterEach(()=>{cleanup();clients.splice(0).forEach(c=>c.clear())})
function draw(base='0.1.0',select=vi.fn()){
 const client=new QueryClient({defaultOptions:{queries:{retry:false},mutations:{retry:false}}});clients.push(client)
 const view=render(<QueryClientProvider client={client}><ArtifactVersionOptions base={base} onSelect={select}/></QueryClientProvider>)
 return {...view,client,select,changeBase:(next:string)=>view.rerender(<QueryClientProvider client={client}><ArtifactVersionOptions base={next} onSelect={select}/></QueryClientProvider>)}
}
const open=()=>fireEvent.pointerDown(screen.getByRole('button',{name:'Choose next version'}),{button:0,ctrlKey:false,pointerType:'mouse'})
it('uses Runtime choices against the saved base and only changes the draft on selection',async()=>{
 const view=draw();expect(connection.client.callTool).not.toHaveBeenCalled();open()
 fireEvent.click(await screen.findByRole('menuitem',{name:/Patch 0.1.1/}))
 expect(view.select).toHaveBeenCalledExactlyOnceWith('0.1.1')
 expect(connection.client.callTool).toHaveBeenCalledWith({name:'get_artifact_version_options',arguments:{base:'0.1.0'}},undefined,expect.objectContaining({signal:expect.any(AbortSignal)}))
 open();fireEvent.click(await screen.findByRole('menuitem',{name:/Patch 0.1.1/}))
 expect(view.select.mock.calls).toEqual([['0.1.1'],['0.1.1']]);expect(connection.client.callTool).toHaveBeenCalledOnce()
})
it('does not invent choices when Runtime lacks the capability or is disconnected',()=>{
 connection.artifactVersionOptionsSupported=false;const view=draw();expect(screen.queryByRole('button')).toBeNull()
 connection.artifactVersionOptionsSupported=true;connection.status='reconnecting';view.changeBase('1.0.0');expect(screen.queryByRole('button')).toBeNull();expect(connection.client.callTool).not.toHaveBeenCalled()
})
it('refuses mismatched replies and never applies an option automatically',async()=>{
 connection.client.callTool.mockResolvedValue({structuredContent:report('2.0.0')});const view=draw();open()
 await screen.findByText('Version options unavailable');expect(view.select).not.toHaveBeenCalled();expect(screen.queryByRole('menuitem',{name:/Patch/})).toBeNull()
})
it('does not carry version choices across a changed saved base',async()=>{
 let finish:(v:unknown)=>void=()=>{};connection.client.callTool.mockImplementationOnce(()=>new Promise(resolve=>{finish=resolve})).mockResolvedValue({structuredContent:{...report('1.0.0'),choices:{patch:'1.0.1',minor:'1.1.0',major:'2.0.0'}}})
 const view=draw();open();await waitFor(()=>expect(connection.client.callTool).toHaveBeenCalledOnce());view.changeBase('1.0.0');finish({structuredContent:report()})
 fireEvent.click(await screen.findByRole('menuitem',{name:/Patch 1.0.1/}));expect(view.select).toHaveBeenCalledExactlyOnceWith('1.0.1')
})
it('keeps arithmetic separate from compatibility and publication',()=>{
 expect(readVersionOptions(report(),'0.1.0').major).toBe('1.0.0')
 for(const bad of [null,{}, {...report(),compatibilityChecked:true},{...report(),choices:{patch:'0.1.1'}},{...report(),scope:'publication-approved'}])expect(()=>readVersionOptions(bad,'0.1.0')).toThrow()
 expect(runtimeToolContextPolicy('get_artifact_version_options')).toEqual({immutable:true})
 expect(runtimeToolContextPolicy('read_pack')).toEqual({})
 expect(readCapabilities([{name:'get_artifact_version_options'}]).artifactVersionOptionsSupported).toBe(true)
 expect(readCapabilities([]).artifactVersionOptionsSupported).toBe(false)
})
