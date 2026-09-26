import { memoryDraftPersistence } from '../testing/draftPersistence'
// Adapted from the independent D2 reproduction recorded on PR #110.
import { act, cleanup, renderHook } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import { ChatStore } from './store'
import { useChatAttachments } from './useChatAttachments'
import { DESK_DEFAULTS, DOCUMENT_DEFAULTS } from '../config/deskConfig'
const mocked = vi.hoisted(() => ({ pick: vi.fn(), ingest: vi.fn() }))
vi.mock('../connections/client', () => ({ authorizeDrive: mocked.pick }))
vi.mock('../documents/client', () => ({ ingestDrive: mocked.ingest, ingestDocument: vi.fn(), loadDocument: vi.fn(), documentContext: vi.fn() }))
afterEach(() => { cleanup(); vi.clearAllMocks(); sessionStorage.clear() })
it.each(['disabled', 'external'] as const)('cancels pending selection when document configuration becomes %s', async mode => {
 const store = new ChatStore('/review', {read:async()=>({project:'/review',sha256:'absent',content:{version:1,chats:[]}}),write:vi.fn()}, memoryDraftPersistence('/review'))
 await store.load()
 const chat = store.startChat()
 const config = {...DESK_DEFAULTS.research, gateway:{url:'http://127.0.0.1:9001',authority:'gateway:desk-local',signer:{algorithm:'ed25519' as const,public:'ab'.repeat(32)}},documents:{...DOCUMENT_DEFAULTS,enabled:true}}
 const hook = renderHook(({current}) => useChatAttachments(store, chat.id, false, current), {initialProps:{current:config}})
 let finish!: (value: unknown) => void
 mocked.pick.mockReturnValue(new Promise(resolve => { finish = resolve }))
 mocked.ingest.mockRejectedValue(new Error('review stopped before network'))
 let work!: ReturnType<ReturnType<typeof useChatAttachments>['attachDrive']>
 act(() => { work = hook.result.current.attachDrive() })
 const signal = mocked.pick.mock.calls[0]![1] as AbortSignal
 const changed = mode === 'disabled' ? {...config,documents:{...config.documents,enabled:false}} : {...config,gateway:{...config.gateway,url:'https://external.example',authority:'gateway:external'}}
 hook.rerender({current:changed})
 await act(async () => { finish([{fileId:'synthetic-file',grant:'aa'.repeat(32)}]); await work })
 store.dispose()
 expect(signal.aborted).toBe(true)
 expect(mocked.ingest).not.toHaveBeenCalled()
})

it('imports a first-use picker result without opening Google authorization a second time', async () => {
 const store = new ChatStore('/review', {read:async()=>({project:'/review',sha256:'absent',content:{version:1,chats:[]}}),write:vi.fn()}, memoryDraftPersistence('/review'))
 await store.load()
 const chat = store.startChat()
 const config = {...DESK_DEFAULTS.research, gateway:{url:'http://127.0.0.1:9001',authority:'gateway:desk-local',signer:{algorithm:'ed25519' as const,public:'ab'.repeat(32)}},documents:{...DOCUMENT_DEFAULTS,enabled:true}}
 const hook = renderHook(() => useChatAttachments(store, chat.id, false, config))
 const selected = {fileId:'selected-file',grant:'aa'.repeat(32)}
 mocked.ingest.mockResolvedValue({reference:{id:'retained-document'},document:{record:{document:{name:'Selected policy.pdf'}}}})
 await act(async () => { await hook.result.current.attachDrive([selected]) })
 expect(mocked.pick).not.toHaveBeenCalled()
 expect(mocked.ingest).toHaveBeenCalledWith(selected, config, expect.any(AbortSignal), expect.any(Function))
 const saved = store.getSnapshot().drafts.find(item => item.id === chat.id)!
 expect(saved.attachments?.map(item => item.name)).toEqual(['Selected policy.pdf'])
 expect(JSON.stringify(saved)).not.toContain(selected.grant)
 store.dispose()
})
