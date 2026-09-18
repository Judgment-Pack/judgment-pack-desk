import { useEffect, useState } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import { msg, systemMessage, useLocale } from '../i18n'
import { useEffectiveConfig } from '../config/DeskConfigProvider'
import { decodeDeskConfig, DOCUMENT_DEFAULTS, DOCUMENT_LIMIT_BOUNDS, type DocumentSourceConfig, type EffectiveConfig } from '../config/deskConfig'
import { DESK_CONFIG_QUERY_KEY } from '../config/queries'
import { answer, deskFetch } from '../files/client'
import { Button } from '../ui/Button'
import { Field, FieldGroup } from '../ui/Field'
import { Input } from '../ui/Input'
import { SettingsSection } from '../ui/SettingsSection'
import { useUnsavedChanges } from '../shell/DraftScope'

export function DocumentSettings() {
  useLocale()
  const effective = useEffectiveConfig(), client = useQueryClient()
  const seed = () => ({ gateway: effective.config.research.gateway ?? { url: '', authority: '', signer: { algorithm: 'ed25519' as const, public: '' } }, documents: effective.config.research.documents ?? DOCUMENT_DEFAULTS, enabled: Boolean(effective.config.research.documents), digest: effective.desk?.sha256 })
  const [base, setBase] = useState(seed), [draft, setDraft] = useState(seed)
  const [busy, setBusy] = useState(false), [error, setError] = useState(''), [saved, setSaved] = useState(false)
  const dirty = JSON.stringify(draft) !== JSON.stringify(base)
  useUnsavedChanges(dirty)
  useEffect(() => { if (!dirty && effective.desk?.sha256 !== base.digest) { const next = seed(); setBase(next); setDraft(next) } }, [effective, dirty, base.digest])
  const change = (patch: Partial<typeof draft>) => { setDraft({ ...draft, ...patch }); setError(''); setSaved(false) }
  const save = async () => {
    if (busy || draft.digest === undefined) return
    const research = { ...effective.config.research, gateway: draft.gateway.url ? draft.gateway : null, documents: draft.enabled ? draft.documents : null }
    const decoded = decodeDeskConfig(JSON.stringify({ deskConfigVersion: 1, research }), 'desk')
    if (decoded.problems.length) { setError(decoded.problems.map(p => `${p.key}: ${systemMessage(p.reason)}`).join('\n')); return }
    if (draft.enabled && !research.gateway) { setError(msg('Configure a gateway and signing key before enabling document uploads.')); return }
    setBusy(true); setError(''); setSaved(false)
    try {
      const result = await answer<{ sha256: string; path: string }>(await deskFetch('/api/desk-config', { method: 'PUT', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ research, ifMatch: draft.digest }) }))
      const next = { ...draft, digest: result.sha256 }; setBase(next); setDraft(next); setSaved(true)
      client.setQueryData<EffectiveConfig>(DESK_CONFIG_QUERY_KEY, previous => previous && ({ ...previous, config: { ...previous.config, research: decoded.values!.research! }, desk: { ...previous.desk, present: true, path: result.path, sha256: result.sha256, problems: [] } }))
      void client.invalidateQueries({ queryKey: DESK_CONFIG_QUERY_KEY })
    } catch (cause) { setError((cause as Error).message) }
    finally { setBusy(false) }
  }
  const labels = { maxFileBytes: msg('Maximum file size (bytes)'), maxRequestBytes: msg('Gateway request limit (bytes)'), maxResponseBytes: msg('Extraction response limit (bytes)') }
  return <SettingsSection title={msg('Documents')} level={2} variant="plain" description={msg('Personal document connection. Originals and extraction receipts are kept in your private chat storage and included in backups.')}
    footer={<><Button disabled={!dirty || busy || draft.digest === undefined} onClick={() => void save()}>{busy ? msg('Saving…') : msg('Save')}</Button>{error && <Button variant="quiet" disabled={busy} onClick={() => { void client.invalidateQueries({ queryKey: DESK_CONFIG_QUERY_KEY }); const next = seed(); setBase(next); setDraft(next); setError('') }}>{msg('Reload')}</Button>}</>}>
    <FieldGroup>
      <label className="checkbox"><input type="checkbox" checked={draft.enabled} disabled={busy} onChange={e => change({ enabled: e.target.checked })} />{msg('Enable document uploads through the gateway')}</label>
      <Field label={msg('Gateway URL')}>{w => <Input {...w} value={draft.gateway.url} disabled={busy} onChange={e => change({ gateway: { ...draft.gateway, url: e.target.value } })} />}</Field>
      <Field label={msg('Gateway authority')}>{w => <Input {...w} value={draft.gateway.authority} disabled={busy} onChange={e => change({ gateway: { ...draft.gateway, authority: e.target.value } })} />}</Field>
      <Field label={msg('Signing public key')} hint={msg('Use the Ed25519 public key printed by gateway keygen. Never enter the private signing key.')}>{w => <Input {...w} value={draft.gateway.signer.public} disabled={busy} onChange={e => change({ gateway: { ...draft.gateway, signer: { algorithm: 'ed25519', public: e.target.value } } })} />}</Field>
      <Field label={msg('Document source name')}>{w => <Input {...w} value={draft.documents.source} disabled={busy} onChange={e => change({ documents: { ...draft.documents, source: e.target.value } })} />}</Field>
      <details><summary>{msg('Upload limits')}</summary><FieldGroup>{(Object.keys(DOCUMENT_LIMIT_BOUNDS) as (keyof Omit<DocumentSourceConfig,'source'>)[]).map(key => <Field key={key} label={labels[key]}>{w => <Input {...w} type="number" min={DOCUMENT_LIMIT_BOUNDS[key][0]} max={DOCUMENT_LIMIT_BOUNDS[key][1]} value={draft.documents[key]} disabled={busy} onChange={e => change({ documents: { ...draft.documents, [key]: Number(e.target.value) } })} />}</Field>)}</FieldGroup>
        <p>{msg('Match limits to the gateway and document adapter. Canceling in Desk does not stop gateway work already in progress.')}</p>
      </details>
      <p>{msg('PDFs require the document adapter. Scanned pages need OCR configured on the gateway. Google Drive is not connected by these settings.')}</p>
      <p>{msg('Removing a file from a message does not erase its retained original. Unsent and canceled uploads also remain in private storage and backups.')}</p>
      {error && <p role="alert">{systemMessage(error)}</p>}{saved && <p role="status">{msg('Saved.')}</p>}
    </FieldGroup>
  </SettingsSection>
}
