import { useEffect, useRef, useState } from 'react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { msg, systemMessage, useLocale } from '../i18n'
import { sourceMessage } from '../i18n/source'
import { DESK_CONFIG_QUERY_KEY } from '../config/queries'
import { useUnsavedChanges } from '../shell/DraftScope'
import { base64 } from '../documents/client'
import { Alert } from '../ui/Alert'
import { Button } from '../ui/Button'
import { Dialog, DialogActions } from '../ui/Dialog'
import { Field, FieldGroup } from '../ui/Field'
import { Input } from '../ui/Input'
import { Select } from '../ui/Select'
import { SettingRow } from '../ui/SettingRow'
import { TextArea } from '../ui/TextArea'
import {
  ProcessingError, cloudOCR, PROCESSING_KEY, PROCESSING_TIMEOUT_BOUNDS, processingCall, processorName, restartsGateway, sameDestination, saveProcessing,
  TEST_PDF_BYTES, useProcessing, type OCRConnection, type OCRKind, type ProcessingAnswer, type ProcessingSettings, type ProcessingTest, type RunningGateway
} from './processing'
import styles from './ProcessingSettings.module.css'

type Settings = ProcessingAnswer<ProcessingSettings>

/** Scanned pages (OCR) on this desk's local gateway: the mode, the processors, a test. */
export function OCRSettings() {
  useLocale()
  const query = useProcessing(), client = useQueryClient()
  const [notice, setNotice] = useState('')
  const read = query.data
  const failed = async (error: Error) => {
    if (error instanceof ProcessingError && error.word === 'processing-restart-changed') {
      await query.refetch()
      setNotice(error.message)
    }
  }
  // A save answers the settings and the running gateway; both replace what was read.
  const saved = (next: Settings) => {
    client.setQueryData(PROCESSING_KEY, next)
    if (next.localGateway.restarted) {
      setNotice(next.localGateway.status === 'ready' ? sourceMessage('Desk restarted its local gateway with these settings.') : sourceMessage('Desk stopped its local gateway, and it could not start again.'))
      void client.invalidateQueries({ queryKey: DESK_CONFIG_QUERY_KEY })
    } else setNotice('')
  }
  if (!read) return <div className={styles.stack}>
    <p className={styles.note} role={query.error ? 'alert' : 'status'}>{query.error ? msg('The document processing settings could not be read.') : msg('Loading OCR settings…')}</p>
    {!!query.error && <Button onClick={() => void query.refetch()}>{msg('Retry')}</Button>}
  </div>
  return <div className={styles.stack}>
    <RunningState settings={read.result} gateway={read.localGateway} />
    {notice && <p role="status" className={styles.note}>{notice}</p>}
    <OCRPreferences read={read} onSaved={saved} onError={failed} />
    <OCRProcessors read={read} onSaved={saved} onError={failed} />
  </div>
}

/** Which plan the running local gateway has, and whether it is these settings'. */
function RunningState({ settings, gateway }: { settings: ProcessingSettings; gateway: RunningGateway }) {
  if (gateway.status !== 'ready') return <div><Alert>{gateway.appliesWhenStarted ? msg('The local gateway is not running. Settings saved now apply when it starts.') : msg('The local gateway is not running.')}</Alert>{gateway.problem && <p className={styles.note}>{systemMessage(gateway.problem)}</p>}</div>
  const on = gateway.documentProcessing === true
  return <div className={styles.stack}>
    <p className={styles.note}>{on ? msg('The running local gateway reads scanned pages with OCR.') : msg('The running local gateway reads no scanned pages: it was started without OCR.')}</p>
    {(settings.mode === 'auto') !== on && <p className={styles.note}>{msg('It takes these settings when it starts again: when you next save them, or when Desk starts.')}</p>}
  </div>
}

function RestartWarning({ mode }: { mode: 'off' | 'auto' }) {
  return <Alert>{mode === 'auto'
    ? msg('Saving turns OCR on. Desk restarts its local gateway to take it, and reads in progress stop.')
    : msg('Saving turns OCR off. Desk restarts its local gateway to take it, and reads in progress stop.')}</Alert>
}

type Preferences = { mode: 'off' | 'auto'; connection: string; timeoutSeconds: number }

function OCRPreferences({ read, onSaved, onError }: { read: Settings; onSaved: (next: Settings) => void; onError: (error: Error) => Promise<void> }) {
  const settings = read.result
  // `acknowledged`: the owner ticked "Turn OCR on anyway" for this draft's
  // processor, which is not available on this computer.
  const [draft, setDraft] = useState<(Preferences & { base: Settings; acknowledged: boolean }) | null>(null)
  const save = useMutation({
    mutationFn: async () => {
      if (!draft) throw new Error(sourceMessage('There is nothing to save.'))
      return saveProcessing(draft.base.result, { ...draft.base.result, mode: draft.mode, connection: draft.connection, timeoutSeconds: draft.timeoutSeconds }, restartsGateway(draft.mode, draft.base.localGateway))
    },
    onError,
    onSuccess: next => { onSaved(next); setDraft(null) }
  })
  useUnsavedChanges(!!draft)
  const current = { mode: draft?.mode ?? settings.mode, connection: draft?.connection ?? settings.connection, timeoutSeconds: draft?.timeoutSeconds ?? settings.timeoutSeconds ?? PROCESSING_TIMEOUT_BOUNDS[1] }
  const selected = settings.connections.find(c => c.id === current.connection)
  const [low, high] = PROCESSING_TIMEOUT_BOUNDS
  const timeoutValid = Number.isInteger(current.timeoutSeconds) && current.timeoutSeconds >= low && current.timeoutSeconds <= high
  const restart = !!draft && restartsGateway(current.mode, draft.base.localGateway)
  // Gateway v0.10.0 applies the settings before an adapter reads anything, so
  // OCR on with a processor whose programs are not there stops every document
  // read, text included, not only scanned pages.
  const unready = current.mode === 'auto' && !!selected && selected.ready !== true
  const edit = (patch: Partial<Preferences>) => {
    setDraft(previous => ({ base: previous?.base ?? read, mode: current.mode, connection: current.connection, timeoutSeconds: current.timeoutSeconds,
      acknowledged: 'mode' in patch || 'connection' in patch ? false : previous?.acknowledged ?? false, ...patch }))
    save.reset()
  }
  return <div className={styles.stack}>
    <SettingRow title={msg('Scanned pages')} description={msg('Read pages that have no text with an OCR processor. The chat model is chosen separately.')}
      status={settings.mode === 'off' ? msg('OCR is off') : settings.connections.find(c => c.id === settings.connection)?.ready ? msg('OCR is on') : msg('OCR is on, and its processor is not available')} />
    <FieldGroup>
      <Field label={msg('OCR mode')}>{w => <Select {...w} value={current.mode} disabled={save.isPending} onValueChange={value => edit({ mode: value as Preferences['mode'] })}
        options={[{ value: 'off', label: msg('Off') }, { value: 'auto', label: msg('When a page has no text') }]} />}</Field>
      <Field label={msg('OCR processor')}>{w => <Select {...w} value={current.connection} disabled={save.isPending || !settings.connections.some(c => c.enabled)} placeholder={msg('Choose a processor')}
        onValueChange={value => edit({ connection: value })} options={settings.connections.filter(c => c.enabled).map(c => ({ value: c.id, label: c.ready ? c.name : msg('{{name}} · not available on this computer', { name: c.name }) }))} />}</Field>
      <Field label={msg('Processing timeout (seconds)')} hint={msg('10–120 seconds per document, text extraction and OCR together.')} error={timeoutValid ? undefined : msg('Enter a whole number from 10 to 120.')}>
        {w => <Input {...w} type="number" min={low} max={high} step={1} value={current.timeoutSeconds} disabled={save.isPending} onChange={event => edit({ timeoutSeconds: Number(event.target.value) })} />}</Field>
    </FieldGroup>
    <p className={styles.note}>{msg('Applies to new PDFs from uploads, Drive, connected files and links. Text already extracted stays as it is.')}</p>
    {unready && <Alert>{msg('{{name}} is not available on this computer. With OCR on, every document read stops until its programs are installed: text PDFs and plain text too, from uploads, Drive, connected files and links.', { name: selected.name })}</Alert>}
    {unready && draft && <label className={styles.label}><input type="checkbox" checked={draft.acknowledged} disabled={save.isPending} onChange={event => { const acknowledged = event.target.checked; setDraft(d => d && { ...d, acknowledged }) }} />{msg('Turn OCR on anyway')}</label>}
    {restart && <RestartWarning mode={current.mode} />}
    {draft && <div className={styles.actions}>
      <Button variant="quiet" disabled={save.isPending} onClick={() => { setDraft(null); save.reset() }}>{msg('Cancel')}</Button>
      <Button variant="primary" disabled={save.isPending || !timeoutValid || current.mode === 'auto' && !selected?.enabled || unready && !draft.acknowledged} onClick={() => save.mutate()}>{restart ? msg('Save and restart') : msg('Save changes')}</Button>
    </div>}
    {save.error && <Alert>{save.error.message}</Alert>}
    {!draft && selected?.enabled && <TestPDF settings={settings} connection={selected} />}
  </div>
}

function OCRProcessors({ read, onSaved, onError }: { read: Settings; onSaved: (next: Settings) => void; onError: (error: Error) => Promise<void> }) {
  const settings = read.result
  const opener = useRef<HTMLElement | null>(null)
  const [editor, setEditor] = useState<{ base: Settings; value: OCRConnection } | null>(null)
  const [discard, setDiscard] = useState(false)
  const original = editor && editor.base.result.connections.find(c => c.id === editor.value.id)
  const dirty = !!editor && JSON.stringify(editor.value) !== JSON.stringify(original)
  useUnsavedChanges(dirty)
  const save = useMutation({
    mutationFn: async () => {
      if (!editor) throw new Error(sourceMessage('There is nothing to save.'))
      const list = editor.base.result.connections, known = list.some(c => c.id === editor.value.id)
      return saveProcessing(editor.base.result, { ...editor.base.result, connections: known ? list.map(c => c.id === editor.value.id ? editor.value : c) : [...list, editor.value] }, restartsGateway(editor.base.result.mode, editor.base.localGateway))
    },
    // The typed credential is dropped with the editor: nothing keeps it.
    onError,
    onSuccess: next => { onSaved(next); setEditor(null) }
  })
  const close = () => { if (!save.isPending) { if (dirty) setDiscard(true); else setEditor(null) } }
  const edit = (patch: Partial<OCRConnection>) => setEditor(e => e && ({ ...e, value: { ...e.value, ...patch } }))
  const restart = !!editor && restartsGateway(editor.base.result.mode, editor.base.localGateway)
  const problem = editor ? draftProblem(editor.value, original ?? undefined) : ''
  return <div className={styles.stack}>
    <div className={styles.toolbar}>
      <p className={styles.note}>{msg('OCR processors on this computer. A cloud processor’s credential is entered once and never shown again.')}</p>
      <Button disabled={settings.connections.length >= 16} onClick={event => {
        opener.current = event.currentTarget; save.reset()
        setEditor({ base: read, value: { id: 'ocr-' + crypto.randomUUID().replaceAll('-', '').slice(0, 24), name: '', kind: 'tesseract', enabled: true } })
      }}>{msg('Add processor')}</Button>
    </div>
    {!settings.connections.length && <p className={styles.note}>{msg('Add a local or cloud OCR processor, then choose it above.')}</p>}
    {settings.connections.map(c => <SettingRow key={c.id} title={c.name} description={processorName(c.kind)}
      status={!c.enabled ? msg('Disabled') : !c.ready ? msg('Not available on this computer') : cloudOCR(c.kind) ? msg('Credential held · Test a PDF to check it') : msg('Available on this computer')}
      action={<Button variant="quiet" aria-label={msg('Manage {{name}}', { name: c.name })} onClick={event => { opener.current = event.currentTarget; save.reset(); setEditor({ base: read, value: c }) }}>{msg('Manage')}</Button>} />)}
    <Dialog open={!!editor && !discard} onOpenChange={open => { if (!open) close() }} title={msg('OCR processor')} openerRef={opener}
      footer={editor && <DialogActions><Button variant="quiet" disabled={save.isPending} onClick={close}>{msg('Cancel')}</Button>
        <Button variant="primary" disabled={save.isPending || !dirty || !!problem} onClick={() => save.mutate()}>{restart ? msg('Save and restart') : msg('Save')}</Button></DialogActions>}>
      {editor && <div className={styles.stack}>
        <FieldGroup>
          <Field label={msg('Name')}>{w => <Input {...w} value={editor.value.name} maxLength={80} disabled={save.isPending} onChange={event => edit({ name: event.target.value })} />}</Field>
          <Field label={msg('Processor')}>{w => <Select {...w} value={editor.value.kind} disabled={save.isPending}
            onValueChange={kind => setEditor(e => e && ({ ...e, value: { id: e.value.id, name: e.value.name, enabled: e.value.enabled, kind: kind as OCRKind } }))}
            options={(['tesseract', 'google-document-ai', 'azure-document-intelligence', 'aws-textract', 'program'] as const).map(kind => ({ value: kind, label: processorName(kind) }))} />}</Field>
          {editor.value.kind === 'program' && <Field label={msg('Program path')} hint={msg('An absolute path to a program in the OCR tools bundle beside the gateway, or in /usr/bin. No shell command or arguments.')}>
            {w => <Input {...w} value={editor.value.program ?? ''} spellCheck={false} disabled={save.isPending} onChange={event => edit({ program: event.target.value })} />}</Field>}
          <CloudFields value={editor.value} edit={edit} disabled={save.isPending} />
        </FieldGroup>
        {!cloudOCR(editor.value.kind) && <p className={styles.note}>{editor.value.kind === 'tesseract' ? msg('Local OCR runs Tesseract and Poppler from /usr/bin on this computer. Pages are not sent anywhere.') : msg('The program is given the PDF and the page numbers, and answers each page’s text.')}</p>}
        <label className={styles.label}><input type="checkbox" checked={editor.value.enabled} disabled={save.isPending || settings.connection === editor.value.id && settings.mode === 'auto'} onChange={event => edit({ enabled: event.target.checked })} />{msg('Enabled')}</label>
        {settings.connection === editor.value.id && settings.mode === 'auto' && <p className={styles.note}>{msg('Choose another processor, or turn OCR off, before disabling this one.')}</p>}
        {problem && <p className={styles.note}>{problem}</p>}
        {restart && <RestartWarning mode={editor.base.result.mode} />}
        {save.error && <Alert>{save.error.message}</Alert>}
        {!dirty && original?.enabled && <TestPDF settings={editor.base.result} connection={original} />}
      </div>}
    </Dialog>
    <Dialog open={discard} onOpenChange={setDiscard} title={msg('Discard changes?')} description={msg('Your unsaved changes will be lost.')}
      footer={<DialogActions><Button onClick={() => setDiscard(false)}>{msg('Keep editing')}</Button><Button variant="danger" onClick={() => { setDiscard(false); setEditor(null) }}>{msg('Discard changes')}</Button></DialogActions>}>{null}</Dialog>
  </div>
}

/** Why a processor's draft cannot be saved yet, or nothing. */
function draftProblem(c: OCRConnection, saved: OCRConnection | undefined): string {
  if (!c.name.trim()) return sourceMessage('Enter a name.')
  if (c.kind === 'program') return c.program?.startsWith('/') ? '' : sourceMessage('Enter the program’s absolute path.')
  if (!cloudOCR(c.kind)) return ''
  // A credential is kept only for the destination it was entered for.
  const held = !!saved?.credentialConfigured && sameDestination(c, saved)
  if (!c.credential && !held) return saved?.credentialConfigured ? sourceMessage('Enter the credential again: it is sent only to the destination it was entered for.') : sourceMessage('Enter the credential.')
  if (c.kind === 'google-document-ai' && !(c.project?.trim() && c.location?.trim() && c.processor?.trim())) return sourceMessage('Enter the project, location and processor.')
  if (c.kind === 'azure-document-intelligence' && !c.endpoint?.startsWith('https://')) return sourceMessage('Enter the resource’s HTTPS endpoint.')
  if (c.kind === 'aws-textract') {
    if (!c.region?.trim()) return sourceMessage('Enter the region.')
    if (c.credential) {
      try { const v = JSON.parse(c.credential) as { accessKeyId?: string; secretAccessKey?: string }; if (!v.accessKeyId || !v.secretAccessKey) return sourceMessage('Enter the access key ID and the secret access key.') }
      catch { return sourceMessage('Enter the access key ID and the secret access key.') }
    }
  }
  return ''
}

function CloudFields({ value: c, edit, disabled }: { value: OCRConnection; edit: (patch: Partial<OCRConnection>) => void; disabled: boolean }) {
  if (!cloudOCR(c.kind)) return null
  const secretHint = c.credentialConfigured ? msg('A credential is held. Leave this blank to keep it for the same destination.') : msg('Kept by the gateway in its private store on this computer, outside the project. It is never shown again.')
  let aws: { accessKeyId?: string; secretAccessKey?: string; sessionToken?: string } = {}
  if (c.kind === 'aws-textract' && c.credential) { try { aws = JSON.parse(c.credential) as typeof aws } catch { /* an incomplete draft */ } }
  const awsEdit = (patch: Partial<typeof aws>) => {
    const next = Object.fromEntries(Object.entries({ ...aws, ...patch }).filter(([, v]) => v))
    edit({ credential: Object.keys(next).length ? JSON.stringify(next) : '' })
  }
  return <>
    {c.kind === 'google-document-ai' && <>
      <Field label={msg('Google Cloud project ID')}>{w => <Input {...w} value={c.project ?? ''} disabled={disabled} onChange={event => edit({ project: event.target.value.trim() })} />}</Field>
      <Field label={msg('Location')} hint={msg('The location of your Document AI processor, such as us or eu.')}>{w => <Input {...w} value={c.location ?? ''} disabled={disabled} onChange={event => edit({ location: event.target.value.trim() })} />}</Field>
      <Field label={msg('Processor ID')} hint={msg('A Document OCR processor the service account may use.')}>{w => <Input {...w} value={c.processor ?? ''} disabled={disabled} onChange={event => edit({ processor: event.target.value.trim() })} />}</Field>
      <Field label={msg('Service account JSON')} hint={secretHint}>{w => <TextArea {...w} className={styles.credential} value={c.credential ?? ''} spellCheck={false} autoComplete="off" disabled={disabled} onChange={event => edit({ credential: event.target.value })} />}</Field>
    </>}
    {c.kind === 'azure-document-intelligence' && <>
      <Field label={msg('Azure endpoint')} hint={msg('The HTTPS endpoint of your Document Intelligence resource, under cognitiveservices.azure.com. It uses prebuilt-read.')}>{w => <Input {...w} value={c.endpoint ?? ''} spellCheck={false} disabled={disabled} onChange={event => edit({ endpoint: event.target.value.trim() })} />}</Field>
      <Field label={msg('API key')} hint={secretHint}>{w => <Input {...w} type="password" autoComplete="new-password" value={c.credential ?? ''} disabled={disabled} onChange={event => edit({ credential: event.target.value.trim() })} />}</Field>
    </>}
    {c.kind === 'aws-textract' && <>
      <Field label={msg('AWS region')} hint={msg('A region where Textract is available. The credential needs textract:DetectDocumentText.')}>{w => <Input {...w} value={c.region ?? ''} disabled={disabled} onChange={event => edit({ region: event.target.value.trim() })} />}</Field>
      <Field label={msg('Access key ID')} hint={secretHint}>{w => <Input {...w} type="password" autoComplete="new-password" value={aws.accessKeyId ?? ''} disabled={disabled} onChange={event => awsEdit({ accessKeyId: event.target.value.trim() })} />}</Field>
      <Field label={msg('Secret access key')}>{w => <Input {...w} type="password" autoComplete="new-password" value={aws.secretAccessKey ?? ''} disabled={disabled} onChange={event => awsEdit({ secretAccessKey: event.target.value.trim() })} />}</Field>
      <Field label={msg('Session token (optional)')}>{w => <Input {...w} type="password" autoComplete="new-password" value={aws.sessionToken ?? ''} disabled={disabled} onChange={event => awsEdit({ sessionToken: event.target.value.trim() })} />}</Field>
    </>}
    <p className={styles.note}>{msg('Only pages that need OCR are sent to this provider, and the provider may charge for each. Saving does not check the credential: test a scanned PDF after saving.')}</p>
  </>
}

/** One PDF through a saved processor: a short preview, kept nowhere. */
function TestPDF({ settings, connection }: { settings: ProcessingSettings; connection: OCRConnection }) {
  const input = useRef<HTMLInputElement>(null), abort = useRef<AbortController | null>(null)
  useEffect(() => () => abort.current?.abort(), [])
  const test = useMutation({
    mutationFn: async (file: File) => {
      if (!file.name.toLowerCase().endsWith('.pdf') || file.size === 0 || file.size > TEST_PDF_BYTES) throw new Error(sourceMessage('Choose a PDF of at most 4 MiB.'))
      abort.current?.abort()
      const controller = new AbortController(); abort.current = controller
      const bytes = base64(new Uint8Array(await file.arrayBuffer()))
      return (await processingCall<ProcessingTest>('test', { connection: connection.id, revision: settings.sha256, document: { name: file.name, mediaType: 'application/pdf', bytes } }, controller.signal)).result
    }
  })
  const reset = test.reset
  useEffect(() => { abort.current?.abort(); reset() }, [connection.id, settings.sha256, reset])
  return <div className={styles.stack}>
    <p className={styles.note}>{cloudOCR(connection.kind) ? msg('A test sends the scanned pages to {{provider}} with {{connection}}. The provider may charge for them.', { provider: processorName(connection.kind), connection: connection.name })
      : connection.kind === 'tesseract' ? msg('A test reads the scanned pages on this computer.') : msg('A test gives the PDF to the OCR program on this computer.')}</p>
    <div className={styles.actions}>
      <input ref={input} type="file" accept="application/pdf,.pdf" hidden aria-label={msg('Test PDF file')} onChange={event => { const file = event.target.files?.[0]; event.target.value = ''; if (file) test.mutate(file) }} />
      <Button disabled={test.isPending} onClick={() => input.current?.click()}>{test.isPending ? msg('Reading the PDF…') : msg('Test a PDF')}</Button>
      <span className={styles.note}>{msg('At most 4 MiB. The text is not saved to a chat or sent to the assistant.')}</span>
    </div>
    {test.error && <Alert>{test.error.message}</Alert>}
    {test.data && <div className={styles.stack} role="status">
      <p className={styles.note}>{msg('Result: {{status}} · {{count}} pages', { status: test.data.processing.status, count: test.data.pageCount })}</p>
      {test.data.extraction === 'text-layer' && <p className={styles.note}>{msg('This PDF has readable text, so OCR did not run and the processor was not tested.')}</p>}
      {test.data.processing.errors.map((e, i) => <p className={styles.note} key={i}><code>{e.code}</code>{e.page !== null && <> · {msg('Page {{number}}', { number: e.page })}</>}</p>)}
      {test.data.pages.map(page => <div key={page.number}>
        <p className={styles.note}>{msg('Page {{number}}', { number: page.number })} · <code>{page.extraction}</code></p>
        <pre className={styles.preview}>{page.text || msg('No readable text')}</pre>
      </div>)}
    </div>}
  </div>
}
