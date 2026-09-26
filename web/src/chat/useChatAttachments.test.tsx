import { memoryDraftPersistence } from '../testing/draftPersistence'
import { act, cleanup, renderHook } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import { ChatStore } from './store'
import { useChatAttachments } from './useChatAttachments'

const stores: ChatStore[] = []
afterEach(() => { cleanup(); stores.forEach(store => store.dispose()); stores.length = 0; sessionStorage.clear() })
async function setup() {
  const write = vi.fn()
  const store = new ChatStore('/project', { read: async () => ({ project: '/project', sha256: 'absent', content: { version: 1, chats: [] } }), write }, memoryDraftPersistence('/project'))
  stores.push(store)
  await store.load()
  const chat = store.startChat()
  const hook = renderHook(({ id, disabled }) => useChatAttachments(store, id, disabled), { initialProps: { id: chat.id, disabled: false } })
  const files = () => store.getSnapshot().drafts.find(item => item.id === chat.id)?.attachments ?? []
  return { ...hook, store, chat, files, write }
}
function file(name: string, text = 'Policy text') {
  const bytes = new TextEncoder().encode(text).buffer
  return { name, size: bytes.byteLength, arrayBuffer: vi.fn(async () => bytes) } as unknown as File
}
function pendingFile() {
  let finish!: (bytes: ArrayBuffer) => void
  const pending = new Promise<ArrayBuffer>(resolve => { finish = resolve })
  const entry = file('slow.txt')
  vi.mocked(entry.arrayBuffer).mockReturnValue(pending)
  return { entry, finish: () => finish(new TextEncoder().encode('Read completely').buffer) }
}

it('keeps valid files separate from the composer and out of history until Send', async () => {
  const { result, store, chat, files, write } = await setup()
  act(() => store.update(chat.id, { composer: 'My question' }))
  await act(() => result.current.attach([file('policy.md'), file('inputs.json', '{}')]))
  expect(files().map(item => item.name)).toEqual(['policy.md', 'inputs.json'])
  expect(store.getSnapshot().drafts[0]?.composer).toBe('My question')
  expect(store.getSnapshot().chats).toHaveLength(0)
  expect(write).not.toHaveBeenCalled()
})

it('blocks a second batch while reading and cancellation discards its late result', async () => {
  const { result, files } = await setup()
  const pending = pendingFile()
  let work!: Promise<void>
  act(() => { work = result.current.attach([pending.entry]); expect(result.current.isReading()).toBe(true) })
  expect(result.current.reading).toBe(true)
  const ignored = file('ignored.txt')
  await act(() => result.current.attach([ignored]))
  expect(ignored.arrayBuffer).not.toHaveBeenCalled()
  act(() => result.current.cancel())
  await act(() => result.current.attach([file('replacement.txt')]))
  await act(async () => { pending.finish(); await work })
  expect(files().map(item => item.name)).toEqual(['replacement.txt'])
  expect(result.current.isReading()).toBe(false)
  expect(result.current.error).toBe('')
})

it.each(['switch', 'unmount', 'lock'] as const)('discards a pending read on %s', async action => {
  const { result, store, chat, files, rerender, unmount } = await setup()
  const pending = pendingFile()
  let work!: Promise<void>
  act(() => { work = result.current.attach([pending.entry]) })
  if (action === 'switch') {
    const other = store.startChat({ id: 'other', path: 'other.json', digest: 'digest' })
    rerender({ id: other.id, disabled: false })
  } else if (action === 'lock') rerender({ id: chat.id, disabled: true })
  else unmount()
  await act(async () => { pending.finish(); await work })
  expect(files()).toEqual([])
  expect(store.getSnapshot().drafts.every(item => !item.attachments?.length)).toBe(true)
})

it('does not restore a file removed while another is reading', async () => {
  const { result, store, chat, files } = await setup()
  await act(() => result.current.attach([file('removed.txt')]))
  const pending = pendingFile()
  let work!: Promise<void>
  act(() => { work = result.current.attach([pending.entry]); store.update(chat.id, { attachments: [] }) })
  await act(async () => { pending.finish(); await work })
  expect(files().map(item => item.name)).toEqual(['slow.txt'])
})

it.each([
  { name: 'unsupported type', files: () => [file('valid.txt'), file('policy.pdf')], error: /Enable PDF processing/ },
  { name: 'too many', files: () => Array.from({ length: 5 }, (_, i) => file(`${i}.txt`)), error: /four files/ },
  { name: 'too large', files: () => [file('large.txt', 'x'.repeat(200_001))], error: /200 KB/ }
])('rejects $name before reading any file', async scenario => {
  const { result, files } = await setup()
  const chosen = scenario.files()
  await act(() => result.current.attach(chosen))
  expect(result.current.error).toMatch(scenario.error)
  expect(files()).toEqual([])
  for (const entry of chosen) expect(entry.arrayBuffer).not.toHaveBeenCalled()
  expect(result.current.reading).toBe(false)
})

it.each(['invalid-utf8', 'binary'])('rejects the whole batch for %s content', async content => {
  const { result, files } = await setup()
  const bad = file('broken.txt', 'binary\0bytes')
  if (content === 'invalid-utf8') vi.mocked(bad.arrayBuffer).mockResolvedValue(new Uint8Array([0xff]).buffer)
  await act(() => result.current.attach([file('valid.txt'), bad]))
  expect(files()).toEqual([])
  expect(result.current.error).toContain('broken.txt')
  expect(result.current.isReading()).toBe(false)
})

it('delivers files to a test case without a chat and discards a late read after changing cases', async () => {
  let selected: import('./store').ChatAttachment[] = []
  const append = vi.fn((files: import('./store').ChatAttachment[]) => { selected = [...selected,...files] })
  const hook = renderHook(({id}) => useChatAttachments(null, '', false, undefined, {kind:'test-case',id,current:()=>selected,append}), {initialProps:{id:'case-one'}})
  await act(() => hook.result.current.attach([file('case-source.txt')]))
  expect(selected.map(s=>s.name)).toEqual(['case-source.txt'])
  const pending=pendingFile()
  let completion!:Promise<void>
  act(()=>{completion=hook.result.current.attach([pending.entry])})
  hook.rerender({id:'case-two'})
  await act(async()=>{pending.finish();await completion})
  expect(append).toHaveBeenCalledTimes(1)
  expect(selected).toHaveLength(1)
})
