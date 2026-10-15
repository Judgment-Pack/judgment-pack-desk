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
