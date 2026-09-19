import { useEffect, useId, useRef, useState } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import { msg, systemMessage, useLocale } from '../i18n'
import { sourceMessage } from '../i18n/source'
import { useEffectiveConfig } from '../config/DeskConfigProvider'
import { decodeDeskConfig, DESK_DEFAULTS, DOCUMENT_DEFAULTS, DOCUMENT_LIMIT_BOUNDS, withLocalGateway, type ConfigProblem, type EffectiveConfig, type LocalGatewayStatus, type ResearchConfig } from '../config/deskConfig'
import { DESK_CONFIG_QUERY_KEY, loadDeskLevelConfig } from '../config/queries'
import { answer, deskFetch } from '../files/client'
import { Alert } from '../ui/Alert'
import { Button } from '../ui/Button'
import { Dialog, DialogActions } from '../ui/Dialog'
import { Disclosure } from '../ui/Disclosure'
import { Field, FieldGroup } from '../ui/Field'
import { Select } from '../ui/Select'
import { Input } from '../ui/Input'
import { SettingRow } from '../ui/SettingRow'
import { SettingsSection } from '../ui/SettingsSection'
import { useUnsavedChanges } from '../shell/DraftScope'
import { formatStorageBytes } from './chatStorage'
import styles from './DocumentProcessingSettings.module.css'

type Editor = 'gateway' | 'pdf'
type Snapshot = { research: ResearchConfig; digest?: string; text?: string }
const blankGateway = { url: '', authority: '', signer: { algorithm: 'ed25519' as const, public: '' } }
const MIB = 1024 * 1024
function snapshot(effective: EffectiveConfig): Snapshot {
  return { research: effective.desk?.decoded?.values?.research ?? DESK_DEFAULTS.research, digest: effective.desk?.readFailure || effective.desk?.problems.length ? undefined : effective.desk?.sha256, text: effective.desk?.text }
}
function editable(research: ResearchConfig, local?: LocalGatewayStatus) {
  return {
    mode: research.gateway || !local ? 'external' : 'local',
    gateway: research.gateway ?? blankGateway,
    documents: withLocalGateway(research, local).documents ?? { ...DOCUMENT_DEFAULTS, enabled: false }
  }
}

export function DocumentProcessingSettings() {
  useLocale()
  const effective = useEffectiveConfig(), client = useQueryClient(), formId = useId()
  const local = effective.desk?.localGateway
  const [current, setCurrent] = useState(() => snapshot(effective))
  const [base, setBase] = useState(current)
  const [draft, setDraft] = useState(() => editable(current.research, local))
  const [editor, setEditor] = useState<Editor | null>(null)
  const [busy, setBusy] = useState(false), [error, setError] = useState(''), [notice, setNotice] = useState('')
  const [problems, setProblems] = useState<ConfigProblem[]>([])
  const [advanced, setAdvanced] = useState(false), [discard, setDiscard] = useState(false)
  const gatewayOpener = useRef<HTMLButtonElement>(null), pdfOpener = useRef<HTMLButtonElement>(null)
  const opener = useRef<HTMLElement | null>(null)
  const dirty = editor !== null && JSON.stringify(draft) !== JSON.stringify(editable(base.research, local))
  useUnsavedChanges(dirty)
  useEffect(() => { setCurrent(snapshot(effective)) }, [effective])

  function open(kind: Editor) {
    setBase(current); setDraft(editable(current.research, local)); setEditor(kind)
    setError(''); setNotice(''); setProblems([]); setAdvanced(false); setDiscard(false)
    opener.current = kind === 'gateway' ? gatewayOpener.current : pdfOpener.current
  }
  function close() { if (!busy) { if (dirty) setDiscard(true); else setEditor(null) } }
  function change(patch: Partial<typeof draft>) { setDraft(previous => ({ ...previous, ...patch })); setProblems([]); setError('') }
  function fieldError(key: string) { return problems.filter(problem => problem.key === key).map(problem => systemMessage(problem.reason)).join(' ') || undefined }
  function reflect(next: Snapshot, path: string, status = local) {
    setCurrent(next)
    client.setQueryData<EffectiveConfig>(DESK_CONFIG_QUERY_KEY, previous => previous && ({
      ...previous, config: { ...previous.config, research: withLocalGateway(next.research, status) },
      desk: { ...previous.desk, localGateway: status, present: Boolean(next.text), path, sha256: next.digest, text: next.text, decoded: next.text ? decodeDeskConfig(next.text, 'desk') : undefined, problems: [], readFailure: undefined }
    }))
  }
  async function reload() {
    if (busy) return
    setBusy(true)
    try {
      const read = await loadDeskLevelConfig()
      if (read.readFailure || read.decoded?.problems.length || read.sha256 === undefined) throw new Error(sourceMessage('Settings could not be read. Your changes have been kept.'))
      const next = { research: read.decoded?.values?.research ?? DESK_DEFAULTS.research, digest: read.sha256, text: read.text }
      reflect(next, read.path, read.localGateway); setBase(next); setDraft(editable(next.research, read.localGateway)); setError(''); setProblems([])
    } catch (cause) { setError((cause as Error).message) }
    finally { setBusy(false) }
  }
  async function save() {
    if (!editor || !dirty || busy || base.digest === undefined) return
    const research = { ...base.research, ...(editor === 'gateway' ? { gateway: draft.mode === 'local' ? null : draft.gateway } : { documents: draft.documents }) }
    const text = JSON.stringify({ ...JSON.parse(base.text ?? '{}'), deskConfigVersion: 1, research })
    const decoded = decodeDeskConfig(text, 'desk')
    if (decoded.problems.length) {
      setProblems(decoded.problems)
      setError(sourceMessage('Review the highlighted settings.'))
      if (decoded.problems.some(problem => ['research.documents.source', 'research.documents.maxRequestBytes', 'research.documents.maxResponseBytes'].includes(problem.key))) setAdvanced(true)
      return
    }
    if (editor === 'pdf' && draft.documents.enabled && !research.gateway && !local?.gateway) { setError(sourceMessage('Set up the gateway before enabling PDF processing.')); return }
    setBusy(true); setError('')
    try {
      const result = await answer<{ sha256: string; path: string }>(await deskFetch('/api/desk-config', {
        method: 'PUT', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ research, ifMatch: base.digest })
      }))
      const next = { research: decoded.values!.research!, digest: result.sha256, text }
      reflect(next, result.path); setBase(next); setEditor(null); setNotice(sourceMessage('Saved.'))
      void client.invalidateQueries({ queryKey: DESK_CONFIG_QUERY_KEY })
    } catch (cause) { setError((cause as Error).message) }
    finally { setBusy(false) }
  }
  const research = withLocalGateway(current.research, local)
  const gateway = research.gateway, documents = research.documents
  const managed = Boolean(local) && !current.research.gateway
  const writable = current.digest !== undefined
  return <>
    <SettingsSection title={msg('Document processing')} level={2} variant="standalone" description={msg('Extract text from uploaded PDFs.')}>
      <p className={styles.scope}>{msg('Personal · This computer')}</p>
      <SettingRow title={managed ? msg('Local processing') : msg('Gateway')} description={managed ? msg('Managed automatically by Desk on this computer.') : msg('Shared by research and PDF processing.')}
        status={managed ? local?.status === 'ready' ? msg('Ready') : msg('Unavailable') : gateway ? msg('Configured · Availability checked when used') : msg('Not configured')}
        action={<Button ref={gatewayOpener} disabled={!writable} aria-label={gateway ? msg('Manage gateway') : msg('Set up gateway')} onClick={() => open('gateway')}>{gateway || managed ? msg('Manage') : msg('Set up')}</Button>} />
      <SettingRow title={msg('PDF processing')} description={msg('Extract text from uploaded PDFs.')}
        status={!gateway ? msg('Requires a gateway') : documents?.enabled ? msg('Enabled · Up to {{size}} per file', { size: formatStorageBytes(documents.maxFileBytes) }) : msg('Disabled')}
        action={<Button ref={pdfOpener} disabled={!writable || !gateway} aria-label={msg('Manage PDF processing')} onClick={() => open('pdf')}>{msg('Manage')}</Button>} />
      {managed && local?.status === 'unavailable' && <div><Alert>{msg('Local processing is unavailable. Check the details below or use an existing gateway.')}</Alert><Disclosure title={msg('Technical details')}><p>{systemMessage(local.problem ?? '')}</p></Disclosure></div>}
      {!writable && <Alert>{msg('Settings could not be read. Reload the page before making changes.')}</Alert>}
      <p className={styles.note}>{msg('Text files can be attached without a gateway.')}</p>
      <p className={styles.note}>{msg('Uploaded originals stay in chat storage when removed from a message.')}</p>
      {notice && <p role="status" className={styles.note}>{systemMessage(notice)}</p>}
    </SettingsSection>
    <Dialog open={editor !== null} onOpenChange={isOpen => { if (!isOpen) close() }} openerRef={opener}
      title={discard ? msg('Discard changes?') : editor === 'gateway' ? msg('Gateway') : msg('PDF processing')}
      description={discard ? msg('Your unsaved changes will be lost.') : editor === 'gateway' ? msg('Used by research and PDF processing on this computer.') : msg('Process new PDF uploads through your gateway.')}
      footer={<DialogActions>{discard ? <>
        <Button onClick={() => setDiscard(false)}>{msg('Keep editing')}</Button><Button onClick={() => { setEditor(null); setDiscard(false) }}>{msg('Discard changes')}</Button>
      </> : <><Button disabled={busy} onClick={close}>{msg('Cancel')}</Button><Button type="submit" form={formId} variant="primary" disabled={!dirty || busy || base.digest === undefined}>{busy ? msg('Saving…') : msg('Save changes')}</Button></>}</DialogActions>}>
      {!discard && <form id={formId} noValidate onSubmit={event => { event.preventDefault(); void save() }}><FieldGroup>
        {editor === 'gateway' ? <>
          {local && <Field label={msg('Connection')}>{w => <Select {...w} value={draft.mode} disabled={busy} onValueChange={mode => change({ mode })} options={[{ value: 'local', label: msg('Local (automatic)') }, { value: 'external', label: msg('Existing gateway') }]} />}</Field>}
          {draft.mode === 'local' ? <p>{msg('Desk starts local PDF processing automatically. No URL or key is needed.')}</p> : <>
          <Field label={msg('Gateway URL')} error={fieldError('research.gateway.url')}>{w => <Input {...w} autoComplete="off" spellCheck={false} value={draft.gateway.url} disabled={busy} onChange={event => change({ gateway: { ...draft.gateway, url: event.target.value } })} />}</Field>
          <Field label={msg('Gateway identity')} hint={msg('Use the authority name configured by the gateway operator.')} error={fieldError('research.gateway.authority')}>{w => <Input {...w} autoComplete="off" spellCheck={false} value={draft.gateway.authority} disabled={busy} onChange={event => change({ gateway: { ...draft.gateway, authority: event.target.value } })} />}</Field>
          <Field label={msg('Verification public key')} hint={msg('Use the Ed25519 public key printed by gateway keygen. Never enter the private signing key.')} error={fieldError('research.gateway.signer.public')}>{w => <Input {...w} autoComplete="off" spellCheck={false} value={draft.gateway.signer.public} disabled={busy} onChange={event => change({ gateway: { ...draft.gateway, signer: { algorithm: 'ed25519', public: event.target.value } } })} />}</Field>
          <p className={styles.note}>{msg('Changing the identity or key can prevent saved documents from being verified. Keep the previous settings if you need to restore access.')}</p></>}
          {draft.mode === 'local' && base.research.gateway && <p className={styles.note}>{msg('Changing the identity or key can prevent saved documents from being verified. Keep the previous settings if you need to restore access.')}</p>}
        </> : <>
          <label className="checkbox"><input type="checkbox" checked={draft.documents.enabled} disabled={busy} onChange={event => change({ documents: { ...draft.documents, enabled: event.target.checked } })} />{msg('Enable PDF processing')}</label>
          <p className={styles.note}>{msg('Turning this off keeps your settings and previously uploaded documents.')}</p>
          <Field label={msg('Maximum file size (MiB)')} error={fieldError('research.documents.maxFileBytes')}>{w => <Input {...w} type="number" step="any" min={1 / MIB} max={16} value={draft.documents.maxFileBytes / MIB} disabled={busy} onChange={event => change({ documents: { ...draft.documents, maxFileBytes: Number(event.target.value) * MIB } })} />}</Field>
          {!managed && <Disclosure title={msg('Advanced settings')} open={advanced} onToggle={event => setAdvanced(event.currentTarget.open)}><FieldGroup>
            <Field label={msg('Document source name')} error={fieldError('research.documents.source')}>{w => <Input {...w} value={draft.documents.source} disabled={busy || managed} onChange={event => change({ documents: { ...draft.documents, source: event.target.value } })} />}</Field>
            {(['maxRequestBytes', 'maxResponseBytes'] as const).map(key => <Field key={key} label={key === 'maxRequestBytes' ? msg('Gateway request limit (MiB)') : msg('Extraction response limit (MiB)')} error={fieldError(`research.documents.${key}`)}>{w => <Input {...w} type="number" step="any" min={DOCUMENT_LIMIT_BOUNDS[key][0] / MIB} max={DOCUMENT_LIMIT_BOUNDS[key][1] / MIB} value={draft.documents[key] / MIB} disabled={busy || managed} onChange={event => change({ documents: { ...draft.documents, [key]: Number(event.target.value) * MIB } })} />}</Field>)}
            <p className={styles.note}>{msg('Match limits to the gateway and document adapter. Canceling in Desk does not stop gateway work already in progress.')}</p>
          </FieldGroup></Disclosure>}
          <p className={styles.note}>{msg('Scanned PDFs need OCR configured on the gateway.')}</p>
        </>}
        {error && <div><Alert>{systemMessage(error)}</Alert>{problems.length === 0 && <Button variant="quiet" disabled={busy} onClick={() => void reload()}>{msg('Reload and discard changes')}</Button>}</div>}
      </FieldGroup></form>}
    </Dialog>
  </>
}
