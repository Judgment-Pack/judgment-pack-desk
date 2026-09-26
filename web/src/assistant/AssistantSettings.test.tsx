import { QueryClientProvider } from '@tanstack/react-query'
import { act, cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import { DeskConfigFixture } from '../config/DeskConfigProvider'
import { decodeDeskConfig, effectiveConfig } from '../config/deskConfig'
import { testQueryClient } from '../testing/harness'
import { AssistantSettings } from './AssistantSettings'
import { assistantReady, useAssistantSlot } from './useAssistantSlot'

const agent={provider:'openai',authMethod:'subscription',model:'model',tools:['get_schema','validate']}
const endpoint={url:'https://api.example.invalid/v1',kind:'openai-compatible',model:'api-model',models:['api-model'],tools:['validate']}
const digest='a'.repeat(64)
afterEach(()=>{cleanup();vi.unstubAllGlobals()})
function setup(options:{connected?:boolean;enabled?:boolean;modelError?:boolean;model?:string|null;slotOnly?:boolean;tools?:string[];runtimeMissing?:boolean;holdLogin?:boolean;failLoginOnce?:boolean;alreadyConnected?:boolean}={}) {
  let connected=options.connected??true
  let pending=false
  let prepared=!options.runtimeMissing, loginFailed=false
  let releaseLogin:()=>void=()=>{}
  const calls:{path:string;method:string;body:unknown}[]=[]
  const client=testQueryClient()
  const config=effectiveConfig(undefined,undefined,undefined,{path:'/private/desk.json',present:true,sha256:digest,decoded:decodeDeskConfig(JSON.stringify({deskConfigVersion:1,assistant:{engine:'codex',endpoint,thinking:'off',agent:{...agent,tools:options.tools??agent.tools,model:options.model===undefined?'model':options.model}}}),'desk')})
  vi.stubGlobal('fetch',async(input:string,init:RequestInit={})=>{
    const path=new URL(input,'http://localhost').pathname, method=init.method??'GET', body=typeof init.body==='string'?JSON.parse(init.body):null
    calls.push({path,method,body})
    let value:unknown={},status=200
    if(path==='/api/model-providers')value={providers:[{id:'openai',authMethod:'subscription',agent:'codex',configured:options.enabled!==false,enabled:options.enabled!==false,engineReady:options.enabled!==false,requiredVersion:'codex-cli 0.156.0',availability:options.enabled===false?'disabled':'available',loginMethods:['browser','device']}]}
    else if(path.endsWith('/status'))value={provider:'openai',authMethod:'subscription',agent:'codex',runtime:prepared?'available':'not-installed',account:connected?'connected':pending?'login-pending':'signed-out',...(pending?{login:{id:'attempt',state:'pending',expiresAt:new Date(Date.now()+60_000).toISOString()}}:{})}
    else if(path.endsWith('/models')) {value=options.modelError?{error:'provider-unavailable'}:{models:[{id:'model',name:'Account model',efforts:['medium','high'],defaultEffort:'medium'}]};if(options.modelError)status=503}
    else if(path.endsWith('/login')){
      if(options.holdLogin)await new Promise<void>((resolve,reject)=>{releaseLogin=resolve;init.signal?.addEventListener('abort',()=>reject(new DOMException('Canceled','AbortError')),{once:true})})
      if(options.failLoginOnce&&!loginFailed){loginFailed=true;return new Response(JSON.stringify({error:'runtime-install-failed'}),{status:503})}
      if(options.alreadyConnected){prepared=true;connected=true;return new Response(JSON.stringify({error:'already-connected'}),{status:409})}
      prepared=true;pending=true;value={id:'attempt',method:'browser',url:'https://auth.openai.com/oauth/authorize?state=TRANSIENT_CHALLENGE',expiresAt:new Date(Date.now()+60_000).toISOString()}}
    else if(path.endsWith('/cancel'))pending=false
    else if(path.endsWith('/logout')){connected=false;pending=false}
    else if(path==='/api/assistant/key')value={present:false,fingerprint:'',origin:'',kind:'',bound:false,configuredOrigin:'',configuredKind:''}
    else if(path==='/api/desk-config'&&method==='PUT')value={assistant:body.assistant,path:'/private/desk.json',sha256:'b'.repeat(64),created:false,keyRebindRequired:false}
    else if(path==='/api/desk-config')value={present:true,path:'/private/desk.json',sha256:digest,content:JSON.stringify({deskConfigVersion:1,assistant:config.config.assistant})}
    return new Response(JSON.stringify(value),{status,headers:{'Content-Type':'application/json'}})
  })
  function Reading(){const slot=useAssistantSlot();return <output>{assistantReady(slot)?'ready':slot.unusable??'not ready'}</output>}
  const view=render(<QueryClientProvider client={client}><DeskConfigFixture value={config}>{options.slotOnly?<Reading/>:<AssistantSettings unavailable={false}/>}</DeskConfigFixture></QueryClientProvider>)
  return {calls,client,view,finishPreparation:()=>releaseLogin(),connect:()=>{connected=true;pending=false}}
}
async function pick(label:string,option:string){const trigger=screen.getByRole('combobox',{name:label}) as HTMLButtonElement;await waitFor(()=>expect(trigger.disabled).toBe(false));fireEvent.click(trigger);fireEvent.click(await screen.findByRole('option',{name:option}))}

it('explains an administrator-disabled connection without executable setup instructions',async()=>{
 const {calls}=setup({enabled:false})
 await screen.findByText(/ChatGPT subscriptions are disabled by this installation/)
 expect(screen.queryByRole('button',{name:'Connect ChatGPT'})).toBeNull()
 expect(screen.getByLabelText('API key').closest('[hidden]')).not.toBeNull()
 expect(calls.some(c=>c.path.endsWith('/login'))).toBe(false)
})
it('saves an explicit subscription selection and retains the inactive API target without a key',async()=>{
 const {calls}=setup()
 await screen.findByText('Connected to ChatGPT')
 await screen.findByRole('combobox',{name:'Reasoning'})
 await pick('Reasoning','high')
 fireEvent.click(screen.getByRole('button',{name:'Save selection'}))
 await waitFor(()=>expect(calls.some(c=>c.method==='PUT')).toBe(true))
 const write=calls.find(c=>c.method==='PUT')!.body
 expect(write).toEqual({ifMatch:digest,assistant:{engine:'codex',endpoint,thinking:'off',agent:{...agent,effort:'high'}}})
 expect(calls.some(c=>c.path.startsWith('/api/model/'))).toBe(false)
 expect(screen.getByLabelText('API key').closest('[hidden]')).not.toBeNull()
})
it('keeps a login challenge out of the query cache and cancels the owning attempt',async()=>{
 const {calls,client}=setup({connected:false,model:null})
 const connect=await screen.findByRole('button',{name:'Connect ChatGPT'})
 await waitFor(()=>expect((connect as HTMLButtonElement).disabled).toBe(false));fireEvent.click(connect)
 const link=await screen.findByRole('link',{name:'Continue sign-in in your browser'})
 expect(link.getAttribute('href')).toContain('TRANSIENT_CHALLENGE')
 expect(JSON.stringify(client.getQueryCache().getAll().map(q=>q.state.data))).not.toContain('TRANSIENT_CHALLENGE')
 expect(calls.find(c=>c.path.endsWith('/login'))?.body).toEqual({method:'browser'})
 fireEvent.click(screen.getByRole('button',{name:'Cancel sign-in'}))
 await waitFor(()=>expect(screen.queryByRole('link',{name:'Continue sign-in in your browser'})).toBeNull())
 expect(calls.find(c=>c.path.endsWith('/cancel'))?.body).toEqual({id:'attempt'})
})
it('loads account models after sign-in and removes the completed challenge',async()=>{
 const state=setup({connected:false,model:null})
 const connect=await screen.findByRole('button',{name:'Connect ChatGPT'})
 await waitFor(()=>expect((connect as HTMLButtonElement).disabled).toBe(false));fireEvent.click(connect)
 await screen.findByRole('link',{name:'Continue sign-in in your browser'})
 state.connect();await act(async()=>{await state.client.invalidateQueries({queryKey:['model-provider']})})
 await screen.findByText('Connected to ChatGPT')
 await pick('Model','Account model')
 expect(screen.queryByRole('link',{name:'Continue sign-in in your browser'})).toBeNull()
 expect((screen.getByRole('button',{name:'Test connection'}) as HTMLButtonElement).disabled).toBe(true)
})
it('requires the styled confirmation before disconnecting and clears available models',async()=>{
 const {calls}=setup()
 await screen.findByText('Connected to ChatGPT')
 fireEvent.click(screen.getByRole('button',{name:'Disconnect'}))
 const dialog=screen.getByRole('dialog',{name:'Disconnect ChatGPT?'})
 expect(calls.some(c=>c.path.endsWith('/logout'))).toBe(false)
 fireEvent.click(within(dialog).getByRole('button',{name:'Disconnect'}))
 await waitFor(()=>expect(screen.queryByRole('dialog')).toBeNull())
 expect(calls.filter(c=>c.path.endsWith('/logout'))).toHaveLength(1)
 expect(calls.some(c=>c.method==='DELETE')).toBe(false)
})
it('refuses a stale model selection when discovery fails',async()=>{
 setup({modelError:true})
 await screen.findByText('This model is no longer available. Choose another model.')
 expect((screen.getByRole('button',{name:'Save selection'}) as HTMLButtonElement).disabled).toBe(true)
 expect((screen.getByRole('button',{name:'Test connection'}) as HTMLButtonElement).disabled).toBe(true)
})
it('makes subscription readiness independent of the saved API key',async()=>{
 const {calls}=setup({slotOnly:true})
 await waitFor(()=>expect(screen.getByRole('status').textContent).toBe('ready'))
 expect(calls.some(c=>c.path==='/api/assistant/key')).toBe(false)
})

it('does not grant an excluded tool for the connection test',async()=>{
 setup({tools:[]})
 await screen.findByText('Connected to ChatGPT')
 await screen.findByRole('combobox',{name:'Reasoning'})
 expect((screen.getByRole('button',{name:'Test connection'}) as HTMLButtonElement).disabled).toBe(true)
 expect(screen.getByText('Enable and save get_schema in Allowed pack tools to test the connection.')).toBeTruthy()
})

it('prepares on Connect, shows progress, and continues to sign-in without restarting',async()=>{
 const state=setup({connected:false,model:null,runtimeMissing:true,holdLogin:true})
 const connect=await screen.findByRole('button',{name:'Connect ChatGPT'})
 await waitFor(()=>expect((connect as HTMLButtonElement).disabled).toBe(false))
 expect(state.calls.some(c=>c.path.endsWith('/login'))).toBe(false)
 expect(screen.queryByText(/--codex/)).toBeNull()
 fireEvent.click(connect)
 const progress=await screen.findByText('Preparing ChatGPT…')
 expect(progress.closest('[role="status"]')?.getAttribute('data-running')).toBe('true')
 expect(screen.getByRole('button',{name:'Cancel'})).toBeTruthy()
 await act(async()=>state.finishPreparation())
 await screen.findByRole('link',{name:'Continue sign-in in your browser'})
 expect(state.calls.filter(c=>c.path.endsWith('/login'))).toHaveLength(1)
 expect(screen.queryByText('Preparing ChatGPT…')).toBeNull()
})
it('allows canceling preparation without leaving an error or a sign-in link',async()=>{
 const state=setup({connected:false,model:null,runtimeMissing:true,holdLogin:true})
 const connect=await screen.findByRole('button',{name:'Connect ChatGPT'})
 await waitFor(()=>expect((connect as HTMLButtonElement).disabled).toBe(false));fireEvent.click(connect)
 await screen.findByText('Preparing ChatGPT…');fireEvent.click(screen.getByRole('button',{name:'Cancel'}))
 await screen.findByRole('button',{name:'Connect ChatGPT'})
 await act(async()=>state.finishPreparation())
 expect(screen.queryByRole('link',{name:'Continue sign-in in your browser'})).toBeNull()
 expect(screen.queryByRole('alert')).toBeNull()
})
it('explains a preparation failure and lets Connect retry in place',async()=>{
 setup({connected:false,model:null,runtimeMissing:true,failLoginOnce:true})
 const connect=await screen.findByRole('button',{name:'Connect ChatGPT'})
 await waitFor(()=>expect((connect as HTMLButtonElement).disabled).toBe(false));fireEvent.click(connect)
 await screen.findByText('Desk could not prepare the ChatGPT connection. Check your internet connection and try again.')
 fireEvent.click(screen.getByRole('button',{name:'Connect ChatGPT'}))
 await screen.findByRole('link',{name:'Continue sign-in in your browser'})
 expect(screen.queryByRole('alert')).toBeNull()
})

it('recovers a remembered account after runtime preparation without requiring another sign-in',async()=>{
 setup({connected:false,model:null,runtimeMissing:true,alreadyConnected:true})
 const connect=await screen.findByRole('button',{name:'Connect ChatGPT'})
 await waitFor(()=>expect((connect as HTMLButtonElement).disabled).toBe(false));fireEvent.click(connect)
 await screen.findByText('Connected to ChatGPT')
 await screen.findByRole('combobox',{name:'Model'})
 expect(screen.queryByRole('alert')).toBeNull()
 expect(screen.queryByRole('link',{name:'Continue sign-in in your browser'})).toBeNull()
})
