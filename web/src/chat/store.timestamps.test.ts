import { afterEach, expect, it, vi } from 'vitest'
import { ChatStore, type Chat, type ChatPersistence } from './store'
import { checkpoint } from './checkpoint'
import { INITIAL_STATE, type Turn } from '../research/run'
import type { ResearchRunBinding } from '../research/useResearchRun'
const stores: ChatStore[] = []
afterEach(() => { stores.forEach(store => store.dispose()); stores.length = 0; sessionStorage.clear(); vi.useRealTimers() })
const binding = { state: INITIAL_STATE, sources: [], blocked: '', run: { running: false, stop: vi.fn() } } as unknown as ResearchRunBinding
function setup(chats: Chat[] = []) {
  const write = vi.fn(async (content: unknown) => ({ project: '/project', sha256: 'saved', content }))
  const io: ChatPersistence = { read: async () => ({ project: '/project', sha256: 'initial', content: { version: 1, chats } }), write }
  const store = new ChatStore('/project', io); stores.push(store)
  return { store, write }
}
const legacy: Chat = { id: 'legacy', title: 'Older chat', composer: '', pinned: false, archived: false, updatedAt: '2026-09-20T12:00:00.000Z', model: '', mode: 'draft', view: 'chat' }
it('records creation at the first accepted submission and keeps it across later submissions, edits and reload', async () => {
  vi.useFakeTimers(); vi.setSystemTime(new Date('2026-09-21T12:00:00.000Z'))
  const { store, write } = setup(); await store.load()
  const draft = store.startChat()
  expect(draft.createdAt).toBeUndefined()
  store.update(draft.id, { composer: 'Hello' })
  store.report(draft.id, { ...binding, blocked: 'Configure Assistant' })
  expect(store.perform(draft.id, () => {})).toBe(false)
  expect(store.getSnapshot().drafts[0]?.createdAt).toBeUndefined()
  expect(store.getSnapshot().chats).toEqual([])
  vi.setSystemTime(new Date('2026-09-21T12:05:00.000Z'))
  store.report(draft.id, binding)
  expect(store.perform(draft.id, () => {})).toBe(true)
  expect(store.getSnapshot().chats[0]?.createdAt).toBe('2026-09-21T12:05:00.000Z')
  const at = '2026-09-21T12:05:00.321Z', turns: Turn[] = [{ role: 'user', kind: 'message', text: 'Hello', at }]
  store.report(draft.id, { ...binding, state: { ...INITIAL_STATE, phase: 'conversation', status: 'complete', turns } })
  vi.setSystemTime(new Date('2026-09-22T12:00:00.000Z'))
  store.update(draft.id, { title: 'Renamed', pinned: true, archived: true, composer: 'Later prompt' })
  expect(store.perform(draft.id, () => {})).toBe(true)
  await store.flush()
  const saved = JSON.parse(JSON.stringify(write.mock.calls.at(-1)![0])) as { chats: Chat[] }
  const restored = setup(saved.chats); await restored.store.load()
  expect(restored.store.getSnapshot().chats[0]).toMatchObject({ createdAt: '2026-09-21T12:05:00.000Z', updatedAt: at, title: 'Renamed', pinned: true, archived: true })
  expect(restored.store.getSnapshot().chats[0]?.checkpoint?.state.turns).toEqual(turns)
})
it('round-trips legacy history without inventing a creation time or normalizing message times', async () => {
  const turns: Turn[] = ['2026-09-20T15:00:00+03:00', '2026-09-20T15:00:00+03:00', 'unknown', '2026-09-19T12:00:00Z'].map((at, i) => ({ role: 'user', kind: 'message', text: `Message ${i}`, at }))
  const chat = { ...legacy, checkpoint: checkpoint({ ...INITIAL_STATE, phase: 'conversation', status: 'complete', turns }, []) }
  const { store, write } = setup([chat]); await store.load()
  store.report(chat.id, binding); store.perform(chat.id, () => {})
  store.update(chat.id, { pinned: true }); await store.flush()
  const saved = JSON.parse(JSON.stringify(write.mock.calls.at(-1)![0])) as { chats: Chat[] }
  expect(saved.chats[0]).not.toHaveProperty('createdAt')
  expect(saved.chats[0]?.checkpoint?.state.turns).toEqual(turns)
})
it.each(['2026-02-30T12:00:00Z', '2026-09-21', 'unknown', null, 0])('refuses a malformed new creation field (%s) without overwriting history', async createdAt => {
  const { store, write } = setup([{ ...legacy, createdAt } as Chat]); await store.load()
  expect(store.getSnapshot().ready).toBe(false)
  expect(store.getSnapshot().error).toContain('Invalid saved chat')
  expect(await store.flush()).toBe(false)
  expect(write).not.toHaveBeenCalled()
})
