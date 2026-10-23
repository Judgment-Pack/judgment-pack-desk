import { QueryClientProvider } from '@tanstack/react-query'
import { cleanup,fireEvent,render,screen,waitFor } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { afterEach,expect,it,vi } from 'vitest'
import { DeskConfigFixture } from '../config/DeskConfigProvider'
import { decodeDeskConfig,effectiveConfig } from '../config/deskConfig'
import { type AssistantProfileRead } from '../config/assistantProfile'
import { testQueryClient } from '../testing/harness'
import { DeskModelPreferences } from './DeskModelPreferences'
vi.mock('./providers',()=>({useProviderStatus:()=>({data:{account:'connected'}}),useProviderModels:()=>({data:{models:[{id:'machine',name:'Machine',efforts:['low','high'],defaultEffort:'low'},{id:'local',name:'Local',efforts:['low','high'],defaultEffort:'low'}]}})}))
afterEach(()=>{cleanup();vi.unstubAllGlobals()})
const machine={engine:'codex',thinking:'off',agent:{provider:'openai',authMethod:'subscription',model:'machine',models:['machine'],tools:[],effort:'high'}}
function setup(profile:AssistantProfileRead={present:true,sha256:'a'.repeat(64),value:{profileVersion:1,codex:{inherit:true}}},fail=false,withPolicy=true){
 const writes:any[]=[]
 vi.stubGlobal('fetch',async(_url:string,init:RequestInit={})=>{
  const body=typeof init.body==='string'?JSON.parse(init.body):null
  if(init.method==='PUT'){writes.push(body);return new Response(JSON.stringify(fail?{error:'File changed on disk',code:'stale'}:{path:body.path,content:body.content,sha256:'b'.repeat(64),bytes:body.content.length}),{status:fail?409:200})}
  return new Response('{}',{status:404})
 })
 const client=testQueryClient()
 const config=(read:AssistantProfileRead)=>effectiveConfig(undefined,undefined,undefined,{promptPolicy:withPolicy?{projectInstructionsBytes:4000}:undefined,path:'/machine/desk.json',present:true,sha256:'m'.repeat(64),decoded:decodeDeskConfig(JSON.stringify({deskConfigVersion:1,assistant:machine}),'desk')},undefined,undefined,read)
 const tree=(read:AssistantProfileRead)=><QueryClientProvider client={client}><MemoryRouter><DeskConfigFixture value={config(read)}><DeskModelPreferences unavailable={false}/></DeskConfigFixture></MemoryRouter></QueryClientProvider>
 const view=render(tree(profile))
 return {writes,rerender:(read:AssistantProfileRead)=>view.rerender(tree(read))}
}
it('saves only this desk profile and uses the original revision if disk changes during editing',async()=>{
 const state=setup()
 expect((screen.getByRole('checkbox',{name:'Use shared defaults'}) as HTMLInputElement).checked).toBe(true)
 fireEvent.click(screen.getByRole('checkbox',{name:'Use shared defaults'}))
 fireEvent.click(screen.getByRole('button',{name:'Allowed models'}));fireEvent.click(screen.getByRole('button',{name:'Select all'}));fireEvent.keyDown(screen.getByRole('dialog'),{key:'Escape'})
 fireEvent.click(screen.getByRole('combobox',{name:'Default model'}));fireEvent.click(screen.getByRole('option',{name:'local'}))
 state.rerender({present:true,sha256:'c'.repeat(64),value:{profileVersion:1,codex:{inherit:true},api:{inherit:true}}})
 fireEvent.click(screen.getByRole('button',{name:'Save preferences'}))
 await waitFor(()=>expect(state.writes).toHaveLength(1))
 expect(state.writes[0].path).toBe('jpack-assistant.json');expect(state.writes[0].baseSha256).toBe('a'.repeat(64))
 expect(JSON.parse(state.writes[0].content)).toEqual({profileVersion:1,codex:{inherit:false,models:['machine','local'],model:'local'}})
 expect(machine.agent.model).toBe('machine');expect(machine.agent.models).toEqual(['machine'])
})
it('restores inheritance without removing the inactive provider preference',async()=>{
 const api={inherit:false as const,models:['api'],model:'api'}
 const state=setup({present:true,sha256:'a'.repeat(64),value:{profileVersion:1,codex:{inherit:false,models:['local'],model:'local',effort:'low'},api}})
 fireEvent.click(screen.getByRole('checkbox',{name:'Use shared defaults'}));fireEvent.click(screen.getByRole('button',{name:'Save preferences'}))
 await waitFor(()=>expect(state.writes).toHaveLength(1))
 expect(JSON.parse(state.writes[0].content)).toEqual({profileVersion:1,codex:{inherit:true},api})
})
it('preserves edits and reports a stale write without forcing replacement',async()=>{
 const state=setup(undefined,true);fireEvent.click(screen.getByRole('checkbox',{name:'Use shared defaults'}));fireEvent.click(screen.getByRole('button',{name:'Save preferences'}))
 await screen.findByRole('alert');expect(state.writes[0].override).toBe(false)
 expect((screen.getByRole('checkbox',{name:'Use shared defaults'}) as HTMLInputElement).checked).toBe(false)
 expect(screen.queryByText('Desk model preferences saved.')).toBeNull()
})
it('blocks invalid profiles while keeping the file link available for repair',()=>{
 const state=setup({present:true,sha256:'a'.repeat(64),problem:'Profile is invalid'})
 expect((screen.getByRole('button',{name:'Save preferences'}) as HTMLButtonElement).disabled).toBe(true)
 expect(screen.getByRole('link',{name:'jpack-assistant.json'})).toBeTruthy();expect(state.writes).toHaveLength(0)
})

it('edits project instructions and retains original revision and inactive preferences',async()=>{
 const original={profileVersion:1 as const,codex:{inherit:true as const},api:{inherit:true as const},context:{version:1 as const,projectBrief:'brief.md'},projectInstructions:'Original guidance.\n'}
 const state=setup({present:true,sha256:'a'.repeat(64),value:original})
 const field=screen.getByRole('textbox',{name:'Project instructions'}) as HTMLTextAreaElement
 expect(field.value).toBe('Original guidance.\n')
 const text='Keep missing facts explicit. 😀\n'
 fireEvent.change(field,{target:{value:text}})
 expect(screen.getByText('Unsaved changes')).toBeTruthy()
 state.rerender({present:true,sha256:'c'.repeat(64),value:{...original,projectInstructions:'Another editor'}})
 expect(field.value).toBe(text)
 fireEvent.click(screen.getByRole('button',{name:'Save preferences'}))
 await waitFor(()=>expect(state.writes).toHaveLength(1))
 expect(state.writes[0].baseSha256).toBe('a'.repeat(64))
 expect(JSON.parse(state.writes[0].content)).toEqual({...original,projectInstructions:text})
 await screen.findByText('Desk model preferences saved.')
})
it('refuses multibyte instructions over the byte bound and cancels without losing source',()=>{
 const state=setup({present:true,sha256:'a'.repeat(64),value:{profileVersion:1,projectInstructions:'Original'}})
 const field=screen.getByRole('textbox',{name:'Project instructions'}) as HTMLTextAreaElement
 fireEvent.change(field,{target:{value:'😀'.repeat(1000)+'x'}})
 expect(field.getAttribute('aria-invalid')).toBe('true')
 expect((screen.getByRole('button',{name:'Save preferences'}) as HTMLButtonElement).disabled).toBe(true)
 expect(state.writes).toHaveLength(0)
 fireEvent.click(screen.getByRole('button',{name:'Cancel'}))
 expect(field.value).toBe('Original')
})
it('saves instructions in registry profiles without introducing legacy selectors',async()=>{
 const original={profileVersion:2 as const,inherit:true,projectInstructions:'Old'}
 const state=setup({present:true,sha256:'a'.repeat(64),value:original})
 fireEvent.change(screen.getByRole('textbox',{name:'Project instructions'}),{target:{value:'New'}})
 fireEvent.click(screen.getByRole('button',{name:'Save preferences'}))
 await waitFor(()=>expect(state.writes).toHaveLength(1))
 expect(JSON.parse(state.writes[0].content)).toEqual({...original,projectInstructions:'New'})
})

it('preserves instructions and allows model preference saves while policy metadata is unavailable',async()=>{
 const original={profileVersion:1 as const,codex:{inherit:true as const},projectInstructions:'Saved guidance.\n'}
 const state=setup({present:true,sha256:'a'.repeat(64),value:original},false,false)
 const field=screen.getByRole('textbox',{name:'Project instructions'}) as HTMLTextAreaElement
 expect(field.disabled).toBe(true);expect(field.value).toBe(original.projectInstructions)
 fireEvent.click(screen.getByRole('checkbox',{name:'Use shared defaults'}))
 fireEvent.click(screen.getByRole('button',{name:'Save preferences'}))
 await waitFor(()=>expect(state.writes).toHaveLength(1))
 expect(JSON.parse(state.writes[0].content).projectInstructions).toBe(original.projectInstructions)
})

it.each([1,2] as const)('saves and reloads every project critic choice in profile v%d', async version=>{
 const state=setup({present:true,sha256:'a'.repeat(64),value:version===1?{profileVersion:1,codex:{inherit:true}}:{profileVersion:2,inherit:true}})
 for(const [label,critic] of [['Off','off'],['Auto','auto'],['On','on'],['Policy table default (auto)',undefined]] as const){
  fireEvent.click(screen.getByRole('combobox',{name:'Project review default'}))
  fireEvent.click(screen.getByRole('option',{name:label}))
  fireEvent.click(screen.getByRole('button',{name:'Save preferences'}))
  await waitFor(()=>expect(state.writes.length).toBeGreaterThan(0))
  const saved=JSON.parse(state.writes.at(-1).content)
  expect(saved.critic).toBe(critic)
  expect(saved.profileVersion).toBe(version)
  state.rerender({present:true,sha256:'b'.repeat(64),value:saved})
  await screen.findByText('Desk model preferences saved.')
  expect(screen.getByRole('combobox',{name:'Project review default'}).textContent).toBe(label)
  state.writes.length=0
 }
})
