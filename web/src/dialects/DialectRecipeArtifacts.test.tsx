import {act,cleanup,fireEvent,render,screen,waitFor} from '@testing-library/react'
import {QueryClient,QueryClientProvider} from '@tanstack/react-query'
import {MemoryRouter} from 'react-router-dom'
import {Tooltip} from 'radix-ui'
import {afterEach,beforeEach,expect,it,vi} from 'vitest'
import {DialectRecipeNotice,RecipeReference} from './DialectRecipeArtifacts'
import {dialectAPI} from './client'
import type {DialectRecipeProposal} from './proposals'
const m=vi.hoisted(()=>({update:vi.fn(),snapshot:{chats:[]} as any}))
vi.mock('./client',async original=>({...await original<typeof import('./client')>(),dialectAPI:vi.fn()}))
vi.mock('../chat/ChatProvider',()=>({useChats:()=>({store:{update:m.update,getSnapshot:()=>m.snapshot},...m.snapshot})}))
vi.mock('../config/DeskConfigProvider',()=>({useEffectiveConfig:()=>({aiConnections:{data:{connections:[]}},config:{assistant:{engine:'vercel',endpoint:null}}})}))
vi.mock('../assistant/aiConnections',()=>({deskAIConnections:()=>[{id:'legacy-api',name:'My connection'}],resolveAIConnection:(_:unknown,id:string)=>({assistant:{engine:'vercel',endpoint:{model:'enabled-model',models:['enabled-model']}},...(id==='legacy-api'?{connection:{id,revision:'a'.repeat(64)}}:{})})}))
vi.mock('../search/connections',()=>({useSearchConnections:()=>({available:true,loading:false,data:{connections:[]}})}))
vi.mock('../mcp/queries',()=>({usePacks:()=>({data:{packs:[]}}),usePack:()=>({})}))
const proposal:DialectRecipeProposal={version:1,draftId:'draft-1',createdAt:'2026-01-01',name:'Research brief',intent:'Find companies serving decision teams.',target:'companies',fields:[{name:'description',description:'What the company does',type:'string'}],criteria:[],limits:{records:25,searches:10,pages:30,modelCalls:30}}
beforeEach(()=>{m.update.mockClear();m.snapshot={chats:[{id:'chat-1',inputDrafts:[proposal]}]};vi.mocked(dialectAPI).mockReset()})
afterEach(cleanup)
function show(){render(<QueryClientProvider client={new QueryClient({defaultOptions:{queries:{retry:false}}})}><Tooltip.Provider><MemoryRouter><DialectRecipeNotice chatId="chat-1" drafts={[proposal]}/></MemoryRouter></Tooltip.Provider></QueryClientProvider>)}
async function chooseAI(){fireEvent.keyDown(screen.getByRole('combobox',{name:'AI connection'}),{key:'ArrowDown'});fireEvent.click(await screen.findByRole('option',{name:'My connection'}))}
it('requires a human connection choice, saves once, and never starts research',async()=>{
 let finish!:(value:unknown)=>void;vi.mocked(dialectAPI).mockImplementation(()=>new Promise(resolve=>{finish=resolve}))
 show();fireEvent.click(screen.getByRole('button',{name:'Review recipe'}))
 expect(screen.getByRole('dialog',{name:'Review dialect recipe'})).toBeTruthy()
 const save=screen.getByRole('button',{name:'Save to Dialects'}) as HTMLButtonElement
 expect(save.disabled).toBe(true);expect(dialectAPI).not.toHaveBeenCalled()
 await chooseAI();expect(save.disabled).toBe(false)
 fireEvent.click(save);fireEvent.click(save)
 expect(dialectAPI).toHaveBeenCalledTimes(1)
 expect(dialectAPI).toHaveBeenCalledWith('','POST',expect.objectContaining({name:'Research brief',recipe:expect.objectContaining({research:expect.objectContaining({ai:{connection:'legacy-api',revision:'a'.repeat(64),model:'enabled-model'}})}),chatOrigin:{chatId:'chat-1',draftId:'draft-1'}}))
 await act(async()=>finish({document:{id:'prep_'+'b'.repeat(32)}}))
 await waitFor(()=>expect(screen.queryByRole('dialog',{name:'Review dialect recipe'})).toBeNull())
 expect(m.update).toHaveBeenCalledWith('chat-1',{inputDrafts:[expect.objectContaining({savedInputId:'prep_'+'b'.repeat(32)})]})
 expect(dialectAPI).toHaveBeenCalledTimes(1)
})
it('cancel leaves the proposal in chat and restores focus without writing Dialects',async()=>{
 show();const opener=screen.getByRole('button',{name:'Review recipe'});opener.focus();fireEvent.click(opener)
 fireEvent.click(screen.getByRole('button',{name:'Cancel'}))
 await waitFor(()=>expect(screen.queryByRole('dialog')).toBeNull())
 expect(document.activeElement).toBe(opener);expect(dialectAPI).not.toHaveBeenCalled();expect(m.update).not.toHaveBeenCalled()
})
it('retains reviewed fields and the same save key after a failed save',async()=>{
 vi.mocked(dialectAPI).mockRejectedValue(Error('Connection interrupted'))
 show();fireEvent.click(screen.getByRole('button',{name:'Review recipe'}));await chooseAI()
 fireEvent.change(screen.getByRole('textbox',{name:'Name'}),{target:{value:'Reviewed name'}})
 fireEvent.click(screen.getByRole('button',{name:'Save to Dialects'}));await screen.findByText('Connection interrupted')
 expect((screen.getByRole('textbox',{name:'Name'}) as HTMLInputElement).value).toBe('Reviewed name')
 fireEvent.click(screen.getByRole('button',{name:'Save to Dialects'}))
 await waitFor(()=>expect(dialectAPI).toHaveBeenCalledTimes(2))
 expect(vi.mocked(dialectAPI).mock.calls[0]).toEqual(vi.mocked(dialectAPI).mock.calls[1]);expect(m.update).not.toHaveBeenCalled()
})

it('opens the saved recipe together with the originating chat',()=>{
 const saved={...proposal,savedInputId:'prep_'+'b'.repeat(32)}
 render(<QueryClientProvider client={new QueryClient()}><Tooltip.Provider><MemoryRouter><RecipeReference chatId="chat-1" draft={saved}/></MemoryRouter></Tooltip.Provider></QueryClientProvider>)
 expect(screen.getByRole('link',{name:'Open dialect'}).getAttribute('href')).toBe('/dialects/'+saved.savedInputId+'?tab=recipe&chat=chat-1')
})

it('omits saved recipes from the conversation tail without reviving an older proposal',()=>{
 const id='prep_'+'b'.repeat(32),saved={...proposal,draftId:'saved-draft',savedInputId:id}
 const view=render(<MemoryRouter><DialectRecipeNotice chatId="chat-1" drafts={[proposal,saved]}/></MemoryRouter>)
 expect(screen.queryByRole('link',{name:'Open dialect'})).toBeNull()
 expect(screen.queryByRole('button',{name:'Review recipe'})).toBeNull()
 view.rerender(<MemoryRouter><DialectRecipeNotice chatId="chat-1" drafts={[proposal]}/></MemoryRouter>)
 expect(screen.getByRole('button',{name:'Review recipe'})).toBeTruthy()
 // The summary no longer duplicates the field list; the review still owns it.
 expect(screen.queryByText('description',{exact:true})).toBeNull()
 view.rerender(<MemoryRouter><DialectRecipeNotice chatId="chat-1" drafts={[saved]}/></MemoryRouter>)
 expect(screen.queryByRole('link',{name:'Open dialect'})).toBeNull()
})
