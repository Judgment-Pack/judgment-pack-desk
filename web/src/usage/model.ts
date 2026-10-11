import type { GraphDocument, PackSummary } from '../mcp/types'
import type { Job, Page, Release } from '../jobs/client'

export interface UsageRow {
  id: string
  kind: 'graph' | 'job'
  name: string
  href: string
  draft?: boolean
  nodes?: string[]
  packKeys?: string[]
  versions: string[]
  graphVersion?: string
  job?: Job
  release?: Release
}

/** Graphs bind configured keys. Jobs bind the document identity in a frozen release. */
export function packReferences(packs: readonly PackSummary[], configuredKey?: string, documentId?: string) {
  return packs.filter(pack => configuredKey ? pack.id === configuredKey : !!documentId && pack.packId === documentId)
}
export function graphReferences(document: GraphDocument, packs: readonly PackSummary[]) {
  const nodes = Object.entries(document.nodes ?? {}).filter(([, node]) => packs.some(pack => pack.id === node.pack))
  const keys = [...new Set(nodes.map(([, node]) => node.pack))]
  return { nodes: nodes.map(([id]) => id), packKeys: keys, versions: [...new Set(packs.filter(pack => keys.includes(pack.id)).flatMap(pack => pack.packVersion ? [pack.packVersion] : []))] }
}

/** Bounded reads, preserving order and successful results when another read fails. */
export async function readBatch<T, R>(items: readonly T[], read: (item: T) => Promise<R>, signal?: AbortSignal): Promise<PromiseSettledResult<R>[]> {
  const results: PromiseSettledResult<R>[] = new Array(items.length)
  let cursor = 0
  await Promise.all(Array.from({ length: Math.min(4, items.length) }, async () => {
    while (cursor < items.length) {
      if (signal?.aborted) throw signal.reason
      const index = cursor++
      try { results[index] = { status: 'fulfilled', value: await read(items[index]) } }
      catch (reason) { results[index] = { status: 'rejected', reason } }
    }
  }))
  if (signal?.aborted) throw signal.reason
  return results
}

type ReadJobs = <T>(path: string) => Promise<T>
export interface JobUsagePage { rows: UsageRow[]; unread: number; scanned: number; next: number }
/** One runner page at a time. A failed detail read cannot be reported as no usage. */
export async function readJobUsagePage(read: ReadJobs, after: number, documentId: string, signal?: AbortSignal): Promise<JobUsagePage> {
  const page = await read<Page<Job>>(`jobs?after=${after}`)
  const details = await readBatch(page.items, job => read<{ job: Job; release: Release }>(`jobs/${encodeURIComponent(job.id)}`), signal)
  const rows: UsageRow[] = []
  let unread = 0
  details.forEach((result, index) => {
    if (result.status === 'rejected') { unread++; return }
    const { job, release } = result.value
    if (job.id !== page.items[index].id || job.releaseId !== release.id) { unread++; return }
    if (release.packId !== documentId) return
    rows.push({ id: `job:${job.id}`, kind: 'job', name: job.name, href: `/jobs/${encodeURIComponent(job.id)}`,
      versions: [release.packVersion], job: { ...page.items[index], ...job }, release })
  })
  // Refuse a repeated/backwards cursor instead of looping or claiming completeness.
  if (page.next && page.next <= after) throw Error('The job list returned an invalid cursor.')
  return { rows, unread, scanned: page.items.length, next: page.next }
}
