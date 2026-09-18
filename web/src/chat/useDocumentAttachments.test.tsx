import { act, cleanup, renderHook } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import { ChatStore, retainSentDocuments } from './store'
import { useChatAttachments } from './useChatAttachments'
import { DESK_DEFAULTS, DOCUMENT_DEFAULTS } from '../config/deskConfig'
import { signedDocument } from '../documents/__fixtures__/signedDocument'
import { verifyDocument } from '../documents/client'
import { readDocumentRecord } from '../documents/record'
import partial from '../documents/__fixtures__/partial-ocr-failed.json'
const mocked = vi.hoisted(() => ({ ingest: vi.fn(), load: vi.fn(), pick: vi.fn(), drive: vi.fn() }))
vi.mock('../documents/client', async original => ({...await original<typeof import('../documents/client')>(), ingestDocument:mocked.ingest,loadDocument:mocked.load,ingestDrive:mocked.drive}))
vi.mock('../connections/client', () => ({ authorizeDrive: mocked.pick }))
const stores: ChatStore[] = []
afterEach(()=>{cleanup();stores.forEach(s=>s.dispose());stores.length=0;sessionStorage.clear();vi.clearAllMocks()})
async function setup(enabled = true) {
 const {object,pin,reference} = await signedDocument(), document = await verifyDocument(object,pin)
 const config = {...DESK_DEFAULTS.research,gateway:pin,documents:{...DOCUMENT_DEFAULTS,enabled}}
 const store = new ChatStore('/isolated',{read:async()=>({project:'/isolated',sha256:'absent',content:{version:1,chats:[]}}),write:vi.fn()});stores.push(store)
 await store.load(); const chat=store.startChat()
 mocked.ingest.mockResolvedValue({reference,document});mocked.load.mockResolvedValue(document)
 const hook=renderHook(({id})=>useChatAttachments(store,id,false,config),{initialProps:{id:chat.id}})
 const file={name:'policy.pdf',size:100,arrayBuffer:vi.fn()} as unknown as File
 return { ...hook,store,chat,file,reference,document }
}
it('blocks new PDF uploads when processing is off while still verifying previously saved documents', async () => {
 const s=await setup(false); await act(()=>s.result.current.attach([s.file]))
 expect(mocked.ingest).not.toHaveBeenCalled()
 expect(s.result.current.error).toContain('Enable PDF processing')
 let prompt:string|undefined
 await act(async()=>{prompt=await s.result.current.prepare([{id:s.reference.id,name:'policy.pdf',text:'',document:s.reference}])})
 expect(prompt).toContain('selectedPages')
 expect(mocked.load).toHaveBeenCalledOnce()
})
it('stores only a reference in an unsent home draft',async()=>{
 const s=await setup();await act(()=>s.result.current.attach([s.file]))
 expect(s.store.getSnapshot().chats).toHaveLength(0)
 expect(s.store.getSnapshot().drafts[0]!.attachments![0]).toEqual({id:s.reference.id,name:'policy.pdf',text:'',document:s.reference})
})
it('keeps previously cited pages when the same document is used in a later turn', async () => {
 const {reference} = await signedDocument()
 const earlier = {id:reference.id,name:'policy.pdf',text:'',document:{...reference,pages:[1],allowPartial:true}}
 const later = {...earlier,document:{...reference,pages:[3]}}
 const saved = retainSentDocuments([earlier],[later,{id:'text',name:'notes.txt',text:'reference'}])
 expect(saved).toHaveLength(1)
 expect(saved[0]!.document).toMatchObject({pages:[1,3],allowPartial:true})
 expect(later.document.pages).toEqual([3])
})
it.each(['cancel','switch','unmount'] as const)('discards a gateway completion after %s',async how=>{
 const s=await setup();let finish!:(value:unknown)=>void
 mocked.ingest.mockReturnValue(new Promise(resolve=>{finish=resolve}))
 let work!:Promise<void>;act(()=>{work=s.result.current.attach([s.file])})
 const signal=mocked.ingest.mock.calls[0]![2] as AbortSignal
 if(how==='cancel')act(()=>s.result.current.cancel())
 if(how==='switch'){const other=s.store.startChat({id:'other',path:'other.json',digest:'x'});s.rerender({id:other.id})}
 if(how==='unmount')s.unmount()
 expect(signal.aborted).toBe(true)
 await act(async()=>{finish({reference:s.reference,document:s.document});await work})
 expect(s.store.getSnapshot().drafts.every(chat=>!chat.attachments?.length)).toBe(true)
})
it('requires consent before using partial extraction and re-verifies before every send',async()=>{
 const s=await setup();s.document.record=readDocumentRecord(partial)
 const file={id:s.reference.id,name:'policy.pdf',text:'',document:s.reference}
 let prompt:string|undefined
 await act(async()=>{prompt=await s.result.current.prepare([file])})
 expect(prompt).toBeUndefined();expect(s.result.current.error).toMatch(/confirmation/)
 await act(async()=>{prompt=await s.result.current.prepare([{...file,document:{...s.reference,allowPartial:true}}])})
 expect(prompt).toContain('selectedPages');expect(prompt).not.toContain('"page":2')
 mocked.load.mockRejectedValue(new Error('tampered receipt'))
 await act(async()=>{prompt=await s.result.current.prepare([file])})
 expect(prompt).toBeUndefined();expect(mocked.load).toHaveBeenCalledTimes(3)
})

it('refuses Drive selection before consent when document processing is disabled', async () => {
 const s = await setup(false)
 await act(() => s.result.current.attachDrive())
 expect(mocked.pick).not.toHaveBeenCalled()
 expect(mocked.drive).not.toHaveBeenCalled()
 expect(s.result.current.error).toContain('Enable document processing')
})
it.each(['cancel', 'switch', 'unmount'] as const)('does not attach a Drive result after %s', async how => {
 const s = await setup()
 mocked.pick.mockResolvedValue([{fileId:'selected-file',grant:'ab'.repeat(32)}])
 let finish!: (value: unknown) => void
 mocked.drive.mockReturnValue(new Promise(resolve => { finish = resolve }))
 let work!: Promise<void>
 await act(async () => { work = s.result.current.attachDrive(); await Promise.resolve() })
 const signal = mocked.drive.mock.calls[0]![2] as AbortSignal
 if (how === 'cancel') act(() => s.result.current.cancel())
 if (how === 'switch') {const other=s.store.startChat({id:'other',path:'other.json',digest:'x'});s.rerender({id:other.id})}
 if (how === 'unmount') s.unmount()
 expect(signal.aborted).toBe(true)
 await act(async () => {finish({reference:s.reference,document:s.document});await work})
 expect(s.store.getSnapshot().drafts.every(chat => !chat.attachments?.length)).toBe(true)
})
