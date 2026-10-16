import {cleanup,fireEvent,render,screen,waitFor} from '@testing-library/react'
import {QueryClient,QueryClientProvider} from '@tanstack/react-query'
import {Tooltip} from 'radix-ui'
import {afterEach,beforeEach,expect,it,vi} from 'vitest'
import {DialectPublication,type DialectPublicationAnswer} from './DialectPublication'
import {dialectAPI,type PreparationReply} from './client'
vi.mock('./client',async original=>({...await original<object>(),dialectAPI:vi.fn()}))
vi.mock('../artifacts/ArtifactVersionOptions',()=>({ArtifactVersionOptions:()=>null}))
const saved={sha256:'a'.repeat(64),document:{id:'prep_'+'b'.repeat(32),name:'Companies'}} as PreparationReply
const checked:DialectPublicationAnswer={publication:{id:'p',at:'2026-10-16',dialect:{preparationId:saved.document.id,revision:saved.sha256,variant:'asserted'},documents:[{id:'urn:test',version:'1.0.0'}]},document:'{"formatVersion":"2.0.0","id":"urn:test","version":"1.0.0","case":{"facts":[],"evidence":[]}}',digest:'sha256:exact',saved:false}
let cache:QueryClient
beforeEach(()=>{vi.resetAllMocks();cache=new QueryClient({defaultOptions:{queries:{retry:false}}});vi.mocked(dialectAPI).mockImplementation(async(_path,method,body)=>method==='GET'?{items:[]} as any:{...checked,saved:(body as any).action==='publish'} as any)})
afterEach(()=>{cleanup();cache.clear()})
function show(initial=saved){const wrap=(value:PreparationReply,disabled=false)=><QueryClientProvider client={cache}><Tooltip.Provider><DialectPublication saved={value} disabled={disabled}/></Tooltip.Provider></QueryClientProvider>;const view=render(wrap(initial));return {...view,change:(value:PreparationReply,disabled=false)=>view.rerender(wrap(value,disabled))}}
it('requires an explicit check and review before publication; saved revision is immutable',async()=>{
 show();const publish=screen.getByRole('button',{name:'Publish version'}) as HTMLButtonElement;expect(publish.disabled).toBe(true)
 fireEvent.click(screen.getByRole('button',{name:'Check proposal'}));await waitFor(()=>expect(publish.disabled).toBe(false));fireEvent.click(publish)
 await screen.findByText('Version published')
 const calls=vi.mocked(dialectAPI).mock.calls.filter(([,method])=>method==='POST');expect(calls).toHaveLength(2)
 expect(calls[1][2]).toEqual({action:'publish',revision:saved.sha256,variant:'asserted',version:'1.0.0',reviewed:true,checkDigest:checked.digest})
 expect(publish.disabled).toBe(true)
 expect(vi.mocked(dialectAPI).mock.calls.some(([path])=>/research|jobs|evaluate/.test(path??''))).toBe(false)
})
it('invalidates the review when the version or saved recipe changes and blocks dirty edits',async()=>{
 const view=show();fireEvent.click(screen.getByRole('button',{name:'Check proposal'}));const publish=screen.getByRole('button',{name:'Publish version'}) as HTMLButtonElement
 await waitFor(()=>expect(publish.disabled).toBe(false));fireEvent.change(screen.getByLabelText('Version'),{target:{value:'1.0.1'}});expect(publish.disabled).toBe(true)
 fireEvent.click(screen.getByRole('button',{name:'Check proposal'}));await waitFor(()=>expect(publish.disabled).toBe(false));view.change({...saved,sha256:'c'.repeat(64)});expect(publish.disabled).toBe(true)
 view.change(saved,true);expect((screen.getByRole('button',{name:'Check proposal'}) as HTMLButtonElement).disabled).toBe(true)
})
it('preserves errors and never authorizes publication after a refused check',async()=>{
 vi.mocked(dialectAPI).mockImplementation(async(_path,method)=>{if(method==='POST')throw Error('The recipe changed.');return {items:[]} as any})
 show();fireEvent.click(screen.getByRole('button',{name:'Check proposal'}));await screen.findByText('The recipe changed.')
 expect((screen.getByRole('button',{name:'Publish version'}) as HTMLButtonElement).disabled).toBe(true)
})
