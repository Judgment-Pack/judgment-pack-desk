import { afterEach, describe, expect, it, vi } from 'vitest'
import { ChatStore, type ChatPersistence } from './store'
import { INITIAL_STATE } from '../research/run'
import type { ResearchRunBinding } from '../research/useResearchRun'

const stores: ChatStore[] = []
afterEach(() => { stores.forEach(store => store.dispose()); stores.length = 0; sessionStorage.clear(); vi.useRealTimers() })
function setup(io?: Partial<ChatPersistence>) {
  const read = vi.fn(async () => ({ project: '/project', sha256: 'absent', content: { version: 1, chats: [] } }))
  const write = vi.fn(async (document: unknown) => ({ project: '/project', sha256: 'saved', content: document }))
  const store = new ChatStore('/project', { read, write, ...io }); stores.push(store)
  return { store, read, write }
}
describe('project conversation history', () => {
  it('keeps home visits, typing and New chat out of history until the first accepted Send', async () => {
    const { store, write } = setup(); await store.load()
    const draft = store.startChat()
    store.update(draft.id, { composer: 'A decision brief', model: 'model-a' })
    expect(store.startChat().id).toBe(draft.id)
    await store.flush()
    expect(store.getSnapshot().chats).toHaveLength(0)
    expect(store.getSnapshot().dirty).toBe(false)
    expect(write).not.toHaveBeenCalled()

    const restored = setup(); await restored.store.load()
    expect(restored.store.startChat()).toMatchObject({ id: draft.id, composer: 'A decision brief', model: 'model-a' })
    expect(restored.store.getSnapshot().chats).toHaveLength(0)

    const run = { running: false, stop: vi.fn() }
    const binding = { state: INITIAL_STATE, sources: [], blocked: 'Configure Assistant', model: 'model-a', researchConfigured: false, run, ledger: null } as unknown as ResearchRunBinding
    store.report(draft.id, binding)
    const send = vi.fn()
    expect(store.perform(draft.id, send)).toBe(false)
    expect(store.getSnapshot().chats).toHaveLength(0)
    expect(send).not.toHaveBeenCalled()
    store.report(draft.id, { ...binding, blocked: '' })
    expect(store.perform(draft.id, send)).toBe(true)
    expect(store.perform(draft.id, send)).toBe(true)
    expect(store.getSnapshot().chats.map(chat => chat.id)).toEqual([draft.id])
    expect(store.getSnapshot().drafts).toHaveLength(0)
    await store.flush()
    expect(write).toHaveBeenCalledOnce()
    expect((write.mock.calls[0]![0] as { chats: unknown[] }).chats).toHaveLength(1)

    const fresh = store.startChat(undefined, undefined, true)
    expect(fresh.id).not.toBe(draft.id)
    await store.flush()
    expect(store.getSnapshot().chats).toHaveLength(1)
    expect(write).toHaveBeenCalledOnce()
  })

  it('does not confuse a different project’s unsent home draft with this project', async () => {
    const { store } = setup(); await store.load()
    const draft = store.startChat(); store.update(draft.id, { composer: 'Private to this project' })
    const other = new ChatStore('/other', { read: async () => ({ project: '/other', sha256: 'absent', content: { version: 1, chats: [] } }), write: vi.fn() })
    stores.push(other); await other.load()
    expect(other.startChat().composer).toBe('')
    expect(other.getSnapshot().chats).toHaveLength(0)
  })
  it('preserves composer and model separately for each chat and links creation without replacing history', async () => {
    const { store, write } = setup(); await store.load()
    const first = store.create(); store.update(first.id,{ composer: 'My unfinished prompt', model: 'model-a' })
    const second = store.create(); store.update(second.id,{ composer: 'A different prompt' })
    store.activate(first.id)
    expect(store.getSnapshot().chats.find(chat => chat.id === first.id)).toMatchObject({ composer: 'My unfinished prompt', model: 'model-a' })
    store.update(first.id,{ pack: { id: 'my-pack', path: 'packs/my-pack.pack.json', digest: 'abc' } })
    await store.flush()
    const saved = write.mock.calls[0]![0] as { chats: { id: string; composer: string }[] }
    expect(saved.chats).toHaveLength(2)
    expect(saved.chats.find(chat => chat.id === first.id)?.composer).toBe('My unfinished prompt')
    expect(store.getSnapshot().dirty).toBe(false)
  })
  it('switching chats does not stop the running task; a second task cannot start', async () => {
    const { store } = setup(); await store.load()
    const a = store.create(), b = store.create()
    const stop = vi.fn()
    let running = false
    const binding = { state: INITIAL_STATE, sources: [], blocked: '', model: 'model', researchConfigured: false,
      run: { get running() { return running }, stop }, ledger: null } as unknown as ResearchRunBinding
    store.report(a.id,binding); store.report(b.id,{ ...binding, run: { running: false, stop: vi.fn() } as unknown as ResearchRunBinding['run'] })
    expect(store.perform(a.id, () => { running = true })).toBe(true)
    store.activate(b.id)
    const second = vi.fn(); expect(store.perform(b.id,second)).toBe(false)
    expect(second).not.toHaveBeenCalled(); expect(stop).not.toHaveBeenCalled()
    expect(() => store.remove(a.id)).toThrow('Stop')
    running = false
    expect(store.perform(b.id,second)).toBe(true)
  })
  it('does not overwrite unreadable history or claim saving succeeded after a conflict', async () => {
    const bad = setup({ read: async () => ({ project: '/other-project', sha256: 'x', content: { version: 1, chats: [] } }) })
    await bad.store.load(); expect(bad.store.getSnapshot().ready).toBe(false)
    expect(() => bad.store.create()).toThrow(); expect(await bad.store.flush()).toBe(false); expect(bad.write).not.toHaveBeenCalled()
    const { store } = setup({ write: async () => { throw new Error('history changed in another window') } })
    await store.load(); store.create(); expect(await store.flush()).toBe(false)
    expect(store.getSnapshot()).toMatchObject({ dirty: true, saving: false })
    expect(store.getSnapshot().error).toContain('not saved')
  })
  it('writes newer edits after the in-flight checkpoint, without advancing its saved revision early', async () => {
    vi.useFakeTimers()
    let release!: (reply: Awaited<ReturnType<ChatPersistence['write']>>) => void
    const write = vi.fn<ChatPersistence['write']>().mockImplementationOnce(() => new Promise(resolve => { release = resolve }))
      .mockImplementation(async document => ({ project: '/project', sha256: 'second', content: document }))
    const { store } = setup({ write }); await store.load()
    const chat = store.create(); const saving = store.flush()
    store.update(chat.id,{ composer: 'Typed while saving' })
    release({ project: '/project', sha256: 'first', content: { version: 1, chats: [] } }); await saving
    expect(store.getSnapshot().dirty).toBe(true)
    await vi.advanceTimersByTimeAsync(601)
    expect(write.mock.calls[1]![1]).toBe('first')
    expect(write.mock.calls[1]![0].chats[0]?.composer).toBe('Typed while saving')
    expect(store.getSnapshot().dirty).toBe(false)
  })
  it('rejects duplicate conversation identifiers and keeps the file untouched', async () => {
    const base = setup(); await base.store.load(); const chat = base.store.create()
    const { store, write } = setup({ read: async () => ({ project: '/project', sha256: 'x', content: { version: 1, chats: [chat, chat] } }) })
    await store.load(); expect(store.getSnapshot().ready).toBe(false); expect(write).not.toHaveBeenCalled()
  })
})
it('keeps recency stable for opening, restoring, typing, pinning and switching models', async () => {
  vi.useFakeTimers()
  vi.setSystemTime(new Date('2026-09-01T12:00:00Z'))
  const { store } = setup(); await store.load()
  const chat = store.create()
  vi.setSystemTime(new Date('2026-09-16T12:00:00Z'))
  store.activate(chat.id)
  store.update(chat.id, { pinned: true, composer: 'Unsent', model: 'different' })
  expect(store.getSnapshot().chats[0]!.updatedAt).toBe(chat.updatedAt)
  const state = { ...INITIAL_STATE, phase: 'conversation' as const, status: 'complete' as const, turns: [{ role: 'assistant' as const, kind: 'message' as const, text: 'Saved response', at: '2026-09-01T12:00:01Z' }] }
  const binding = { state, sources: [], blocked: '', model: 'model', researchConfigured: false, run: null, ledger: null }
  store.report(chat.id, binding)
  expect(store.getSnapshot().chats[0]!.updatedAt).toBe('2026-09-01T12:00:01Z')
  store.report(chat.id, { ...binding, state: { ...state } })
  expect(store.getSnapshot().chats[0]!.updatedAt).toBe('2026-09-01T12:00:01Z')
})
it('keeps attachments separate from message text and restores an unsent attachment', async () => {
  const { store } = setup(); await store.load()
  const chat = store.startChat()
  store.update(chat.id, { composer: 'Read this', attachments: [{ id: 'file-one', name: 'requirements.md', text: '# Policy\nExample' }] })
  const restored = setup(); await restored.store.load()
  expect(restored.store.startChat()).toMatchObject({ composer: 'Read this', attachments: [{ id: 'file-one', name: 'requirements.md', text: '# Policy\nExample' }] })
  store.update(chat.id, { attachments: [] })
  expect(store.getSnapshot().drafts[0]!.composer).toBe('Read this')
})
it('allows upgrading a draft to research but never downgrading a research candidate to bypass its checks', async () => {
  const { store } = setup(); await store.load()
  const chat = store.create()
  store.report(chat.id, { state: { ...INITIAL_STATE, candidates: [{} as any] }, sources: [], blocked: '', model: 'model', researchConfigured: true, run: null, ledger: null })
  store.update(chat.id, { mode: 'research' })
  expect(store.getSnapshot().chats[0]!.mode).toBe('research')
  store.update(chat.id, { mode: 'draft' })
  expect(store.getSnapshot().chats[0]!.mode).toBe('research')
})
