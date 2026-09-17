import { useRef, useState } from 'react'
import { answer, deskFetch } from '../files/client'
import { Button } from '../ui/Button'
import { Dialog, DialogActions } from '../ui/Dialog'
import { Field, FieldGroup } from '../ui/Field'
import { Input } from '../ui/Input'
import { SettingsSection } from '../ui/SettingsSection'
import { formatStorageBytes, type ChatStorageStatus } from './chatStorage'
import styles from './ChatDataSettings.module.css'

export function ChatBackupSettings({ status, blocked, onRestored }: { status: ChatStorageStatus; blocked: boolean; onRestored: () => void }) {
  const [open, setOpen] = useState(false)
  const [revision, setRevision] = useState('')
  const [path, setPath] = useState('')
  const [file, setFile] = useState<File | null>(null)
  const [busy, setBusy] = useState<'backup' | 'restore' | null>(null)
  const [error, setError] = useState('')
  const [notice, setNotice] = useState('')
  const opener = useRef<HTMLButtonElement>(null)
  async function download() {
    if (busy || blocked) return
    setBusy('backup'); setError(''); setNotice('')
    try {
      const response = await deskFetch('/api/storage/backup')
      if (!response.ok) await answer(response)
      const blob = await response.blob()
      const url = URL.createObjectURL(blob)
      const link = document.createElement('a')
      link.href = url; link.download = `jpack-chat-backup-${new Date().toISOString().slice(0, 10)}.zip`
      document.body.append(link); link.click(); link.remove()
      setTimeout(() => URL.revokeObjectURL(url), 1000)
      setNotice('Backup download started.')
    } catch (cause) { setError((cause as Error).message) }
    finally { setBusy(null) }
  }
  async function restore() {
    if (!file || !path.trim() || busy || blocked) return
    setBusy('restore'); setError(''); setNotice('')
    try {
      const body = new FormData()
      body.append('settings', JSON.stringify({ path: path.trim(), revision }))
      body.append('backup', file)
      await answer(await deskFetch('/api/storage/restore', { method: 'POST', body }))
      onRestored()
    } catch (cause) { setError((cause as Error).message); setBusy(null) }
  }
  const tooLarge = Boolean(file && file.size > status.maxBackupBytes + 2 * 1024 ** 2)
  return <SettingsSection title="Backups & exports" variant="plain" description="Download all saved chat history, drafts and retained source text. Pack files and credential settings are kept separately.">
    <div className={styles.details}>
      <p className={styles.caption}>Backups are unencrypted and may contain private messages and source material. Keep them in a private location.</p>
      <div className={styles.actions}>
        <Button disabled={blocked || busy !== null || status.bytes > status.maxBackupBytes || Boolean(status.problem)} onClick={() => void download()}>{busy === 'backup' ? 'Preparing backup…' : 'Download chat backup'}</Button>
        <Button ref={opener} disabled={blocked || busy !== null} onClick={() => { setOpen(true); setRevision(status.revision); setFile(null); setPath(''); setError(''); setNotice('') }}>Restore backup…</Button>
      </div>
      <p className={styles.caption}>Saved data only; unsent messages are kept in this browser. Backup limit: {formatStorageBytes(status.maxBackupBytes)}.</p>
      {notice && <p role="status" className={styles.caption}>{notice}</p>}
      {error && !open && <p role="alert">{error}</p>}
    </div>
    <Dialog open={open} onOpenChange={value => { if (busy !== 'restore') setOpen(value) }} title="Restore chat backup" openerRef={opener}
      description="Restore replaces the active chat store for all projects. Current files remain in their original folder for recovery. Desk reloads after switching to the restored data.">
      <form onSubmit={event => { event.preventDefault(); void restore() }}>
        <FieldGroup>
          <Field label="Chat backup" error={tooLarge ? 'This backup exceeds the supported size.' : undefined}>
            {wiring => <Input {...wiring} type="file" accept=".zip,application/zip" disabled={busy === 'restore'} onChange={event => setFile(event.target.files?.[0] ?? null)} />}
          </Field>
          <Field label="Restore into" hint="Use a new or empty private folder on the computer running Desk, outside the project and current data folder.">
            {wiring => <Input {...wiring} value={path} autoComplete="off" spellCheck={false} disabled={busy === 'restore'} onChange={event => setPath(event.target.value)} />}
          </Field>
          <p className={styles.caption}>Existing chats are not merged. Chats keep their project association. After moving a project, use its previous folder path to recover its history.</p>
          {error && <p role="alert">{error}</p>}
          {busy === 'restore' && <p role="status" className={styles.caption}>Uploading, verifying and restoring chat data…</p>}
        </FieldGroup>
        <DialogActions><Button disabled={busy === 'restore'} onClick={() => setOpen(false)}>Cancel</Button>
          <Button type="submit" variant="primary" disabled={blocked || busy !== null || !file || !path.trim() || path.trim() === status.path || tooLarge}>{busy === 'restore' ? 'Restoring…' : 'Restore and reload'}</Button></DialogActions>
      </form>
    </Dialog>
  </SettingsSection>
}
