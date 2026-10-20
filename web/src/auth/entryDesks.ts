import { useEffect, useState } from 'react'
import { authCall } from './flow'
import { readDeskSelection, selectDesk } from '../desks/scope'
import type { DeskIdentityChoice } from '../desks/DeskIdentity'

export type EntryDirectory = { startup: string; desks: Array<DeskIdentityChoice & {mark: string | null}> }
const validID = (id: unknown): id is string => typeof id === 'string' && (id === '' || /^[a-f0-9]{32}$/.test(id))
export async function entryDesks(signal?: AbortSignal): Promise<EntryDirectory> {
 const value = await authCall<EntryDirectory>('desks', undefined, undefined, signal)
 if (!value || !validID(value.startup) || !Array.isArray(value.desks) || !value.desks.length || value.desks.length > 257
  || !value.desks.every(row => row && validID(row.id) && typeof row.name === 'string' && row.name.trim() && [...row.name].length <= 80 && !/[\u0000-\u001f\u007f]/.test(row.name)
   && (row.mark === null || typeof row.mark === 'string' && row.mark.length <= 90000 && /^data:image\/(?:svg\+xml|png|jpeg|gif);base64,[A-Za-z0-9+/=]+$/.test(row.mark)))
  || new Set(value.desks.map(row=>row.id)).size !== value.desks.length) throw Error('Invalid entry directory')
 return value
}
export function useEntryDesks(enabled: boolean) {
 const [directory, setDirectory] = useState<EntryDirectory>()
 const [selection, setSelection] = useState<string>()
 const [problem, setProblem] = useState<'unavailable' | 'storage'>()
 const [revision, setRevision] = useState(0)
 useEffect(() => {
  if (!enabled) return
  const abort = new AbortController()
  setProblem(undefined)
  void entryDesks(abort.signal).then(value => {
   if (abort.signal.aborted) return
   setDirectory(value)
   try {
    const id = readDeskSelection() ?? value.startup
    selectDesk(id)
    setSelection(id)
   } catch {setProblem('storage')}
  }).catch(() => {if (!abort.signal.aborted) setProblem('unavailable')})
  return () => abort.abort()
 }, [enabled, revision])
 return {directory, selection, selected: directory?.desks.find(row=>row.id === selection), problem,
  choose: (id: string) => {try {selectDesk(id); setSelection(id); setProblem(undefined)} catch {setProblem('storage')}},
  retry: () => setRevision(value=>value+1)}
}
