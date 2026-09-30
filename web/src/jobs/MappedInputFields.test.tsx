import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { Tooltip } from 'radix-ui'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import type { ComponentProps } from 'react'
import type { DriveFilePicker } from '../connections/DriveFilePicker'
const picker=vi.hoisted(()=>({props:undefined as ComponentProps<typeof DriveFilePicker>|undefined}))
vi.mock('../connections/DriveFilePicker',()=>({DriveFilePicker:(props:ComponentProps<typeof DriveFilePicker>)=>{picker.props=props;return null}}))
import { authorizeDrive, ConnectionRequestError } from '../connections/client'
vi.mock('../connections/client',async original=>({...await original<object>(),authorizeDrive:vi.fn()}))
import { MappedInputFields } from './MappedInputFields'
import { jobsAPI } from './client'
import { localSnapshot } from './sourceInputs'
import { prepareMappedInputs } from './mappedInputs'
import type { PackDocument } from '../mcp/types'
import type { MappingV2 } from './mappingTypes'
vi.mock('./client',()=>({jobsAPI:vi.fn()}))
vi.mock('./mappedInputs',async original=>({...await original<object>(),prepareMappedInputs:vi.fn()}))
vi.mock('./sourceInputs',async original=>({...await original<object>(),localSnapshot:vi.fn()}))
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
 fireEvent.click(screen.getByText('Case inputs (JSON)',{selector:'summary span'}));fireEvent.change(screen.getByLabelText('Case inputs (JSON)'),{target:{value:'{"facts":{"score":8}}'}})
 expect(onChange).toHaveBeenLastCalledWith(undefined)
 expect(screen.queryByText('Mapped inputs')).toBeNull()
})
it('locks a released mapping and starts without retained case values or files',async()=>{
 await view({...mapping,sources:undefined})
 expect(screen.queryByText('Edit mapping')).toBeNull()
 expect(screen.queryByRole('button',{name:'Add local file'})).toBeNull()
 fireEvent.click(screen.getByText('Case inputs (JSON)',{selector:'summary span'}))
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

it('keeps a selected file when an output pointer changes, while requiring a new preview',async()=>{
 const snapshot={original:{name:'sample.json'},content:'{"score":7}'}
 vi.mocked(localSnapshot).mockResolvedValue(snapshot as never)
 const {onChange}=await view()
 fireEvent.click(screen.getByRole('button',{name:'Add source'}))
 fireEvent.click(await screen.findByRole('button',{name:/Local JSON file/}))
 fireEvent.change(await screen.findByLabelText('Choose JSON file for source1'),{target:{files:[new File(['{}'],'sample.json',{type:'application/json'})]}})
 await screen.findByText('sample.json')
 fireEvent.click(screen.getByRole('button',{name:'Configure score'}))
 fireEvent.click(screen.getByRole('combobox',{name:'Source'}))
 fireEvent.click(await screen.findByRole('option',{name:'source1'}))
 fireEvent.change(screen.getByLabelText('Source path'),{target:{value:'/score'}})
 expect(onChange).toHaveBeenLastCalledWith(undefined)
 fireEvent.click(screen.getByRole('button',{name:'Configure source'}))
 expect(await screen.findByText('sample.json')).toBeTruthy()
 fireEvent.click(screen.getByRole('button',{name:'Read sources and preview'}))
 await waitFor(()=>expect(prepareMappedInputs).toHaveBeenCalledWith(expect.objectContaining({files:{source1:snapshot},mapping:expect.objectContaining({sources:[expect.objectContaining({name:'source1',read:{copy:{facts:[{target:'/score',source:'/score'}],evidence:[]}}})]})})))
})

it('requires pending request edits to be applied before replacing the source mapping',async()=>{
 await view()
 fireEvent.click(screen.getByText('Edit mapping'))
 fireEvent.change(screen.getByLabelText('Mapping JSON'),{target:{value:JSON.stringify({...mapping,sources:[{name:'records',kind:'operation',profile:'registry',arguments:{tool:'read',arguments:{}},read:{copy:{facts:[],evidence:[]}}}]})}})
 fireEvent.click(await screen.findByRole('button',{name:'records'}))
 fireEvent.change(screen.getByLabelText('Request parameters (JSON)'),{target:{value:'{"case":"pending"}'}})
 expect((screen.getByRole('button',{name:'Add source'}) as HTMLButtonElement).disabled).toBe(true)
 expect((screen.getByLabelText('Mapping JSON') as HTMLTextAreaElement).disabled).toBe(true)
 expect((screen.getByRole('button',{name:'Read sources and preview'}) as HTMLButtonElement).disabled).toBe(true)
 fireEvent.click(screen.getByRole('button',{name:'Apply configuration'}))
 await waitFor(()=>expect((screen.getByRole('button',{name:'Add source'}) as HTMLButtonElement).disabled).toBe(false))
 expect(JSON.parse((screen.getByLabelText('Mapping JSON') as HTMLTextAreaElement).value).sources[0].arguments.arguments).toEqual({case:'pending'})
})

it('maps an explicitly selected Drive resource to fileId only for the selected mapping source',async()=>{
 await view({...mapping,sources:[{name:'drive',kind:'selected-file',provider:'google-drive',read:{copy:{facts:[],evidence:[]}}}]})
 fireEvent.click(screen.getByRole('button',{name:'Choose from Google Drive'}))
 expect(prepareMappedInputs).not.toHaveBeenCalled()
 await act(()=>picker.props!.onSelect([{resourceId:'chosen',grant:'a'.repeat(64)}],new AbortController().signal))
 await screen.findByText('Selected')
 fireEvent.click(screen.getByRole('button',{name:'Read sources and preview'}))
 await waitFor(()=>expect(prepareMappedInputs).toHaveBeenCalledWith(expect.objectContaining({selections:{drive:{fileId:'chosen',grant:'a'.repeat(64)}}})))
})

it.each(['complete','cancel','unmount'] as const)('offers explicit reconnect for delayed Drive preview failure and handles %s without losing case edits',async action=>{
 const fixed={...mapping,sources:[{name:'drive',kind:'selected-file' as const,provider:'google-drive' as const,read:{copy:{facts:[],evidence:[]}}}]}
 const ui=await view(fixed)
 fireEvent.click(screen.getByText('Case inputs (JSON)',{selector:'summary span'}));fireEvent.change(screen.getByLabelText('Case inputs (JSON)'),{target:{value:'{"facts":{"score":8}}'}})
 fireEvent.click(screen.getByRole('button',{name:'Choose from Google Drive'}))
 await act(()=>picker.props!.onSelect([{resourceId:'chosen',grant:'a'.repeat(64)}],new AbortController().signal))
 vi.mocked(prepareMappedInputs).mockRejectedValue(new ConnectionRequestError('reconnect-required','google-drive'))
 fireEvent.click(screen.getByRole('button',{name:'Read sources and preview'}))
 const reconnect=await screen.findByRole('button',{name:'Reconnect'})
 expect(authorizeDrive).not.toHaveBeenCalled()
 let finish!:()=>void
 vi.mocked(authorizeDrive).mockReturnValue(new Promise(resolve=>{finish=resolve}))
 fireEvent.click(reconnect)
 const signal=vi.mocked(authorizeDrive).mock.calls[0]![1]
 if(action==='cancel')fireEvent.click(screen.getByRole('button',{name:'Cancel'}))
 if(action==='unmount')ui.unmount()
 if(action!=='complete')expect(signal.aborted).toBe(true)
 await act(async()=>finish())
 if(action==='unmount')return
 expect((screen.getByLabelText('Case inputs (JSON)') as HTMLTextAreaElement).value).toBe('{"facts":{"score":8}}')
 expect(screen.queryByText('Mapped inputs')).toBeNull()
 if(action==='cancel'){expect(screen.getByText('Selected')).toBeTruthy();expect(screen.getByRole('button',{name:'Reconnect'})).toBeTruthy()}
 else {expect(screen.queryByText('Selected')).toBeNull();expect(screen.getByText('This selection expired. Search again and reselect your sources.')).toBeTruthy()}
})
