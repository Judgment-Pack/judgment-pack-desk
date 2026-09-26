import { useId, useState } from 'react'
import { msg } from '../i18n'
import { Select } from '../ui/Select'
import { SnapshotComparison } from './SnapshotComparison'
export function DraftComparison({ candidates }: { candidates: readonly { revision: number; text: string }[] }) {
  const id = useId()
  const [revision, setRevision] = useState(String(candidates.at(-2)?.revision ?? ''))
  const latest = candidates.at(-1), baseline = candidates.find(c => String(c.revision) === revision) ?? candidates.at(-2)
  if (!latest || !baseline || candidates.length < 2) return null
  return <section aria-label={msg('Review revision changes')}>
    <h3>{msg('Review revision changes')}</h3>
    <Select id={id} aria-label={msg('Compare with revision')} value={String(baseline.revision)} onValueChange={setRevision}
      options={candidates.slice(0,-1).map(c => ({ value:String(c.revision),label:msg('Revision {{number}}',{number:c.revision}) }))}/>
    <p>{msg('Current revision {{number}}', {number:latest.revision})}</p>
    <SnapshotComparison before={baseline.text} after={latest.text} beforeLabel={msg('Earlier revision')} afterLabel={msg('Current revision')}/>
  </section>
}
