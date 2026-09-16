import { afterEach, describe, expect, it, vi } from 'vitest'
import { ChatStore, type ChatPersistence } from './store'
import { INITIAL_STATE } from '../research/run'
import type { ResearchRunBinding } from '../research/useResearchRun'

const stores: ChatStore[] = []
afterEach(() => { stores.forEach(store => store.dispose()); stores.length = 0; vi.useRealTimers() })
function setup(io?: Partial<ChatPersistence>) {
  const read = vi.fn(async () => ({ project: '/project', sha256: 'absent', content: { version: 1, chats: [] } }))
  const write = vi.fn(async (document: unknown) => ({ project: '/project', sha256: 'saved', content: document }))
  const store = new ChatStore('/project', { read, write, ...io }); stores.push(store)
  return { store, read, write }
}
describe('project conversation history', () => {
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
