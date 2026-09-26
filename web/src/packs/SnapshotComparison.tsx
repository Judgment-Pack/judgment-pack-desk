import { useMemo } from 'react'
import { msg } from '../i18n'
import { diffProposal, type DiffEntry } from '../assistant/proposalDiff'
import { Disclosure } from '../ui/Disclosure'
import { CodeBlock } from '../ui/CodeBlock'

/** Read-only comparison of retained bytes; never a new version/history authority. */
export function SnapshotComparison({ before, after, beforeLabel = msg('Before'), afterLabel = msg('After') }: {
  before: string; after: string; beforeLabel?: string; afterLabel?: string
}) {
  const comparison = useMemo(() => {
    try { return diffProposal(before, JSON.parse(after)) } catch { return null }
  }, [before, after])
  if (!comparison || comparison.against !== 'the draft') return <p>{msg('These snapshots could not be compared.')}</p>
  const changed = comparison.entries.filter(e => e.status !== 'unchanged' || e.moved)
  return <section aria-label={msg('Pack changes')}>
    <p>{changed.length ? changed.length===1 ? msg('1 pack section changed') : msg('{{count}} pack sections changed', { count: changed.length }) : msg('Pack content is unchanged.')}</p>
    {changed.map(entry => <Change key={entry.key} entry={entry} beforeLabel={beforeLabel} afterLabel={afterLabel} />)}
  </section>
}
function Change({ entry, beforeLabel, afterLabel }: { entry: DiffEntry; beforeLabel: string; afterLabel: string }) {
  const status = entry.moved ? msg('Moved') : entry.status === 'added' ? msg('Added') : entry.status === 'removed' ? msg('Removed') : msg('Changed')
  return <Disclosure title={`${entry.label} · ${status}`}>
    {entry.children?.length ? entry.children.filter(c => c.status !== 'unchanged' || c.moved).map(c => <Change key={c.key} entry={c} beforeLabel={beforeLabel} afterLabel={afterLabel}/>) : <>
      {entry.before !== undefined && <CodeBlock label={beforeLabel} text={entry.before}/>}
      {entry.after !== undefined && <CodeBlock label={afterLabel} text={entry.after}/>}
    </>}
  </Disclosure>
}
