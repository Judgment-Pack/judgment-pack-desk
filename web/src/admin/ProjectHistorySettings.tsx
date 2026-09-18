import { Message } from '../i18n/Message'
import { msg, useLocale, systemMessage } from '../i18n'
import { useRef, useState } from 'react'
import { answer, deskFetch } from '../files/client'
import { Button } from '../ui/Button'
import { Dialog, DialogActions } from '../ui/Dialog'
import { Field, FieldGroup } from '../ui/Field'
import { Input } from '../ui/Input'
import styles from './ChatDataSettings.module.css'
interface Preview { previousProject: string; project: string; chatCount: number; sourceRevision: string; bindingsRevision: string }
export function ProjectHistorySettings({ blocked, onLinked }: { blocked: boolean; onLinked: () => void }) {
  useLocale()
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
        method: "POST", headers: { 'Content-Type': "application/json" },
        body: JSON.stringify({ previousProject: path.trim(), sourceRevision: preview?.sourceRevision ?? '', bindingsRevision: preview?.bindingsRevision ?? '' })
      }))
      if (preview) { onLinked(); return }
      setPreview(data)
    } catch (cause) { setError((cause as Error).message); setPreview(null) }
    setBusy(false)
  }
  return <>
    <Button ref={opener} variant="quiet" disabled={blocked} onClick={() => { setOpen(true); setPath(''); setPreview(null); setError('') }}>{msg("Recover project history…")}</Button>
    <Dialog open={open} onOpenChange={value => { if (!busy) setOpen(value) }} title={msg("Recover project history")} openerRef={opener}
      description={msg("If this project moved to a different folder, link it to its previous chat history. No histories are combined.")}>
      <form onSubmit={event => { event.preventDefault(); void submit() }}>
        <FieldGroup>
          <Field label={msg("Previous project folder")} hint={msg("Enter the absolute folder path used before the move. The old folder does not need to exist.")}>
            {wiring => <Input {...wiring} value={path} autoComplete="off" spellCheck={false} disabled={busy} onChange={event => { setPath(event.target.value); setPreview(null) }} />}
          </Field>
          {preview && <div className={styles.details}>
            <p><Message text={"<0/> saved <1/> found."} slots={[preview.chatCount, preview.chatCount === 1 ? msg("chat") : msg("chats")]} /></p>
            <p className={styles.caption}><Message text={"Link these chats to <0/>? Opening the old project location will use the same history. If this is a separate copy of the project, keep a separate history."} slots={[<code>{preview.project}</code>]} /></p>
          </div>}
          {error && <p role="alert">{systemMessage(error)}</p>}
        </FieldGroup>
        <DialogActions><Button disabled={busy} onClick={() => setOpen(false)}>{msg("Cancel")}</Button>
          <Button type="submit" variant="primary" disabled={blocked || busy || !path.trim()}>{busy ? msg("Working…") : preview ? msg("Link history and reload") : msg("Find history")}</Button></DialogActions>
      </form>
    </Dialog>
  </>
}
