import {dialectRecipeIsEditing} from './recipeEditing'
import {cleanup,fireEvent,render,screen,waitFor} from '@testing-library/react'
import {QueryClient,QueryClientProvider} from '@tanstack/react-query'
import {Tooltip} from 'radix-ui'
import {afterEach,beforeEach,expect,it,vi} from 'vitest'
import {DialectPublication,type DialectPublicationAnswer} from './DialectPublication'
import {dialectAPI,type PreparationReply} from './client'
vi.mock('./client',async original=>({...await original<object>(),dialectAPI:vi.fn()}))
vi.mock('../artifacts/ArtifactVersionOptions',()=>({ArtifactVersionOptions:()=>null}))
const saved={sha256:'a'.repeat(64),document:{id:'prep_'+'b'.repeat(32),name:'Companies'}} as PreparationReply
const checked:DialectPublicationAnswer={publication:{id:'p',at:'2026-10-16',dialect:{preparationId:saved.document.id,revision:saved.sha256,variant:'asserted',request:'e'.repeat(64)},documents:[{id:'urn:test',version:'1.0.0'}]},document:'{"formatVersion":"2.0.0","id":"urn:test","version":"1.0.0","case":{"facts":[],"evidence":[]}}',digest:'sha256:exact',saved:false,basePublication:'',review:{sha256:'d'.repeat(64),bytes:1024}}
let cache:QueryClient
beforeEach(()=>{vi.resetAllMocks();cache=new QueryClient({defaultOptions:{queries:{retry:false}}});vi.mocked(dialectAPI).mockImplementation(async(path,method,body)=>method==='GET'?(path?.includes('review=latest')?{review:null}:{items:[]}) as any:{...checked,saved:(body as any).action==='publish'} as any)})
afterEach(()=>{cleanup();cache.clear()})
async function show(initial=saved,edit=true){const wrap=(value:PreparationReply,disabled=false)=><QueryClientProvider client={cache}><Tooltip.Provider><DialectPublication saved={value} disabled={disabled}/></Tooltip.Provider></QueryClientProvider>;const view=render(wrap(initial));await waitFor(()=>expect((screen.getByLabelText('Reason for this version') as HTMLInputElement).disabled).toBe(false));if(edit){fireEvent.keyDown(screen.getByLabelText('Compatibility impact'),{key:'ArrowDown'});fireEvent.click(await screen.findByRole('option',{name:'First publication'}));fireEvent.change(screen.getByLabelText('Reason for this version'),{target:{value:'Reviewed compatibility'}})}return {...view,change:(value:PreparationReply,disabled=false)=>view.rerender(wrap(value,disabled))}}
it('requires an explicit check and review before publication; saved revision is immutable',async()=>{
 await show();const publish=screen.getByRole('button',{name:'Publish version'}) as HTMLButtonElement;expect(publish.disabled).toBe(true)
 await waitFor(()=>expect((screen.getByRole('button',{name:'Check proposal'}) as HTMLButtonElement).disabled).toBe(false));fireEvent.click(screen.getByRole('button',{name:'Check proposal'}));await waitFor(()=>expect(publish.disabled).toBe(false));fireEvent.click(publish)
 await screen.findByText('Version published')
 const calls=vi.mocked(dialectAPI).mock.calls.filter(([,method])=>method==='POST');expect(calls).toHaveLength(2)
 expect(calls[1][2]).toEqual({action:'publish',revision:saved.sha256,variant:'asserted',version:'1.0.0',impact:{classification:'initial',rationale:'Reviewed compatibility'},reviewed:true,checkDigest:checked.digest,basePublication:'',review:checked.review})
 expect(publish.disabled).toBe(true)
 expect(vi.mocked(dialectAPI).mock.calls.some(([path])=>/research|jobs|evaluate/.test(path??''))).toBe(false)
})
it('invalidates the review when the version or saved recipe changes and blocks dirty edits',async()=>{
 const view=await show();await waitFor(()=>expect((screen.getByRole('button',{name:'Check proposal'}) as HTMLButtonElement).disabled).toBe(false));fireEvent.click(screen.getByRole('button',{name:'Check proposal'}));const publish=screen.getByRole('button',{name:'Publish version'}) as HTMLButtonElement
 await waitFor(()=>expect(publish.disabled).toBe(false));fireEvent.change(screen.getByLabelText('Version'),{target:{value:'1.0.1'}});expect(publish.disabled).toBe(true)
 await waitFor(()=>expect((screen.getByRole('button',{name:'Check proposal'}) as HTMLButtonElement).disabled).toBe(false));fireEvent.click(screen.getByRole('button',{name:'Check proposal'}));await waitFor(()=>expect(publish.disabled).toBe(false));view.change({...saved,sha256:'c'.repeat(64)});expect(publish.disabled).toBe(true)
 view.change(saved,true);expect((screen.getByRole('button',{name:'Check proposal'}) as HTMLButtonElement).disabled).toBe(true)
})
it('preserves errors and never authorizes publication after a refused check',async()=>{
 vi.mocked(dialectAPI).mockImplementation(async(_path,method)=>{if(method==='POST')throw Error('The recipe changed.');return {items:[]} as any})
 await show();await waitFor(()=>expect((screen.getByRole('button',{name:'Check proposal'}) as HTMLButtonElement).disabled).toBe(false));fireEvent.click(screen.getByRole('button',{name:'Check proposal'}));await screen.findByText('The recipe changed.')
 expect((screen.getByRole('button',{name:'Publish version'}) as HTMLButtonElement).disabled).toBe(true)
})

it('restores the exact checked candidate after refresh, without publishing it',async()=>{
 const impact={classification:'patch' as const,rationale:'A compatible conversion fix'}
 vi.mocked(dialectAPI).mockImplementation(async(path,method)=>{
  if(method==='GET'&&path?.includes('review=latest'))return {review:{reference:checked.review,review:{target:`urn:judgmentpack:dialect:${saved.document.id}:asserted`,storageRevision:saved.sha256,proposedVersion:'1.0.1',impact,preview:{...checked,basePublication:'f'.repeat(64)}}}} as any
  return {items:[]} as any
 })
 await show(saved,false)
 await waitFor(()=>expect((screen.getByLabelText('Version') as HTMLInputElement).value).toBe('1.0.1'))
 expect((screen.getByLabelText('Reason for this version') as HTMLInputElement).value).toBe(impact.rationale)
 expect(vi.mocked(dialectAPI).mock.calls.filter(([,method])=>method==='POST')).toHaveLength(0)
 fireEvent.change(screen.getByLabelText('Reason for this version'),{target:{value:'Different review'}})
 expect((screen.getByRole('button',{name:'Publish version'}) as HTMLButtonElement).disabled).toBe(true)
})

it('waits for fresh recovery after an offline cached-null mount',async()=>{
 const {onlineManager}=await import('@tanstack/react-query')
 const {act}=await import('@testing-library/react')
 const impact={classification:'minor' as const,rationale:'Reviewed addition'}
 cache.setQueryData(['dialect-publications',saved.document.id,'asserted',saved.sha256,'review'],{review:null})
 vi.mocked(dialectAPI).mockImplementation(async(path)=>path?.includes('review=latest')?{review:{reference:checked.review,review:{target:`urn:judgmentpack:dialect:${saved.document.id}:asserted`,storageRevision:saved.sha256,proposedVersion:'1.1.0',impact,preview:checked}}} as any:{items:[]} as any)
 onlineManager.setOnline(false)
 try{
  render(<QueryClientProvider client={cache}><Tooltip.Provider><DialectPublication saved={saved}/></Tooltip.Provider></QueryClientProvider>)
  expect((screen.getByLabelText('Version') as HTMLInputElement).disabled).toBe(true)
  expect(vi.mocked(dialectAPI).mock.calls).toHaveLength(0)
  await act(async()=>onlineManager.setOnline(true))
  await waitFor(()=>expect((screen.getByLabelText('Version') as HTMLInputElement).value).toBe('1.1.0'))
  expect((screen.getByLabelText('Reason for this version') as HTMLInputElement).value).toBe(impact.rationale)
  expect(vi.mocked(dialectAPI).mock.calls.every(([,method])=>method==='GET')).toBe(true)
 }finally{onlineManager.setOnline(true)}
})

it('discards manual changes back to the retained check, including an initially empty recovery cache',async()=>{
 let retained:any=null
 vi.mocked(dialectAPI).mockImplementation(async(path,method,body)=>{
  if(method==='GET')return (path?.includes('review=latest')?{review:retained}:{items:[]}) as any
  const request=body as any
  retained={reference:checked.review,review:{target:`urn:judgmentpack:dialect:${saved.document.id}:asserted`,storageRevision:saved.sha256,proposedVersion:request.version,impact:request.impact,preview:checked}}
  return checked as any
 })
 await show()
 fireEvent.change(screen.getByLabelText('Version'),{target:{value:'1.2.3'}})
 fireEvent.click(screen.getByRole('button',{name:'Check proposal'}))
 await waitFor(()=>expect((screen.getByRole('button',{name:'Publish version'}) as HTMLButtonElement).disabled).toBe(false))
 fireEvent.change(screen.getByLabelText('Version'),{target:{value:'2.0.0'}})
 fireEvent.change(screen.getByLabelText('Reason for this version'),{target:{value:'Unfinished change'}})
 fireEvent.click(screen.getByRole('button',{name:'Discard'}))
 await waitFor(()=>expect((screen.getByLabelText('Version') as HTMLInputElement).value).toBe('1.2.3'))
 expect((screen.getByLabelText('Reason for this version') as HTMLInputElement).value).toBe('Reviewed compatibility')
 expect(vi.mocked(dialectAPI).mock.calls.filter(([,method])=>method==='POST')).toHaveLength(1)
})

it('preserves manual review edits when the saved recipe changes and refuses their use',async()=>{
 const view=await show()
 view.change({...saved,sha256:'e'.repeat(64)})
 await screen.findByText('The dialect changed. Refresh and review the saved recipe again.')
 expect((screen.getByLabelText('Reason for this version') as HTMLInputElement).value).toBe('Reviewed compatibility')
 expect((screen.getByRole('button',{name:'Check proposal'}) as HTMLButtonElement).disabled).toBe(true)
 expect((screen.getByRole('button',{name:'Publish version'}) as HTMLButtonElement).disabled).toBe(true)
 expect(vi.mocked(dialectAPI).mock.calls.every(([,method])=>method==='GET')).toBe(true)
})

it('guards competing chat publication while the manual compatibility review has unsaved edits',async()=>{
 await show(saved,false);expect(dialectRecipeIsEditing(saved.document.id)).toBe(false)
 fireEvent.change(screen.getByLabelText('Reason for this version'),{target:{value:'Manual change under review'}})
 expect(dialectRecipeIsEditing(saved.document.id)).toBe(true)
 fireEvent.click(screen.getByRole('button',{name:'Discard'}))
 await waitFor(()=>expect(dialectRecipeIsEditing(saved.document.id)).toBe(false))
})
