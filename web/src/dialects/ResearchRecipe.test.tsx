import {cleanup,fireEvent,render,screen} from '@testing-library/react'
import {MemoryRouter} from 'react-router-dom'
import {Tooltip} from 'radix-ui'
import {QueryClient,QueryClientProvider} from '@tanstack/react-query'
import {afterEach,expect,it,vi} from 'vitest'
import {ResearchRecipe} from './ResearchRecipe'
import type {ResearchRecipe as Recipe} from './research'
const state=vi.hoisted(()=>({available:true,loading:false,isFetching:false,isError:false,revision:'b'.repeat(64)}))
vi.mock('../config/DeskConfigProvider',()=>({useEffectiveConfig:()=>({aiConnections:{data:{connections:[]}}})}))
vi.mock('../assistant/aiConnections',()=>({deskAIConnections:()=>[{id:'legacy-api',name:'My AI connection'}],resolveAIConnection:()=>({assistant:{engine:'vercel',endpoint:{model:'test',models:['test']}},connection:{id:'legacy-api',revision:'a'.repeat(64)}})}))
vi.mock('../search/connections',()=>({useSearchConnections:()=>({...state,data:{connections:[{id:'search-test',name:'Google search',revision:state.revision}]}})}))
vi.mock('../shell/UnsavedChanges',()=>({useConfirmDiscard:()=>vi.fn()}))
vi.mock('./queries',()=>({dialectListQuery:()=>({queryKey:['parent-inputs'],queryFn:async()=>({items:[]})}),dialectQuery:(id:string)=>({queryKey:['parent-input',id],queryFn:async()=>({document:{records:[]}})})}))
afterEach(()=>{cleanup();Object.assign(state,{available:true,loading:false,isFetching:false,isError:false,revision:'b'.repeat(64)})})
const recipe:Recipe={version:1,target:'companies',fields:[{name:'country',description:'Country',type:'string'}],criteria:[],ai:{connection:'legacy-api',revision:'a'.repeat(64),model:'test'},search:{connection:'search-test',revision:'a'.repeat(64)},limits:{records:3,searches:8,pages:6,modelCalls:15}}
function show(disabled=false){const change=vi.fn();render(<MemoryRouter><Tooltip.Provider><ResearchRecipe value={recipe} onChange={change} records={[]} disabled={disabled}/></Tooltip.Provider></MemoryRouter>);return change}
it('keeps the reviewed revision until the user accepts current settings, preserving all other edits',()=>{
 const change=show()
 expect(screen.getByText('Search settings changed. Review the current settings.')).toBeTruthy()
 expect(change).not.toHaveBeenCalled()
 fireEvent.click(screen.getByRole('button',{name:'Use current settings'}))
 expect(change).toHaveBeenCalledExactlyOnceWith({...recipe,search:{connection:'search-test',revision:state.revision}})
 expect(recipe.search?.revision).toBe('a'.repeat(64))
})
it('shows no recovery for a current connection',()=>{
 state.revision='a'.repeat(64);show()
 expect(screen.queryByRole('button',{name:'Use current settings'})).toBeNull()
})
it.each(['isError','available'] as const)('does not accept stale cached settings when %s changes',key=>{
 state[key]=key==='isError';show()
 expect(screen.queryByRole('button',{name:'Use current settings'})).toBeNull()
})
it('disables adoption while fetching a fresh status or while the recipe is locked',()=>{
 state.isFetching=true;let change=show()
 let button=screen.getByRole('button',{name:'Use current settings'}) as HTMLButtonElement
 expect(button.disabled).toBe(true);fireEvent.click(button);expect(change).not.toHaveBeenCalled()
 cleanup();state.isFetching=false;change=show(true)
 button=screen.getByRole('button',{name:'Use current settings'}) as HTMLButtonElement
 expect(button.disabled).toBe(true);fireEvent.click(button);expect(change).not.toHaveBeenCalled()
})
it('suggests two retries only for a new research setup',()=>{
 const change=vi.fn()
 render(<MemoryRouter><Tooltip.Provider><ResearchRecipe onChange={change} records={[]} disabled={false}/></Tooltip.Provider></MemoryRouter>)
 fireEvent.click(screen.getByRole('button',{name:'Set up research'}))
 expect(change.mock.calls[0][0].retries).toEqual({mode:'temporary',maxRetries:2,searchTimeouts:false})
 cleanup();const legacy=show()

 expect(screen.getByRole('combobox',{name:'Automatic retries'}).textContent).toContain('Off')
 expect(legacy).not.toHaveBeenCalled()
 fireEvent.change(screen.getByRole('spinbutton',{name:'Records'}),{target:{value:'4'}})
 expect(legacy.mock.calls[0][0].retries).toBeUndefined()
})

it('explains that the Records setting controls the run count',()=>{
 show()
 expect(screen.getByText('Maximum records per run. This setting takes precedence over record counts in the instructions.')).toBeTruthy()
})

it('keeps every section editable without Find, Edit or Done controls',()=>{
 const change=vi.fn()
 render(<MemoryRouter><Tooltip.Provider><ResearchRecipe value={{...recipe,ai:{connection:'',revision:'',model:''}}} onChange={change} records={[]} disabled={false}/></Tooltip.Provider></MemoryRouter>)
 expect(screen.getByRole('combobox',{name:'AI connection'})).toBeTruthy()
 expect(screen.getByRole('spinbutton',{name:'Records'})).toBeTruthy()
 expect(screen.getByRole('textbox',{name:'Description'})).toBeTruthy()
 expect(screen.queryByRole('combobox',{name:'Find'})).toBeNull()
 expect(screen.queryByRole('button',{name:/^(Done|Edit )/})).toBeNull()
 expect(change).not.toHaveBeenCalled()
 fireEvent.change(screen.getByRole('spinbutton',{name:'Records'}),{target:{value:'4'}})
 expect(change.mock.calls[0][0].limits.records).toBe(4)
})
it.each(['records','people-at-companies'] as const)('uses the saved %s target for parent labels without changing its recipe',target=>{
 const value:Recipe={...recipe,target,requireParents:true,companyIds:[]},change=vi.fn()
 const client=new QueryClient({defaultOptions:{queries:{retry:false}}})
 render(<QueryClientProvider client={client}><MemoryRouter><Tooltip.Provider><ResearchRecipe value={value} onChange={change} records={[]} disabled={false}/></Tooltip.Provider></MemoryRouter></QueryClientProvider>)
 expect(screen.getByRole('group',{name:target==='records'?'Selected records':'Selected companies'})).toBeTruthy()
 expect(screen.getByRole('status').textContent).toContain(target==='records'?'Select source records for related research.':'Select companies to research.')
 expect(change).not.toHaveBeenCalled()
 fireEvent.change(screen.getByRole('spinbutton',{name:'Records'}),{target:{value:'4'}})
 expect(change).toHaveBeenCalledExactlyOnceWith({...value,limits:{...value.limits,records:4}})
 client.clear()
})
