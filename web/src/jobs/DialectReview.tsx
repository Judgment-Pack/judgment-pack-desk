import {sourcePin} from '../dialect/bindings'
import type {BindingDocument} from '../dialect/types'
import { formatDate, msg, useLocale } from '../i18n'
import { Disclosure } from '../ui/Disclosure'
import type { ExecutableDialect, InputProfile, Preparation, CalculationLineage } from './dialectTypes'
import { calculationAge, calculationStatus, calculatorLabel } from './calculatedValues'
import styles from './JobsView.module.css'

export function DialectReview({mapping, binding, profiles = [], warnings = [], details = true}: {mapping: ExecutableDialect;binding?:BindingDocument; profiles?: InputProfile[]; warnings?: string[]; details?: boolean}) {
 useLocale()
 return <div className={styles.fields}>
  <div className={styles.tableWrap}><table className={styles.table}><thead><tr><th>{msg('Source')}</th><th>{msg('Type')}</th><th>{msg('Input class')}</th></tr></thead><tbody>
   {mapping.case && <tr><td>{msg('Case inputs')}</td><td>{msg('Manual / API')}</td><td><code>asserted</code></td></tr>}
   {(mapping.sources ?? []).map(s => {
    const pin=sourcePin(s,binding),profileId=pin?.profile??s.profile,profile = profiles.find(p => p.id === profileId)
    return <tr key={s.name}><td>{s.name}</td><td>{s.kind === 'research' ? msg('Research') : s.kind === 'selected-file' ? s.provider === 'local-file' ? msg('Local JSON file') : msg('Google Drive') : profileId??s.kind}
     {pin&&<div className={styles.note}>{pin.profile} · <code>{pin.profileDigest}</code>{Object.entries(binding?.sources[s.name]?.webProfiles??{}).map(([role,web])=><div key={role}>{role} · {web.profile}</div>)}</div>}
     {profile?.calculator && <div className={styles.note}>
      <div>{calculatorLabel(profile.calculator)}</div>
      {Object.entries(s.calculation?.inputs ?? {}).map(([input, parameter]) => <div key={input}>{input} ← {parameter}</div>)}
      {Object.entries(s.calculation?.tables ?? {}).map(([table, seconds]) => <div key={table}>{msg('{{table}} · maximum age: {{age}}', {table, age: calculationAge(seconds)})}</div>)}
     </div>}
    </td><td><code>{s.provider === 'local-file' ? 'asserted' : s.kind==='research'?'generated':profile?.class ?? '—'}</code></td></tr>
   })}
  </tbody></table></div>
  {warnings.map(w => <p className={styles.note} key={w}>{w}</p>)}
  {details && <Disclosure title={msg('Dialect details')}><pre className={styles.json}>{JSON.stringify(mapping, null, 2)}</pre></Disclosure>}
 </div>
}
function CalculationDetails({calculation}: {calculation: CalculationLineage}) {
 return <div className={styles.note}>
  <div>{calculatorLabel(calculation.calculator)}</div>
  {calculation.inputs.map(input => <div key={input.name}>
   {input.name} ← {input.parameter} · {input.source === 'case' ? msg('Case inputs') : msg('Source {{name}}', {name: input.source})} · <code>{input.pointer}</code>
  </div>)}
  {Object.entries(calculation.asOf).map(([table, timestamp]) => <div key={table}>
   {msg('{{table}} · as of {{time}}', {table, time: formatDate(new Date(timestamp), {dateStyle: 'medium', timeStyle: 'medium'})})}
  </div>)}
 </div>
}
export function InputLineage({preparation,description}: {preparation: Preparation;description?:string}) {
 useLocale()
 return <section className={styles.fields}><h3>{msg('Input lineage')}</h3><p className={styles.note}>{description??msg('Receipts verify the source and request. They do not establish input truth or session completeness.')}</p>
  <div className={styles.tableWrap}><table className={styles.table}><thead><tr><th>{msg('Dialect')}</th><th>{msg('Source')}</th><th>{msg('State')}</th></tr></thead><tbody>{preparation.lineage.map(l => <tr key={`${l.kind}:${l.target}`}><td><code>{l.target}</code></td><td>{l.source}<div className={styles.note}>{l.class==='generated'?msg('AI research'):l.class}{l.generatedInfluence&&l.class!=='generated' ? ' · '+msg('AI research') : ''}</div>{l.calculation && <CalculationDetails calculation={l.calculation}/>}</td><td>{l.present ? msg('Supplied') : msg('Not supplied')}<div className={styles.note}>{l.calculation ? calculationStatus(l.calculation.status) : l.reason}</div></td></tr>)}</tbody></table></div>
  <Disclosure title={msg('Technical details')}><pre className={styles.json}>{JSON.stringify(preparation, null, 2)}</pre></Disclosure>
 </section>
}
