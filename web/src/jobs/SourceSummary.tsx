import { useQuery } from '@tanstack/react-query'
import { useEffectiveConfig } from '../config/DeskConfigProvider'
import { msg } from '../i18n'
import { Disclosure } from '../ui/Disclosure'
import { jobsAPI, type InputPreview, type SourceInput } from './client'
import { sourceText, verifySource } from './sourceInputs'
import styles from './JobsView.module.css'

export function SourceSummary({ source }: { source: SourceInput }) {
  const pin = useEffectiveConfig().config.research.gateway
  const checked = useQuery({ queryKey: ['job-source-check', source.snapshot.original.sha256, source.mapping, source.snapshot.proof, pin], queryFn: async () => { await verifySource(source, pin); return true }, retry: false })
  const mapped = useQuery({ queryKey: ['job-source-preview', source], queryFn: ({ signal }) => jobsAPI<InputPreview>('inputs/preview', { source }, undefined, signal), retry: false })
  let original = ''
  try { original = sourceText(source.snapshot) } catch { /* A malformed retained source is reported by the checks above. */ }
  return <section className={styles.fields}><h2>{msg('Input source')}</h2>
    <dl className={styles.properties}><div><dt>{msg('File')}</dt><dd>{source.snapshot.original.name}</dd></div><div><dt>{msg('Source')}</dt><dd>{source.mapping.provider === 'local-file' ? msg('Local JSON file') : msg('Google Drive')}</dd></div></dl>
    <p className={styles.note}>{msg('The selected file and mapping are retained locally with this run.')}</p>
    {source.mapping.provider === 'google-drive' && <p className={checked.error ? styles.problem : styles.note}>{checked.isPending ? msg('Verifying source receipt…') : checked.error ? msg('The source receipt could not be verified against the current connection.') : msg('Source receipt verified against the current connection. Evidence availability remains a declaration.')}</p>}
    {mapped.error && <p className={styles.problem} role="alert">{msg('The retained input mapping could not be checked.')}</p>}
    {mapped.data && <Disclosure title={msg('Retained inputs')}><pre className={styles.json}>{mapped.data.factsText}</pre><p className={styles.note}>{msg('Evidence availability')}</p><pre className={styles.json}>{mapped.data.evidenceText || msg('Not supplied')}</pre></Disclosure>}
    <Disclosure title={msg('Source JSON')}><pre className={styles.json}>{original}</pre></Disclosure>
    <Disclosure title={msg('Source provenance')}><dl className={styles.properties}><div><dt>{msg('File digest')}</dt><dd><code>{source.snapshot.original.sha256}</code></dd></div><div><dt>{msg('Mapping digest')}</dt><dd><code>{source.mappingDigest}</code></dd></div>{source.snapshot.selectedAt && <div><dt>{msg('Selected')}</dt><dd>{source.snapshot.selectedAt}</dd></div>}{source.snapshot.proof?.drive && <div><dt>{msg('File ID')}</dt><dd>{source.snapshot.proof.drive.fileId}</dd></div>}</dl><pre className={styles.json}>{JSON.stringify(source.mapping, null, 2)}</pre>{source.snapshot.proof && <Disclosure title={msg('Source receipt')}><pre className={styles.json}>{source.snapshot.proof.response}</pre></Disclosure>}</Disclosure>
  </section>
}
