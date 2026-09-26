import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { Tooltip } from 'radix-ui'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { MappedInputFields } from './MappedInputFields'
import { jobsAPI } from './client'
import { prepareMappedInputs } from './mappedInputs'
import type { PackDocument } from '../mcp/types'
import type { MappingV2 } from './mappingTypes'
vi.mock('./client',()=>({jobsAPI:vi.fn()}))
vi.mock('./mappedInputs',async original=>({...await original<object>(),prepareMappedInputs:vi.fn()}))
const doc={rules:[{condition:{op:'fact',path:'/score',value:7}}],evidenceRequirements:[]} as unknown as PackDocument
const mapping: MappingV2={version:2,case:{facts:[{target:'/score',source:'/facts/score'}],evidence:[]},sources:[]}
const source={mapping,case:{facts:{score:7}},sources:{},mappingDigest:'digest'}
const result={input:{source,facts:{score:7}},factsText:'{"score":7}',evidenceText:''}
beforeEach(()=>{vi.mocked(jobsAPI).mockResolvedValue([]);vi.mocked(prepareMappedInputs).mockResolvedValue(result)})
afterEach(()=>{cleanup();vi.resetAllMocks()})
async function view(fixed?:MappingV2) {
 const onChange=vi.fn(), query=new QueryClient({defaultOptions:{queries:{retry:false}}})
 const rendered=render(<QueryClientProvider client={query}><Tooltip.Provider><MappedInputFields doc={doc} fixed={fixed} disabled={false} onChange={onChange}/></Tooltip.Provider></QueryClientProvider>)
 await waitFor(()=>expect((screen.getByRole('button',{name:'Read sources and preview'}) as HTMLButtonElement).disabled).toBe(false))
 return {...rendered,onChange}
}
it('never acquires on mount; explicit preview enables submission and edits invalidate it',async()=>{
 const {onChange}=await view()
 expect(prepareMappedInputs).not.toHaveBeenCalled()
 fireEvent.click(screen.getByRole('button',{name:'Read sources and preview'}))
 await screen.findByText('Mapped inputs')
 expect(onChange).toHaveBeenLastCalledWith(source)
 fireEvent.change(screen.getByLabelText('Case inputs (JSON)'),{target:{value:'{"facts":{"score":8}}'}})
 expect(onChange).toHaveBeenLastCalledWith(undefined)
 expect(screen.queryByText('Mapped inputs')).toBeNull()
})
it('locks a released mapping and starts without retained case values or files',async()=>{
 await view({...mapping,sources:undefined})
 expect(screen.queryByText('Edit mapping')).toBeNull()
 expect(screen.queryByRole('button',{name:'Add local file'})).toBeNull()
 expect((screen.getByLabelText('Case inputs (JSON)') as HTMLTextAreaElement).value).toBe('{"facts": {}, "evidence": {}}')
})
it('cancel and unmount discard late preparation results',async()=>{
 let resolve!:(value:typeof result)=>void
 vi.mocked(prepareMappedInputs).mockReturnValue(new Promise(r=>{resolve=r}))
 const {onChange,unmount}=await view()
 fireEvent.click(screen.getByRole('button',{name:'Read sources and preview'}))
 await waitFor(()=>expect(prepareMappedInputs).toHaveBeenCalled())
 fireEvent.click(screen.getByRole('button',{name:'Cancel'}))
 unmount()
 await act(async()=>resolve(result))
 expect(onChange).not.toHaveBeenCalledWith(source)
})
it('does not allow malformed mapping text to trigger acquisition',async()=>{
 await view()
 fireEvent.click(screen.getByText('Edit mapping'))
 fireEvent.change(screen.getByLabelText('Mapping JSON'),{target:{value:'{"version":2,"sources":{}}'}})
 expect((screen.getByRole('button',{name:'Read sources and preview'}) as HTMLButtonElement).disabled).toBe(true)
 expect(prepareMappedInputs).not.toHaveBeenCalled()
})
