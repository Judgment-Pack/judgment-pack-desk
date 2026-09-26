import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useFileListing } from '../../files/queries'
import { answer, deskFetch } from '../../files/client'
import { decodeStore, emptySuite, type TestStore, type TestSuite } from './model'
export const TESTS_KEY = ['pack-test-workspace'] as const
interface Reply {
  project: string
  sha256: string
  content: TestStore
}
export async function readTests(): Promise<Reply> {
  const r = await answer<Reply>(await deskFetch('/api/pack-tests'))
  return { ...r, content: decodeStore(r.content) }
}
export async function updateTests(
  owner: string,
  change: (suite: TestSuite, document: TestStore) => TestSuite | Promise<TestSuite>,
  project?: string,
): Promise<Reply> {
  const before = await readTests()
  if (project && before.project !== project) throw Error('The project changed. Reopen Tests before saving.')
  const current = Object.hasOwn(before.content.suites, owner) ? before.content.suites[owner]! : emptySuite()
  const next = await change(structuredClone(current), before.content)
  if (JSON.stringify(current) === JSON.stringify(next)) return before
  const content: TestStore = {
    version: 1,
    suites: { ...before.content.suites, [owner]: { ...next, revision: current.revision + 1 } },
  }
  decodeStore(content)
  return answer<Reply>(
    await deskFetch('/api/pack-tests', {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json', 'If-Match': before.sha256 },
      body: JSON.stringify(content),
    }),
  )
}
export function useTestStorage(owner: string) {
  const client = useQueryClient(),
    listing = useFileListing(),
    project = listing.data?.root
  const key = [...TESTS_KEY, project ?? null]
  const query = useQuery({
    queryKey: key,
    queryFn: readTests,
    enabled: !!project,
    refetchOnWindowFocus: false,
  })
  const update = async (
    change: (suite: TestSuite, document: TestStore) => TestSuite | Promise<TestSuite>,
  ) => {
    const r = await updateTests(owner, change, project)
    client.setQueryData(key, r)
    return r.content.suites[owner] ?? emptySuite()
  }
  return {
    query,
    suite:
      query.data && Object.hasOwn(query.data.content.suites, owner)
        ? query.data.content.suites[owner]!
        : emptySuite(),
    update,
  }
}
