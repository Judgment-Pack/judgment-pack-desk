import { msg } from '../i18n'
import { Disclosure } from '../ui/Disclosure'
import type { MappingV2, InputProfile, Preparation } from './mappingTypes'
import styles from './JobsView.module.css'

export function MappingReview({mapping, profiles = [], warnings = [], details = true}: {mapping: MappingV2; profiles?: InputProfile[]; warnings?: string[]; details?: boolean}) {
 return <div className={styles.fields}>
  <div className={styles.tableWrap}><table className={styles.table}><thead><tr><th>{msg('Source')}</th><th>{msg('Type')}</th><th>{msg('Input class')}</th></tr></thead><tbody>
   {mapping.case && <tr><td>{msg('Case inputs')}</td><td>{msg('Manual / API')}</td><td><code>asserted</code></td></tr>}
   {(mapping.sources ?? []).map(s => <tr key={s.name}><td>{s.name}</td><td>{s.kind === 'operation' ? s.profile : s.provider === 'local-file' ? msg('Local JSON file') : msg('Google Drive')}</td><td><code>{s.provider === 'local-file' ? 'asserted' : profiles.find(p => p.id === s.profile)?.class ?? '—'}</code></td></tr>)}
  </tbody></table></div>
  {warnings.map(w => <p className={styles.note} key={w}>{w}</p>)}
  {details && <Disclosure title={msg('Mapping details')}><pre className={styles.json}>{JSON.stringify(mapping, null, 2)}</pre></Disclosure>}
 </div>
}
export function InputLineage({preparation}: {preparation: Preparation}) {
 return <section className={styles.fields}><h3>{msg('Input lineage')}</h3><p className={styles.note}>{msg('Receipts verify the source and request. They do not establish input truth or session completeness.')}</p>
  <div className={styles.tableWrap}><table className={styles.table}><thead><tr><th>{msg('Input')}</th><th>{msg('Source')}</th><th>{msg('State')}</th></tr></thead><tbody>{preparation.lineage.map(l => <tr key={`${l.kind}:${l.target}`}><td><code>{l.target}</code></td><td>{l.source}<div className={styles.note}>{l.class}{l.generatedInfluence ? ' · generated' : ''}</div></td><td>{l.present ? msg('Supplied') : msg('Not supplied')}<div className={styles.note}>{l.reason}</div></td></tr>)}</tbody></table></div>
  <Disclosure title={msg('Technical details')}><pre className={styles.json}>{JSON.stringify(preparation, null, 2)}</pre></Disclosure>
 </section>
}
