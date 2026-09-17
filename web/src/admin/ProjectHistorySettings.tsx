import { useRef, useState } from 'react'
import { answer, deskFetch } from '../files/client'
import { Button } from '../ui/Button'
import { Dialog, DialogActions } from '../ui/Dialog'
import { Field, FieldGroup } from '../ui/Field'
import { Input } from '../ui/Input'
import styles from './ChatDataSettings.module.css'
interface Preview { previousProject: string; project: string; chatCount: number; sourceRevision: string; bindingsRevision: string }
export function ProjectHistorySettings({ blocked, onLinked }: { blocked: boolean; onLinked: () => void }) {
  const [open, setOpen] = useState(false)
  const [path, setPath] = useState('')
  const [preview, setPreview] = useState<Preview | null>(null)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const opener = useRef<HTMLButtonElement>(null)
  async function submit() {
    if (busy || blocked || !path.trim()) return
    setBusy(true); setError('')
    try {
      const data = await answer<Preview>(await deskFetch(`/api/storage/project-history/${preview ? 'relink' : 'preview'}`, {
        method: 'POST', headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ previousProject: path.trim(), sourceRevision: preview?.sourceRevision ?? '', bindingsRevision: preview?.bindingsRevision ?? '' })
      }))
      if (preview) { onLinked(); return }
      setPreview(data)
    } catch (cause) { setError((cause as Error).message); setPreview(null) }
    setBusy(false)
  }
  return <>
    <Button ref={opener} variant="quiet" disabled={blocked} onClick={() => { setOpen(true); setPath(''); setPreview(null); setError('') }}>Recover project history…</Button>
    <Dialog open={open} onOpenChange={value => { if (!busy) setOpen(value) }} title="Recover project history" openerRef={opener}
      description="If this project moved to a different folder, link it to its previous chat history. No histories are combined.">
      <form onSubmit={event => { event.preventDefault(); void submit() }}>
        <FieldGroup>
          <Field label="Previous project folder" hint="Enter the absolute folder path used before the move. The old folder does not need to exist.">
            {wiring => <Input {...wiring} value={path} autoComplete="off" spellCheck={false} disabled={busy} onChange={event => { setPath(event.target.value); setPreview(null) }} />}
          </Field>
          {preview && <div className={styles.details}>
            <p>{preview.chatCount} saved {preview.chatCount === 1 ? 'chat' : 'chats'} found.</p>
            <p className={styles.caption}>Link these chats to <code>{preview.project}</code>? Opening the old project location will use the same history. If this is a separate copy of the project, keep a separate history.</p>
          </div>}
          {error && <p role="alert">{error}</p>}
        </FieldGroup>
        <DialogActions><Button disabled={busy} onClick={() => setOpen(false)}>Cancel</Button>
          <Button type="submit" variant="primary" disabled={blocked || busy || !path.trim()}>{busy ? 'Working…' : preview ? 'Link history and reload' : 'Find history'}</Button></DialogActions>
      </form>
    </Dialog>
  </>
}
