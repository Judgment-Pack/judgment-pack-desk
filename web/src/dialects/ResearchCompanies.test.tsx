import {useState} from 'react'
import {cleanup,fireEvent,render,screen,waitFor} from '@testing-library/react'
import {QueryClient,QueryClientProvider} from '@tanstack/react-query'
import {Tooltip} from 'radix-ui'
import {afterEach,expect,it,vi} from 'vitest'
import {ResearchCompanies} from './ResearchCompanies'
import {dialectAPI} from './client'
import type {ResearchRecipe} from './research'
vi.mock('./client',async original=>({...await original<typeof import('./client')>(),dialectAPI:vi.fn()}))
afterEach(()=>{cleanup();vi.resetAllMocks()})
it('loads only the chosen dialect and retains explicit company identities and revisions',async()=>{
 const source='prep_'+'a'.repeat(32),onChange=vi.fn()
 vi.mocked(dialectAPI).mockImplementation(async path=>!path?{items:[{id:source,name:'Reviewed companies',records:1}]}:{sha256:'b'.repeat(64),document:{records:[{research:{entity:{id:'entity-company',kind:'company',name:'Example Co'}}}]}} as any)
 function Harness(){const[value,setValue]=useState({companyIds:[],companyInputs:[]} as unknown as ResearchRecipe);return <ResearchCompanies value={value} disabled={false} records={[]} onChange={change=>{onChange(change);setValue({...value,...change})}}/>}
 render(<QueryClientProvider client={new QueryClient({defaultOptions:{queries:{retry:false}}})}><Tooltip.Provider><Harness/></Tooltip.Provider></QueryClientProvider>)
 await waitFor(()=>expect(dialectAPI).toHaveBeenCalledTimes(1))
 fireEvent.keyDown(screen.getByRole('combobox',{name:'Company records'}),{key:'ArrowDown'})
 fireEvent.click(await screen.findByRole('option',{name:'Reviewed companies'}))
 const company=await screen.findByRole('checkbox',{name:'Example Co'});expect((company as HTMLInputElement).checked).toBe(false)
 fireEvent.click(company)
 expect(onChange).toHaveBeenLastCalledWith({companyInputs:[{preparationId:source,revision:'b'.repeat(64),entityId:'entity-company'}]})
 fireEvent.keyDown(screen.getByRole('combobox',{name:'Company records'}),{key:'ArrowDown'});fireEvent.click(await screen.findByRole('option',{name:'This dialect'}))
 expect(onChange).toHaveBeenLastCalledWith({companyIds:[],companyInputs:[]})
})
it('selects procurement source records with exact identities and revisions using generic labels',async()=>{
 const source='prep_'+'c'.repeat(32),revision='d'.repeat(64),onChange=vi.fn()
 vi.mocked(dialectAPI).mockImplementation(async path=>!path?{items:[{id:source,name:'Procurement notices',records:2}]}:{sha256:revision,document:{records:[{research:{entity:{id:'notice-123',kind:'record',name:'Laundry services notice'}}},{research:{entity:{id:'person-123',kind:'person',name:'Unrelated person'}}}]}} as any)
 function Harness(){const[value,setValue]=useState({target:'records',companyIds:[],companyInputs:[]} as unknown as ResearchRecipe);return <ResearchCompanies value={value} disabled={false} records={[]} onChange={change=>{onChange(change);setValue({...value,...change})}}/>}
 const client=new QueryClient({defaultOptions:{queries:{retry:false}}})
 render(<QueryClientProvider client={client}><Tooltip.Provider><Harness/></Tooltip.Provider></QueryClientProvider>)
 expect(screen.getByText('Selected records')).toBeTruthy()
 expect(screen.getByText('No eligible records in this dialect. Choose another dialect, or research source records first.')).toBeTruthy()
 expect(screen.queryByText('Selected companies')).toBeNull()
 await waitFor(()=>expect(dialectAPI).toHaveBeenCalledTimes(1))
 fireEvent.keyDown(screen.getByRole('combobox',{name:'Source records'}),{key:'ArrowDown'})
 fireEvent.click(await screen.findByRole('option',{name:'Procurement notices'}))
 fireEvent.click(await screen.findByRole('checkbox',{name:'Laundry services notice'}))
 expect(screen.queryByRole('checkbox',{name:'Unrelated person'})).toBeNull()
 expect(onChange).toHaveBeenLastCalledWith({companyInputs:[{preparationId:source,revision,entityId:'notice-123'}]})
 expect(dialectAPI).toHaveBeenCalledTimes(2)
 client.clear()
})
