import { sourceMessage } from '../i18n/source'
import { Message } from '../i18n/Message'
import { msg, useLocale, systemMessage } from '../i18n'
import { useRef, useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useChats } from '../chat/ChatProvider'
import { answer, deskFetch } from '../files/client'
import { Button } from '../ui/Button'
import { Dialog, DialogActions } from '../ui/Dialog'
import { Field, FieldGroup } from '../ui/Field'
import { Input } from '../ui/Input'
import { SettingsSection } from '../ui/SettingsSection'
import { ChatBackupSettings } from './ChatBackupSettings'
import { CardField } from './SourceCard'
import styles from './ChatDataSettings.module.css'

import { chatStorageQueryKey as queryKey, formatStorageBytes, type ChatStorageStatus } from './chatStorage'
import { ProjectHistorySettings } from './ProjectHistorySettings'

export function ChatDataSettings() {
  useLocale()
  const queryClient = useQueryClient()
  const query = useQuery({ queryKey, queryFn: ({ signal }) => deskFetch('/api/storage', { signal }).then(answer<ChatStorageStatus>), retry: false })
  const { store, saving, dirty, error: chatError } = useChats()
  const [edit, setEdit] = useState<ChatStorageStatus | null>(null)
  const [path, setPath] = useState('')
  const [moving, setMoving] = useState(false)
  const [error, setError] = useState('')
  const [notice, setNotice] = useState('')
  const opener = useRef<HTMLButtonElement>(null)
  const pendingWork = Boolean(store?.running || saving || dirty)
  const blocked = pendingWork || Boolean(chatError)
  const status = query.data
  async function move() {
    if (!edit || moving || blocked || !path.trim() || path.trim() === edit.path) return
    setMoving(true); setError(''); setNotice('')
    try {
      if (store && !await store.flush()) throw new Error(sourceMessage("Save or recover unsaved chat changes before moving data."))
      const next = await answer<ChatStorageStatus>(await deskFetch('/api/storage/move', {
        method: "POST", headers: { 'Content-Type': "application/json" }, body: JSON.stringify({ path: path.trim(), revision: edit.revision })
      }))
      queryClient.setQueryData(queryKey, next)
      setEdit(null)
      setNotice(sourceMessage("Chat data moved. The original folder was kept as a recovery copy."))
    } catch (cause) {
      setError((cause as Error).message)
      void query.refetch()
    } finally { setMoving(false) }
  }
  return <><SettingsSection title={msg("Chat data")} variant="plain" description={msg("Private history, drafts and retained source text for all projects on this Desk.")}>
    {query.isPending && <p role="status" className={styles.caption}>{msg("Reading chat storage…")}</p>}
    {query.isError && <div role="alert"><p>{query.error.message}</p><Button onClick={() => void query.refetch()}>{msg("Retry")}</Button></div>}
    {status && <div className={styles.details}>
      <CardField label={msg("Location")}><code>{status.path}</code></CardField>
      <CardField label={msg("Usage")}>{formatStorageBytes(status.bytes)} · {status.projectCount} {status.projectCount === 1 ? msg("project") : msg("projects")}</CardField>
      <CardField label={msg("This project")}>{formatStorageBytes(status.projectBytes)}</CardField>
      <p className={styles.caption}>{msg("This is a folder on the computer running Desk. Chats stay separate from project files. API keys remain in protected settings.")}</p>
      {status.legacy && <p className={styles.caption}>{msg("Your existing history is still in the settings folder. You can move it to a dedicated data folder.")}</p>}
      {status.previousPath && <p className={styles.caption}><Message text={"Recovery copy: <0/>. New changes are saved only to the current location."} slots={[<code>{status.previousPath}</code>]} /></p>}
      <div className={styles.actions}><Button ref={opener} disabled={blocked || query.isFetching || query.isError || Boolean(status.problem)} onClick={() => { setEdit(status); setPath(status.legacy ? status.recommendedPath : ''); setError('') }}>{msg("Change location…")}</Button>
        <Button variant="quiet" disabled={query.isFetching} onClick={() => void query.refetch()}>{msg("Refresh usage")}</Button>
        {status.projectBytes === 0 && <ProjectHistorySettings blocked={blocked || moving} onLinked={() => window.location.reload()} />}</div>
      {blocked && <p className={styles.caption}>{msg("Finish or stop active work and save chat changes before moving data.")}</p>}
    </div>}
    {(chatError || status?.problem) && <p role="alert">{chatError || status?.problem}</p>}
    {notice && <p role="status" className={styles.caption}>{systemMessage(notice)}</p>}
    <Dialog open={edit !== null} onOpenChange={open => { if (!open && !moving) setEdit(null) }} title={msg("Move chat data")} openerRef={opener}
      description={msg("All saved chats move together. Desk verifies the copy before switching locations and keeps the original folder for recovery.")}>
      <form onSubmit={event => { event.preventDefault(); void move() }}>
        <FieldGroup>
          <Field label={msg("New folder")} hint={msg("Enter an absolute path on the computer running Desk. Use a new or empty private folder outside the project.")}>
            {wiring => <Input {...wiring} autoComplete="off" spellCheck={false} value={path} disabled={moving} onChange={event => setPath(event.target.value)} />}
          </Field>
          <p className={styles.caption}>{msg("Close older Desk versions before moving. Other current Desk windows will follow the new location.")}</p>
          {edit && edit.bytes > edit.maxMoveBytes && <p role="alert"><Message text={"This store exceeds the supported move size of <0/>."} slots={[formatStorageBytes(edit.maxMoveBytes)]} /></p>}
          {error && <div role="alert"><p>{systemMessage(error)}</p><Button disabled={moving || query.isFetching} onClick={() => { void query.refetch().then(result => { if (result.data) { setEdit(result.data); setError('') } }) }}>{msg("Reload settings")}</Button></div>}
          {moving && <p role="status" className={styles.caption}>{msg("Copying and verifying chat data…")}</p>}
        </FieldGroup>
        <DialogActions><Button disabled={moving} onClick={() => setEdit(null)}>{msg("Cancel")}</Button>
          <Button type="submit" variant="primary" disabled={moving || blocked || !path.trim() || path.trim() === edit?.path || Boolean(edit && edit.bytes > edit.maxMoveBytes)}>{moving ? msg("Moving…") : msg("Move data")}</Button></DialogActions>
      </form>
    </Dialog>
  </SettingsSection>
  {status && <ChatBackupSettings status={status} blocked={pendingWork || moving || query.isError} onRestored={() => window.location.reload()} />}
  </>
}
