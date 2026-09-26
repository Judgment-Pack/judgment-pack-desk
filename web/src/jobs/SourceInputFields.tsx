import { useEffect, useMemo, useRef, useState } from 'react'
import { msg, useLocale } from '../i18n'
import { useEffectiveConfig } from '../config/DeskConfigProvider'
import { useConnectionsPane } from '../connections/ConnectionPaneContext'
import { authorizeDrive } from '../connections/client'
import { ingestDrive } from '../documents/client'
import { factFields } from '../packs/test-workspace/model'
import type { PackDocument } from '../mcp/types'
import { Button } from '../ui/Button'
import { Input } from '../ui/Input'
import { Disclosure } from '../ui/Disclosure'
import { jobsAPI, type InputMapping, type InputPreview, type SourceInput } from './client'
import { initialMapping, localSnapshot, sourcePaths, sourceText, verifySource } from './sourceInputs'
import styles from './JobsView.module.css'

export function SourceInputFields({ doc, provider, fixed, disabled, onChange }: { doc: PackDocument; provider: InputMapping['provider']; fixed?: InputMapping; disabled: boolean; onChange: (source: SourceInput | undefined) => void }) {
  useLocale()
  const config = useEffectiveConfig().config.research, connections = useConnectionsPane()
  const [snapshot, setSnapshot] = useState<SourceInput['snapshot']>(), [mapping, setMapping] = useState<InputMapping>()
  const [preview, setPreview] = useState<InputPreview>(), [error, setError] = useState(''), [progress, setProgress] = useState('')
  const operation = useRef<AbortController | null>(null), upload = useRef<HTMLInputElement>(null)
  const context = JSON.stringify([config.gateway, config.documents, provider, fixed])
  useEffect(() => { setSnapshot(undefined); setMapping(undefined); setPreview(undefined); setError(''); setProgress(''); onChange(undefined); return () => { operation.current?.abort(); operation.current = null } }, [context, onChange])
  const paths = useMemo(() => snapshot ? sourcePaths(snapshot) : [], [snapshot])
  const originalText = useMemo(() => snapshot ? sourceText(snapshot) : '', [snapshot])
  function invalidate() { setPreview(undefined); onChange(undefined); setError('') }
  async function select(file?: File) {
    if (operation.current || disabled) return
    const active = new AbortController(); operation.current = active
    invalidate(); setSnapshot(undefined); setMapping(undefined); setProgress(msg('Reading files…'))
    try {
      let chosen: SourceInput['snapshot']
      if (provider === 'local-file') { if (!file) return; chosen = await localSnapshot(file) }
      else {
        if (!config.gateway || !config.documents?.enabled) throw Error(msg('Enable document processing in Admin → Storage & data before attaching Drive files.'))
        setProgress(msg('Continue in the Google sign-in window.'))
        const selected = await authorizeDrive('pick', active.signal)
        active.signal.throwIfAborted()
        if (selected.length !== 1) throw Error(msg('Choose one JSON file up to 200 KB.'))
        chosen = (await ingestDrive(selected[0]!, config, active.signal, message => { if (operation.current === active) setProgress(message) })).document.object
      }
      const candidates = sourcePaths(chosen)
      active.signal.throwIfAborted()
      setSnapshot(chosen); setMapping(fixed ?? initialMapping(doc, provider, candidates))
    } catch (e) { if (!active.signal.aborted) setError(e instanceof Error ? e.message : msg('The file could not be read.')) }
    finally { if (operation.current === active) { operation.current = null; setProgress('') } }
  }
  function change(kind: 'facts' | 'evidence', target: string, source: string) {
    if (!mapping) return
    invalidate()
    setMapping(kind === 'facts' ? { ...mapping, facts: [...mapping.facts.filter(f => f.target !== target), ...(source.trim() ? [{ target, source }] : [])] } : { ...mapping, evidence: [...mapping.evidence.filter(f => f.requirement !== target), ...(source.trim() ? [{ requirement: target, source }] : [])] })
  }
  async function check() {
    if (!snapshot || !mapping || operation.current) return
    const active = new AbortController(); operation.current = active; invalidate(); setProgress(msg('Checking inputs…'))
    try {
      const source = { snapshot, mapping }
      await verifySource(source, config.gateway)
      active.signal.throwIfAborted()
      const result = await jobsAPI<InputPreview>('inputs/preview', { source }, undefined, active.signal)
      active.signal.throwIfAborted()
      setPreview(result); onChange(result.input.source)
    } catch (e) { if (!active.signal.aborted) setError(e instanceof Error ? e.message : msg('The inputs could not be mapped.')) }
    finally { if (operation.current === active) { operation.current = null; setProgress('') } }
  }
  const busy = disabled || Boolean(progress)
  return <section className={styles.fields}>
    <p className={styles.note}>{provider === 'local-file' ? msg('Choose a JSON file from this computer. Inputs and run records stay in local storage.') : msg('Read one selected JSON file from Google Drive. Copies and run records stay in local storage; nothing is written to Drive.')}</p>
    <div className={styles.actions}>
      <input ref={upload} type="file" accept=".json,application/json" hidden aria-label={msg('Choose JSON file')} onChange={e => { const file = e.target.files?.[0]; e.target.value = ''; if (file) void select(file) }} />
      <Button disabled={busy} onClick={() => provider === 'local-file' ? upload.current?.click() : void select()}>{snapshot ? msg('Choose another file') : provider === 'local-file' ? msg('Choose JSON file') : msg('Choose from Google Drive')}</Button>
      {provider === 'google-drive' && <Button variant="quiet" disabled={busy} onClick={e => connections.open({ provider: 'google-drive', opener: e.currentTarget })}>{msg('Manage connection')}</Button>}
      {progress && <Button variant="quiet" onClick={() => { operation.current?.abort(); operation.current = null; setProgress(''); invalidate() }}>{msg('Cancel')}</Button>}
    </div>
    {progress && <p role="status" className={styles.note}>{progress}</p>}
    {error && <p role="alert" className={styles.problem}>{error}</p>}
    {snapshot && mapping && <>
      <div className={styles.field}><strong>{snapshot.original.name}</strong><p className={styles.note}>{fixed ? msg('This run uses the mapping frozen in the job release. Select a new file for every run.') : msg('Map source paths to pack inputs. Leave a path blank to keep that input unknown.')}</p></div>
      <fieldset disabled={busy || Boolean(fixed)} className={styles.fields}>
        {factFields(doc).map((field, index) => <div className={styles.mappingRow} key={field.path}><label htmlFor={`source-fact-${index}`}>{field.label}</label><Input id={`source-fact-${index}`} list="job-source-paths" value={mapping.facts.find(f => f.target === field.path)?.source ?? ''} placeholder={msg('Source path, e.g. /request/type')} onChange={e => change('facts', field.path, e.target.value)} /></div>)}
        {(doc.evidenceRequirements ?? []).length > 0 && <p className={styles.note}>{msg('Evidence paths must contain present, absent or unknown. A file connection does not verify evidence.')}</p>}
        {(doc.evidenceRequirements ?? []).map((field, index) => <div className={styles.mappingRow} key={field.id}><label htmlFor={`source-evidence-${index}`}>{field.description || field.id}</label><Input id={`source-evidence-${index}`} list="job-source-paths" value={mapping.evidence.find(f => f.requirement === field.id)?.source ?? ''} placeholder={msg('Source path, e.g. /evidence/receipt')} onChange={e => change('evidence', field.id, e.target.value)} /></div>)}
        <datalist id="job-source-paths">{paths.filter(Boolean).slice(0, 1000).map(path => <option key={path} value={path} />)}</datalist>
      </fieldset>
      <Disclosure title={msg('Source JSON')}><pre className={styles.json}>{originalText}</pre></Disclosure>
      <div><Button disabled={busy} onClick={() => { void check() }}>{msg('Preview mapping')}</Button></div>
    </>}
    {preview && <div className={styles.fields}><h3>{msg('Mapped inputs')}</h3><pre className={styles.json}>{preview.factsText}</pre><p className={styles.note}>{msg('Evidence availability')}</p><pre className={styles.json}>{preview.evidenceText || msg('Not supplied')}</pre><p className={styles.note}>{msg('Mapping checked. Missing values remain unknown.')}</p></div>}
  </section>
}
