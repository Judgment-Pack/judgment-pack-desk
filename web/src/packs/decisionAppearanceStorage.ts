import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { FileRequestError, readFile, writeFile } from '../files/client'
import { sourceMessage } from '../i18n/source'
import { msg } from '../i18n'
import { useDirtyGuard } from '../shell/useDirtyGuard'
import { decodePresentation, emptyPresentation, PRESENTATION_FILE, PRESENTATION_LIMIT, type DecisionAppearance, type OutcomeAppearances, type PresentationDocument } from './decisionAppearance'
export interface PresentationSnapshot { document: PresentationDocument; digest: string }
export const PRESENTATION_KEY = ['pack-presentation'] as const
export async function loadPresentation(signal?: AbortSignal): Promise<PresentationSnapshot> {
  try {
    const file = await readFile(PRESENTATION_FILE, signal)
    if (file.bytes > PRESENTATION_LIMIT) throw new Error(sourceMessage('Decision appearance could not be read. The saved file has not been changed.'))
    let value: unknown
    try { value = JSON.parse(file.content) } catch { throw new Error(sourceMessage('Decision appearance could not be read. The saved file has not been changed.')) }
    return { document: decodePresentation(value), digest: file.sha256 }
  } catch (cause) {
    if (cause instanceof FileRequestError && cause.status === 404 && cause.code === 'not-found') return { document: emptyPresentation(), digest: '' }
    throw cause
  }
}
export async function savePresentation(before: PresentationSnapshot, packId: string, appearances: OutcomeAppearances): Promise<PresentationSnapshot> {
  const previous = Object.hasOwn(before.document.packs, packId) ? before.document.packs[packId]!.outcomes : {}
  const document = decodePresentation({ ...before.document, packs: { ...before.document.packs, [packId]: { outcomes: { ...previous, ...appearances } } } })
  const content = JSON.stringify(document, null, 2) + '\n'
  if (new TextEncoder().encode(content).length > PRESENTATION_LIMIT) throw new Error(sourceMessage('Decision appearance has reached its storage limit.'))
  try {
    const saved = await writeFile({ path: PRESENTATION_FILE, content, baseSha256: before.digest })
    if (saved.content !== content) throw new Error(sourceMessage('Decision appearance could not be verified. Reload before trying again.'))
    return { document: decodePresentation(JSON.parse(saved.content)), digest: saved.sha256 }
  } catch (cause) {
    if (cause instanceof FileRequestError && cause.status === 409) throw new Error(sourceMessage('Decision appearance changed in another window. Reload before trying again.'))
    throw cause
  }
}
export function useDecisionAppearance(packId?: string) {
  const client = useQueryClient()
  const query = useQuery({ queryKey: PRESENTATION_KEY, queryFn: ({ signal }) => loadPresentation(signal), enabled: Boolean(packId), retry: false, staleTime: 0, refetchOnWindowFocus: true })
  const mutation = useMutation({ mutationFn: async ({ id, appearance, resolved }: { id: string; appearance: DecisionAppearance; resolved: OutcomeAppearances }) => {
    if (!packId || !query.data || query.isError) throw new Error(sourceMessage('Reload appearance before making changes.'))
    return savePresentation(query.data, packId, { ...resolved, [id]: appearance })
  }, onMutate: () => client.cancelQueries({ queryKey: PRESENTATION_KEY }), onSuccess: snapshot => { client.setQueryData(PRESENTATION_KEY, snapshot); void client.invalidateQueries({ queryKey: ['desk-files'] }); void client.invalidateQueries({ queryKey: ['desk-file', PRESENTATION_FILE] }) } })
  useDirtyGuard(mutation.isPending, msg('Saving…'), { busy: mutation.isPending, shouldBlock: () => true })
  const saved = packId && query.data && Object.hasOwn(query.data.document.packs, packId) ? query.data.document.packs[packId]!.outcomes : undefined
  return { saved, pending: mutation.isPending, ready: Boolean(query.data) && !query.isError, error: mutation.error ?? query.error,
    change: (id: string, appearance: DecisionAppearance, resolved: OutcomeAppearances) => { if (!mutation.isPending) mutation.mutate({ id, appearance, resolved }) },
    reload: async () => { const result = await query.refetch(); if (!result.isError) mutation.reset() } }
}
