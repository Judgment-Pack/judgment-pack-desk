import { answer, deskFetch } from '../../files/client'
import { decodeStore, type TestStore } from './model'
export interface Reply {
  project: string
  sha256: string
  content: TestStore
}
export async function readTests(signal?: AbortSignal): Promise<Reply> {
  const r = await answer<Reply>(await deskFetch('/api/pack-tests', {signal}))
  return { ...r, content: decodeStore(r.content) }
}
