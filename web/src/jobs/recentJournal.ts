/** Newest page first where Runner explicitly advertises journal-backward.
 * Entries retain Runner sequence order, even when its clock moves backwards.
 * Old Runners keep the existing forward reader and its exact coverage wording.
 */
import { useEffect, useRef, useState } from 'react'
import { msg } from '../i18n'
import { jobsAPI } from './client'
import { JOURNAL_REFRESH_MS, entryOf, useForwardJournalReader, type JournalEntry, type JournalPage, type JournalState } from './journal'

const sequence = (value: unknown): value is number => Number.isSafeInteger(value) && (value as number) >= 0

export function readRecentPage(page: JournalPage, direction: 'tail' | 'before' | 'after', cursor: number) {
  if (!Array.isArray(page.items) || page.items.length > 50 || !sequence(page.next) || typeof page.more !== 'boolean' ||
      direction !== 'after' && !sequence(page.before)) throw new Error(msg('The activity page could not be read.'))
  const items = page.items.map(entryOf)
  let previous = direction === 'after' ? cursor : 0
  for (const item of items) {
    if (!sequence(item.sequence) || item.sequence <= previous || direction === 'before' && item.sequence >= cursor) throw new Error(msg('The activity page could not be read.'))
    previous = item.sequence
  }
  if (items.length && page.next !== previous || direction === 'after' && page.next < cursor ||
      direction !== 'after' && items.length && page.before !== items[0]!.sequence || page.more && items.length === 0) throw new Error(msg('The activity page could not be read.'))
  return items
}

function useRecentJournal(jobId: string, keep: number, enabled: boolean) {
  const [state, setState] = useState<JournalState>({ phase: 'starting' })
  const actions = useRef({ older: () => {}, resume: () => {} })
  useEffect(() => {
    if (!enabled) return
    const controller = new AbortController()
    let entries: JournalEntry[] = [], next = 0, before = 0, more = false, began: unknown
    let busy = false, initialized = false, reason: string | undefined, failed: 'tail' | 'before' | 'after' = 'tail'
    let timer: ReturnType<typeof setTimeout> | undefined
    const publish = () => {
      if (controller.signal.aborted) return
      if (!initialized && !reason) { setState({ phase: 'starting' }); return }
      setState({ phase: reason ? 'stopped' : busy ? 'reading' : 'caught-up', read: entries.length, next,
        snapshot: initialized ? { entries: entries.slice(), dropped: 0, began } : undefined, reason,
        backward: { before, more, busy, limit: entries.length >= keep, error: reason } })
    }
    const read = async (direction: 'tail' | 'before' | 'after') => {
      if (busy || controller.signal.aborted || direction === 'before' && (!more || entries.length >= keep)) return
      busy = true; reason = undefined; publish(); clearTimeout(timer)
      try {
        let again = true
        while (again && !controller.signal.aborted) {
          const cursor = direction === 'before' ? before : next
          const query = direction === 'tail' ? 'tail=true' : `${direction}=${cursor}`
          const page = await jobsAPI<JournalPage>(`jobs/${jobId}/events?${query}`, undefined, undefined, controller.signal)
          if (controller.signal.aborted) return
          const items = readRecentPage(page, direction, cursor)
          // Only one request runs at a time. Direction bounds ensure no overlap;
          // a sequence-keyed merge also keeps a replay from duplicating rows.
          const merged = new Map([...entries, ...items].map(entry => [entry.sequence as number, entry]))
          entries = [...merged.values()].sort((a, b) => (a.sequence as number) - (b.sequence as number))
          began = page.journalBegan
          if (direction !== 'before') next = page.next
          if (direction !== 'after') { before = page.before!; more = page.more }
          if (entries.length > keep) { entries = entries.slice(-keep); before = entries[0]!.sequence as number; more = true }
          initialized = true
          again = direction === 'after' && page.more
          publish()
          if (again) await new Promise(resolve => setTimeout(resolve, 0))
        }
      } catch (error) {
        if (controller.signal.aborted) return
        failed = direction
        reason = error instanceof Error ? error.message : msg('The local runner could not complete this request.')
      } finally {
        busy = false; publish()
        if (!controller.signal.aborted && !reason) timer = setTimeout(() => void read('after'), JOURNAL_REFRESH_MS)
      }
    }
    actions.current = { older: () => void read('before'), resume: () => void read(failed) }
    void read('tail')
    return () => { controller.abort(); clearTimeout(timer); actions.current = { older: () => {}, resume: () => {} } }
  }, [jobId, keep, enabled])
  return { state, older: () => actions.current.older(), resume: () => actions.current.resume() }
}

export function useJournalReader(jobId: string, keep: number) {
  const [capability, setCapability] = useState<{ jobId: string; backward: boolean }>()
  const mode = capability?.jobId === jobId ? capability.backward : undefined
  useEffect(() => {
    const controller = new AbortController()
    void jobsAPI<{ capabilities?: unknown }>('status', undefined, undefined, controller.signal).then(status => {
      if (!controller.signal.aborted) setCapability({ jobId, backward: Array.isArray(status?.capabilities) && status.capabilities.includes('journal-backward') })
    }).catch(() => { if (!controller.signal.aborted) setCapability({ jobId, backward: false }) })
    return () => controller.abort()
  }, [jobId])
  const forward = useForwardJournalReader(jobId, keep, mode === false)
  const recent = useRecentJournal(jobId, keep, mode === true)
  return mode === undefined ? { state: { phase: 'starting' } as JournalState, resume: () => {}, older: () => {} }
    : mode ? recent : { ...forward, older: () => {} }
}
