import {privateQuery} from '../data/privateQuery'
import { useInfiniteQuery, useQuery, useQueryClient } from '@tanstack/react-query'
import { useMcp } from '../mcp/McpProvider'
import { fetchGraphDocument, useGraphInventory, usePacks } from '../mcp/queries'
import { readGraphDocument } from '../mcp/graphDocument'
import { useChats } from '../chat/ChatProvider'
import { graphDraftHref } from '../graphs/drafts'
import { jobsAPI } from '../jobs/client'
import { graphReferences, packReferences, readBatch, readJobUsagePage, type UsageRow } from './model'

/** Mounted only while Usage is open. This path only lists and reads retained artifacts. */
export function usePackUsage(configuredKey?: string, documentId?: string) {
  const { client, status, connectionEpoch, graphInventorySupported, graphDocumentSupported } = useMcp()
  const cache = useQueryClient(), inventory = useGraphInventory(), packs = usePacks(), chats = useChats()
  const references = packReferences(packs.error ? [] : packs.data?.packs ?? [], configuredKey, documentId)
  const graphs = inventory.error ? [] : inventory.data?.graphs ?? []
  const supported = graphInventorySupported && graphDocumentSupported
  const inventoryKnown = inventory.data?.status === 'none' || (inventory.data?.status === 'resolved' && Array.isArray(inventory.data.graphs))
  const graphReads = useQuery({
    queryKey: ['pack-usage-graphs', connectionEpoch, graphs, references],
    enabled: status === 'ready' && !!client && supported && inventoryKnown && !!packs.data && !inventory.error && !packs.error && references.length > 0,
    queryFn: async ({ signal }) => {
      const reads = await readBatch(graphs, graph => cache.fetchQuery({
        queryKey: ['experimental_get_graph', connectionEpoch, graph.id],
        queryFn: ({ signal: graphSignal }) => fetchGraphDocument(client!, graph.id, graphSignal),
        staleTime: 0
      }), signal)
      const rows: UsageRow[] = []
      let unread = 0
      reads.forEach((read, index) => {
        if (read.status === 'rejected' || !read.value.document) { unread++; return }
        const reference = graphReferences(read.value.document, references)
        if (!reference.nodes.length) return
        const graph = graphs[index]
        rows.push({ id: `graph:${graph.id}`, kind: 'graph', name: graph.id,
          href: `/graphs/${encodeURIComponent(graph.id)}?view=overview`, graphVersion: read.value.document.version, ...reference })
      })
      return { rows, unread }
    }
  })
  const drafts: UsageRow[] = []
  let unreadDrafts = 0
  if (references.length) for (const chat of [...chats.chats, ...chats.drafts]) for (const draft of chat.graphDrafts ?? []) {
    if (draft.saved) continue
    const read = readGraphDocument(draft.content)
    if (!read.ok) { unreadDrafts++; continue }
    const reference = graphReferences(read.document, references)
    if (reference.nodes.length) drafts.push({ id: `draft:${chat.id}:${draft.draftId}`, kind: 'graph', name: draft.id,
      href: graphDraftHref(chat.id, draft.draftId), draft: true, graphVersion: read.document.version, ...reference })
  }
  const jobs = useInfiniteQuery({
    ...privateQuery, queryKey: ['pack-usage-jobs', connectionEpoch, documentId], initialPageParam: 0,
    enabled: !!documentId && status === 'ready',
    queryFn: ({ pageParam, signal }) => readJobUsagePage(<T,>(path: string) => jobsAPI<T>(path, undefined, undefined, signal), pageParam, documentId!, signal),
    getNextPageParam: page => page.next || undefined,
    retry: false
  })
  const jobRows = jobs.data?.pages.flatMap(page => page.rows) ?? []
  const rows = [...(graphReads.data?.rows ?? []), ...drafts, ...jobRows]
  // A concurrently updated page can repeat an item; show it once.
  const unique = [...new Map(rows.map(row => [row.id, row])).values()]
  return { rows: unique, references, inventory, packs, graphReads, supported, inventoryKnown, status, chats, unreadDrafts, jobs,
    unreadJobs: jobs.data?.pages.reduce((sum, page) => sum + page.unread, 0) ?? 0,
    scannedJobs: jobs.data?.pages.reduce((sum, page) => sum + page.scanned, 0) ?? 0 }
}
