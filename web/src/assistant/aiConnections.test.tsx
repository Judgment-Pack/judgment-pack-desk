import { useState } from 'react'
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { QueryClientProvider } from '@tanstack/react-query'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, expect, it, vi } from 'vitest'
import { DeskConfigProvider, DeskConfigFixture } from '../config/DeskConfigProvider'
import { effectiveConfig } from '../config/deskConfig'
import { testQueryClient } from '../testing/harness'
import { AI_CONNECTIONS_KEY, connectionAssistant, deskAIConnections, resolveAIConnection, type AIRegistry, type AIConnection } from './aiConnections'
import { AIConnectionsSettings } from './AIConnectionsSettings'
import { probeAssistantEndpoint, readAssistantKey, removeAssistantKey, storeAssistantKey } from './client'
import { DeskAISettings } from './DeskAISettings'
import { useAssistantSlot } from './useAssistantSlot'
import { ModelControl } from '../chat/ModelControl'
import { decodeCheckpoint } from '../chat/checkpoint'
import { INITIAL_STATE } from '../research/run'

const alpha='ai-111111111111111111111111',beta='ai-222222222222222222222222'
const api=(id:string,name:string):AIConnection=>({id,name,enabled:true,revision:'c'.repeat(64),assistant:{engine:'vercel',thinking:'off',endpoint:{kind:'openai-compatible',url:'https://example.invalid/v1',model:'same-model',models:['same-model'],tools:[]}}})
const registry=():AIRegistry=>({version:1,defaultConnection:alpha,connections:[api(alpha,'Work'),api(beta,'Personal')],sha256:'a'.repeat(64),path:'/machine/ai-connections.json'})
afterEach(()=>{cleanup();vi.unstubAllGlobals()})
function setup(child:React.ReactNode){
 let state=registry(),profile={profileVersion:1,codex:{inherit:true},api:{inherit:true}} as unknown
 const writes:{url:string;body:any;headers:Headers}[]=[]
 const client=testQueryClient()
 vi.stubGlobal('fetch',async(url:string,init:RequestInit={})=>{
  const path=new URL(url,'http://localhost'),headers=new Headers(init.headers),body=typeof init.body==='string'?JSON.parse(init.body):undefined
  if(init.method==='PUT')writes.push({url:path.pathname,body,headers})
  if(path.pathname==='/api/ai-connections'){
   if(init.method==='PUT'){
    if(headers.get('If-Match')!==state.sha256)return new Response(JSON.stringify({error:'AI connections changed',code:'desk-config-changed'}),{status:409})
    state={...body,path:state.path,sha256:'b'.repeat(64),connections:body.connections.map((c:AIConnection)=>({...c,revision:'d'.repeat(64)}))}
   }
   return Response.json(state)
  }
  if(path.pathname==='/api/desk-config')return Response.json({path:'/machine/desk.json',present:true,sha256:'e'.repeat(64),content:JSON.stringify({deskConfigVersion:1})})
  if(path.pathname==='/api/file'&&((body?.path??path.searchParams.get('path'))==='jpack-assistant.json')){
   if(body)profile=JSON.parse(body.content)
   return Response.json({path:'jpack-assistant.json',content:JSON.stringify(profile),sha256:'f'.repeat(64),bytes:100})
  }
  if(path.pathname==='/api/assistant/key')return Response.json({present:true,bound:true,fingerprint:'fixture',origin:'https://example.invalid',kind:'openai-compatible',configuredOrigin:'https://example.invalid',configuredKind:'openai-compatible'})
  if(path.pathname==='/api/model-providers')return Response.json({providers:[]})
  return Response.json({error:'not-found'},{status:404})
 })
 const rendered=render(<QueryClientProvider client={client}><MemoryRouter><DeskConfigProvider>{child}</DeskConfigProvider></MemoryRouter></QueryClientProvider>)
 return {client,writes,...rendered,get registry(){return state}}
}
it('adds another API connection without changing the default or existing connection records',async()=>{
 const state=setup(<AIConnectionsSettings unavailable={false}/>);await screen.findByText('Work')
 fireEvent.click(screen.getByRole('button',{name:'Add connection'}));fireEvent.change(screen.getByRole('textbox',{name:'Name'}),{target:{value:'Anthropic — Work'}})
 fireEvent.click(screen.getByRole('combobox',{name:'Provider'}));fireEvent.click(screen.getByRole('option',{name:'Anthropic API'}))
 fireEvent.click(screen.getByRole('button',{name:'Add connection'}));await screen.findByRole('button',{name:'Back to connections'})
 expect(state.writes).toHaveLength(1);expect(state.registry.connections).toHaveLength(3);expect(state.registry.defaultConnection).toBe(alpha)
 expect(state.registry.connections.slice(0,2).map(c=>[c.id,c.name,c.assistant.endpoint?.url])).toEqual([[alpha,'Work','https://example.invalid/v1'],[beta,'Personal','https://example.invalid/v1']])
 expect(state.writes[0]!.headers.get('If-Match')).toBe('a'.repeat(64));expect(JSON.stringify(state.writes[0]!.body)).not.toContain('key')
 expect(screen.queryByRole('combobox',{name:'Connection method'})).toBeNull()
})
it('saves the desk connection choice only in its project profile',async()=>{
 const state=setup(<DeskAISettings unavailable={false}/>);await screen.findByRole('checkbox',{name:'Use shared assistant defaults'})
 fireEvent.click(screen.getByRole('checkbox',{name:'Use shared assistant defaults'}))
 fireEvent.click(screen.getByRole('combobox',{name:'Default connection'}));fireEvent.click(screen.getByRole('option',{name:'Personal'}))
 fireEvent.click(screen.getByRole('button',{name:'Save preferences'}));await screen.findByText('Assistant preferences saved.')
 expect(state.writes).toHaveLength(1);expect(state.writes[0]!.url).toBe('/api/file')
 expect(JSON.parse(state.writes[0]!.body.content)).toMatchObject({profileVersion:2,inherit:false,connections:[alpha,beta],defaultConnection:beta})
 expect(state.registry.defaultConnection).toBe(alpha)
})
it('keeps an explicitly selected connection when the shared default changes and blocks a disabled target',async()=>{
 function Probe(){const pinned=useAssistantSlot(alpha),normal=useAssistantSlot();return <output>{JSON.stringify({pinned:pinned.connectionId,normal:normal.connectionId,state:pinned.state,problem:pinned.unusable})}</output>}
 const state=setup(<Probe/>);await waitFor(()=>expect(screen.getByRole('status').textContent).toContain(`"normal":"${alpha}"`))
 state.client.setQueryData(AI_CONNECTIONS_KEY,{...state.registry,defaultConnection:beta})
 await waitFor(()=>expect(screen.getByRole('status').textContent).toContain(`"normal":"${beta}"`));expect(screen.getByRole('status').textContent).toContain(`"pinned":"${alpha}"`)
 state.client.setQueryData(AI_CONNECTIONS_KEY,{...state.registry,defaultConnection:beta,connections:state.registry.connections.map(c=>c.id===alpha?{...c,enabled:false}:c)})
 await waitFor(()=>expect(screen.getByRole('status').textContent).toContain('unavailable for this desk'))
 expect(screen.getByRole('status').textContent).not.toContain(`"pinned":"${beta}"`)
})
it('groups identical model IDs by connection and returns the selected connection ID',async()=>{
 const selected=vi.fn(),client=testQueryClient();const effective={...effectiveConfig(undefined),aiConnections:{loading:false,data:registry()}}
 function Composer(){const [id,setId]=useState(alpha);return <ModelControl id="model" connectionId={id} model="same-model" models={['same-model']} codex={false} enabled disabled={false} onModelChange={vi.fn()} onReasoningChange={vi.fn()} onConnectionChange={(...args)=>{selected(...args);setId(args[0])}}/>}
 render(<QueryClientProvider client={client}><DeskConfigFixture value={effective}><Composer/></DeskConfigFixture></QueryClientProvider>)
 fireEvent.click(screen.getByRole('button',{name:'Model: same-model'}));expect(screen.getByText('Work')).toBeTruthy();expect(screen.getByText('Personal')).toBeTruthy()
 fireEvent.click(screen.getAllByRole('radio',{name:'same-model'})[1]!);expect(selected).toHaveBeenCalledWith(beta,'Personal','same-model')
 expect(screen.getAllByRole('radio',{name:'same-model'})[1]!.getAttribute('data-state')).toBe('checked')
})
it('preserves legacy desk model overrides and never resolves removed IDs through the default',()=>{
 const c={...api('legacy-api','Existing'),assistant:api(alpha,'Work').assistant}
 const read={present:true,value:{profileVersion:1 as const,api:{inherit:false as const,models:['custom-model'],model:'custom-model'}}}
 expect(connectionAssistant(c,read).endpoint?.model).toBe('custom-model')
 const effective={...effectiveConfig(undefined),aiConnections:{loading:false,data:registry()},assistantProfile:{present:true,value:{profileVersion:2 as const,inherit:false,connections:[beta],defaultConnection:beta,models:{}}}}
 expect(deskAIConnections(registry(),effective.assistantProfile).map(c=>c.id)).toEqual([beta]);expect(resolveAIConnection(effective,alpha).problem).toContain('unavailable');expect(resolveAIConnection(effective).connection?.id).toBe(beta)
})
it('retains response connection attribution and leaves older replies unattributed',()=>{
 const base={state:{...INITIAL_STATE,turns:[{id:'first',role:'assistant',kind:'message',text:'Reply',at:'2026-10-06T10:00:00Z',target:{connectionId:alpha,connectionName:'Work',model:'same-model'}},{id:'old',role:'assistant',kind:'message',text:'Older reply',at:'2026-10-05T10:00:00Z'}]},sources:[]}
 const decoded=decodeCheckpoint(base);expect(decoded.state.turns[0]?.target).toEqual(base.state.turns[0]!.target);expect(decoded.state.turns[1]?.target).toBeUndefined()
 expect(()=>decodeCheckpoint({...base,state:{...base.state,turns:[{...base.state.turns[0],target:{connectionId:'../../bad',connectionName:'Bad',model:'same-model'}}]}})).toThrow()
})

it('names the AI connection and its revision on the key routes and the probe', async () => {
 const seen:{url:string;headers:Headers}[]=[]
 vi.stubGlobal('fetch',async(url:string,init:RequestInit={})=>{seen.push({url,headers:new Headers(init.headers)});return Response.json(url.includes('/probe')?{reachable:true,status:200,latencyMs:1,diagnostic:''}:{present:true,bound:true,fingerprint:'f',origin:'',kind:'',configuredOrigin:'',configuredKind:''})})
 const revision='e'.repeat(64)
 await readAssistantKey(undefined,alpha,revision);await storeAssistantKey('a-key-for-the-test',alpha,revision);await removeAssistantKey(alpha,revision);await probeAssistantEndpoint(undefined,alpha,revision)
 expect(seen.map(row=>new URL(row.url,'http://desk.invalid').pathname)).toEqual(['/api/assistant/key','/api/assistant/key','/api/assistant/key','/api/assistant/probe'])
 for(const row of seen){expect(row.headers.get('X-Assistant-Connection'),row.url).toBe(alpha);expect(row.headers.get('X-Assistant-Revision'),row.url).toBe(revision)}
})
it('counts a connection name in characters (not UTF-16 units or bytes), as the desk does, and offers no save past 128',async()=>{
 setup(<AIConnectionsSettings unavailable={false}/>);await screen.findByText('Work')
 fireEvent.click(screen.getByRole('button',{name:'Add connection'}))
 const field=screen.getByRole('textbox',{name:'Name'})
 const add=()=>screen.getAllByRole('button',{name:'Add connection'}).at(-1) as HTMLButtonElement
 fireEvent.change(field,{target:{value:'é'.repeat(64)+'😀'.repeat(64)}});expect(add().disabled).toBe(false)
 expect(screen.queryByText("An AI connection's name must be 1 to 128 characters, with no leading or trailing spaces and no control characters.")).toBeNull()
 fireEvent.change(field,{target:{value:'é'.repeat(64)+'😀'.repeat(65)}});expect(add().disabled).toBe(true)
 expect(screen.getByText("An AI connection's name must be 1 to 128 characters, with no leading or trailing spaces and no control characters.")).toBeTruthy()
})
