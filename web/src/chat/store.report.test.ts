import { afterEach, expect, it, vi } from 'vitest'
import { ChatStore, type Chat } from './store'
import { INITIAL_STATE } from '../research/initialState'
import { memoryDraftPersistence } from '../testing/draftPersistence'
import { REPORT1, REPORT2, REPORT_API } from '../assistant/reportFixtures'

const stores: ChatStore[] = []
afterEach(() => {stores.forEach(s => s.dispose()); stores.length = 0; vi.unstubAllGlobals()})
function fixture(report: unknown, transform: (raw: string) => string = raw => raw) {
 const chat: Chat = {id: 'chat-one', title: 'Conversation', pinned: false, archived: false, updatedAt: '2026-10-28T00:00:00Z', composer: '', model: 'model-one', mode: 'draft', view: 'chat', checkpoint: {state: {...INITIAL_STATE, responses: [{id: 'response-one', afterTurnId: 'turn-one', documents: [], websites: [], sourceIds: [], work: {items: [], notices: [], report: {requests: 0, unit: 'provider-managed', canonical: report as never}}}]}, sources: []}}
 const document = transform(JSON.stringify({version: 1, chats: [chat]}))
 const writes: string[] = []
 vi.stubGlobal('fetch', vi.fn(async (_url: unknown, init?: RequestInit) => {
  if (init?.method === 'PUT') writes.push(String(init.body))
  return new Response(`{"project":"/project","sha256":"saved","content":${document}}`, {headers: {'Content-Type': 'application/json'}})
 }))
 const store = new ChatStore('/project', undefined, memoryDraftPersistence('/project')); stores.push(store)
 return {store, writes, document}
}
it.each([REPORT1, REPORT2, REPORT_API])('loads complete retained report v$version through the real HTTP history boundary', async report => {
 const h = fixture(report); await h.store.load()
 expect(h.store.getSnapshot().error).toBe('')
 const responses = h.store.getSnapshot().chats[0]?.checkpoint?.state.responses
 expect(responses).toBeDefined()
 if (!responses) throw new Error('Expected retained responses')
 const work = responses[0]?.work
 expect(work).toBeDefined()
 if (!work) throw new Error('Expected retained response work')
 expect(work.report?.canonical).toEqual(report)
 expect(h.writes).toEqual([])
})
it.each([
 (raw: string) => raw.replace('"used":0', '"used":9007199254740991.1'),
 (raw: string) => raw.replace('"used":0', '"used":0,"used":0'),
 (raw: string) => raw.replace('"checkpointVersion":2', '"checkpointVersion":99'),
 (raw: string) => raw.replace('"used":0', '"used":0,"handoff":{}'),
 (raw: string) => raw.replace('"source":"agent"', '"source":null'),
 (raw: string) => raw.replace('"explanation":"Stopped by user"', `"explanation":"${'x'.repeat(4 * 1024 * 1024)}"`)
])('refuses malformed raw canonical report before generic numeric decoding and leaves history untouched', async transform => {
 const h = fixture(REPORT1, transform), before = h.document
 await h.store.load()
 expect(h.store.getSnapshot().ready).toBe(false)
 expect(h.store.getSnapshot().error).not.toBe('')
 expect(h.writes).toEqual([])
 expect(h.document).toBe(before)
})
