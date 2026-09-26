import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import { MemoryRouter } from 'react-router-dom'
import { SourceRefreshReview } from './SourceRefreshReview'
import { signedDocument } from './__fixtures__/signedDocument'
import { verifyDocument } from './client'
const mocks=vi.hoisted(()=>({config:{} as any,read:vi.fn(),save:vi.fn(),ingest:vi.fn(),load:vi.fn(),open:vi.fn()}))
vi.mock('../config/DeskConfigProvider',()=>({useEffectiveConfig:()=>({config:{research:mocks.config}})}))
vi.mock('../connections/ConnectionPaneContext',()=>({useConnectionsPane:()=>({open:mocks.open})}))
vi.mock('../chat/ChatProvider',()=>({useChats:()=>({packDrafts:[]})}))
vi.mock('../packs/test-workspace/store',()=>({readTests:async()=>({content:{version:1,suites:{}}})}))
vi.mock('./refresh',async original=>({...await original<typeof import('./refresh')>(),readReviews:mocks.read,saveReview:mocks.save}))
vi.mock('./client',async original=>({...await original<typeof import('./client')>(),ingestLink:mocks.ingest,loadDocument:mocks.load}))
afterEach(()=>{cleanup();vi.clearAllMocks()})
async function setup(kind:'web'|true='web'){
 const f=await signedDocument(kind),before=await verifyDocument(f.object,f.pin),after=structuredClone(before)
 after.record.content.pages[0]!.text='Updated policy';after.digest='sha256:'+'b'.repeat(64)
 const reference={...f.reference,id:'87654321-1234-1234-1234-123456789abc',digest:after.digest}
 mocks.config={gateway:f.pin,documents:{enabled:true}}
 mocks.read.mockResolvedValue({project:'/fixture',content:{version:1,reviews:[]}})
 mocks.save.mockImplementation(async r=>({content:{version:1,reviews:[r]}}))
 mocks.ingest.mockResolvedValue({reference,document:after});mocks.load.mockResolvedValue(after)
 return {f,before,after,reference}
}
it('retains a new snapshot for review, never applies automatically, and reports save errors without losing the comparison',async()=>{
 const {f,before,reference}=await setup();const onUse=vi.fn()
 mocks.save.mockRejectedValueOnce(Error('Storage conflict'))
 render(<MemoryRouter><SourceRefreshReview before={before} reference={f.reference} onUse={onUse}/></MemoryRouter>)
 await waitFor(()=>expect((screen.getByRole('button',{name:'Check for source changes'}) as HTMLButtonElement).disabled).toBe(false))
 fireEvent.click(screen.getByRole('button',{name:'Check for source changes'}))
 await screen.findByText('Source text changed')
 expect(onUse).not.toHaveBeenCalled();expect(screen.getByText('Storage conflict')).toBeTruthy()
 fireEvent.click(screen.getByRole('button',{name:'Save refresh review'}))
 await waitFor(()=>expect(screen.queryByText('Storage conflict')).toBeNull())
 fireEvent.click(screen.getByRole('button',{name:'Use snapshot in this case'}))
 await waitFor(()=>expect(onUse).toHaveBeenCalledOnce())
 expect(onUse.mock.calls[0]![0].document.id).toBe(reference.id)
 expect(f.reference.id).not.toBe(reference.id)
 expect(before.record.content.pages[0]!.text).not.toBe('Updated policy')
})
it('reselects connected sources instead of reusing an expired grant and refuses another resource',async()=>{
 const {f,before,after,reference}=await setup(true)
 render(<MemoryRouter><SourceRefreshReview before={before} reference={f.reference}/></MemoryRouter>)
 const button=screen.getByRole('button',{name:'Choose latest from connection'})
 await waitFor(()=>expect((button as HTMLButtonElement).disabled).toBe(false));fireEvent.click(button)
 const request=mocks.open.mock.calls[0]![0]
 expect(request.provider).toBe('google-drive');expect(request.destination.kind).toBe('source-refresh')
 after.record.provenance.source.fileId='other-file'
 await expect(request.destination.append([{id:reference.id,name:'Other',text:'',document:reference}])).rejects.toThrow('same source')
 expect(mocks.save).not.toHaveBeenCalled()
})
it('ignores late delivery after the source reader is closed',async()=>{
 const {f,before,reference}=await setup(true)
 const view=render(<MemoryRouter><SourceRefreshReview before={before} reference={f.reference}/></MemoryRouter>)
 const button=screen.getByRole('button',{name:'Choose latest from connection'})
 await waitFor(()=>expect((button as HTMLButtonElement).disabled).toBe(false));fireEvent.click(button)
 const dest=mocks.open.mock.calls[0]![0].destination;view.unmount()
 expect(dest.current()).toBeUndefined();await dest.append([{id:reference.id,name:'Source',text:'',document:reference}])
 expect(mocks.save).not.toHaveBeenCalled()
})
