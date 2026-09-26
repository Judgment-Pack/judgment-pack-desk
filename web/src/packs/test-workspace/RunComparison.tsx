import { Fragment, useId, useState } from 'react'
import { msg } from '../../i18n'
import { Button } from '../../ui/Button'
import { Select } from '../../ui/Select'
import { CodeBlock } from '../../ui/CodeBlock'
import { Disclosure } from '../../ui/Disclosure'
import { MatrixRowList } from '../../components/MatrixRowList'
import { SnapshotComparison } from '../SnapshotComparison'
import { compareRuns } from './comparison'
import type { TestRun } from './model'
import styles from './TestsWorkspace.module.css'
export function RunComparison({ runs, currentText }: { runs: TestRun[]; currentText: string }) {
  const [selected,setSelected] = useState<string>()
  const id = useId()
  const [beforeId, setBefore] = useState(runs.at(-2)?.id ?? runs.at(-1)?.id ?? '')
  const [afterId, setAfter] = useState(runs.at(-1)?.id ?? '')
  const before = runs.find(r => r.id === beforeId), after = runs.find(r => r.id === afterId)
  const options = [...runs].reverse().map((r, i) => ({value:r.id, label:`${new Date(r.at).toLocaleString()} · ${r.packVersion} · ${runs.length-i}`}))
  return <section aria-label={msg('Compare runs')}>
    <div className={styles.filters}>
      <div className={styles.comparisonPicker}><label htmlFor={id+'-before'}>{msg('Earlier run')}</label><Select id={id+'-before'} aria-label={msg('Earlier run')} value={beforeId} onValueChange={setBefore} options={options}/></div>
      <div className={styles.comparisonPicker}><label htmlFor={id+'-after'}>{msg('Later run')}</label><Select id={id+'-after'} aria-label={msg('Later run')} value={afterId} onValueChange={setAfter} options={options}/></div>
    </div>
    {!before || !after ? <p>{msg('Choose two retained runs to compare.')}</p> : <>
      <p className={styles.muted}>{msg('Each result belongs to its recorded pack and inputs. Changed inputs or expectations prevent attributing a result change to the pack alone.')}</p>
      {(before.error || after.error) && <p role="status">{msg('One of these runs did not complete. Missing results are not passes.')}</p>}
      <SnapshotComparison before={before.packText} after={after.packText} beforeLabel={msg('Earlier pack')} afterLabel={msg('Later pack')}/>
      <Disclosure title={msg('Compare later pack with current pack')}><SnapshotComparison before={after.packText} after={currentText} beforeLabel={msg('Tested pack')} afterLabel={msg('Current pack')}/></Disclosure>
      <div className={styles.tableWrap}><table className={`${styles.table} ${styles.comparisonTable}`}>
        <thead><tr><th>{msg('Case')}</th><th>{msg('Earlier result')}</th><th>{msg('Later result')}</th><th>{msg('Changes')}</th></tr></thead>
        <tbody>{compareRuns(before,after).map(row => <Fragment key={row.id}><tr>
          <td><Button variant="inline" aria-expanded={selected===row.id} onClick={()=>setSelected(selected===row.id?undefined:row.id)}>{row.name}</Button></td>
          <td>{row.before?.status ?? msg('Not reported')}</td><td>{row.after?.status ?? msg('Not reported')}</td>
          <td>{row.presence === 'added' ? msg('Added case') : row.presence === 'removed' ? msg('Removed case') : [row.inputsChanged ? msg('Inputs or sources changed') : '',row.expectationChanged ? msg('Expectation changed') : ''].filter(Boolean).join(' · ') || msg('Same inputs and expectations')}</td>
        </tr>{selected===row.id&&<tr><td colSpan={4}><h4>{msg('Earlier result')}</h4><MatrixRowList rows={row.before ? [row.before] : []}/><h4>{msg('Later result')}</h4><MatrixRowList rows={row.after ? [row.after] : []}/></td></tr>}</Fragment>)}</tbody>
      </table></div>
      {(before.trial || after.trial) && <Disclosure title={msg('Compare exploratory observations')}>
        <p>{msg('Exploratory results have no saved expectation. They are not pass/fail comparisons.')}</p>
        {[{run:before,label:msg('Earlier observation')},{run:after,label:msg('Later observation')}].filter(item=>item.run.trial).map(({run,label})=><CodeBlock key={label} label={label} text={JSON.stringify({facts:run.trial!.facts,evidence:run.trial!.evidence,disposition:run.trial!.disposition,handoffTarget:run.trial!.handoffTarget},null,2)}/>)}
      </Disclosure>}
      {!before.cases.length && !before.trial || !after.cases.length && !after.trial ? <p>{msg('Some historical runs have no retained case snapshots. Their inputs cannot be compared.')}</p> : null}
    </>}
  </section>
}
