import type { GraphProposal } from './author'

/** A graph draft belongs to a chat; it is never a pack candidate or a saved project file. */
export interface GraphDraft extends GraphProposal {
  draftId: string
  createdAt: string
  saved?: boolean
}
export function validGraphDraft(value: unknown): value is GraphDraft {
  if (!value || typeof value !== 'object') return false
  const draft = value as GraphDraft
  return typeof draft.draftId === 'string' && /^[a-zA-Z0-9-]{1,80}$/.test(draft.draftId)
    && typeof draft.id === 'string' && /^[a-z][a-z0-9]*(?:-[a-z0-9]+)*$/.test(draft.id) && draft.id.length <= 128
    && typeof draft.path === 'string' && draft.path.length <= 1024
    && typeof draft.content === 'string' && new TextEncoder().encode(draft.content).length <= 1024 * 1024
    && typeof draft.createdAt === 'string' && Number.isFinite(Date.parse(draft.createdAt))
    && (draft.description === undefined || typeof draft.description === 'string' && draft.description.length <= 4096)
    && (draft.baseSha256 === undefined || /^[a-f0-9]{64}$/.test(draft.baseSha256))
    && (draft.saved === undefined || typeof draft.saved === 'boolean')
}
export function graphDraftHref(chatId: string, draftId: string) {
  return `/graphs?chat=${encodeURIComponent(chatId)}&draft=${encodeURIComponent(draftId)}`
}
