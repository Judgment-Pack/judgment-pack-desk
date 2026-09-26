import { useState, type RefObject } from 'react'
import { msg, useLocale } from '../i18n'
import { signOut } from '../mcp/session'
import { Button } from '../ui/Button'
import { Dialog, DialogActions } from '../ui/Dialog'

export function SignOutDialog({ open, onOpenChange, openerRef }: {
  open: boolean; onOpenChange: (open: boolean) => void; openerRef: RefObject<HTMLElement | null>
}) {
  useLocale()
  const [busy, setBusy] = useState(false)
  const [failed, setFailed] = useState(false)
  return <Dialog open={open} onOpenChange={value => { if (!busy) { setFailed(false); onOpenChange(value) } }} openerRef={openerRef}
    title={msg('End this local session?')}
    description={msg('Active responses will stop. Unsaved changes may be lost. Saved packs and chats will not be deleted.')}
    footer={<DialogActions><Button disabled={busy} onClick={() => { setFailed(false); onOpenChange(false) }}>{msg('Cancel')}</Button><Button variant="primary" disabled={busy} onClick={async () => {
      setBusy(true); setFailed(false)
      try { await signOut() } catch { setFailed(true); setBusy(false) }
    }}>{busy ? msg('Ending session…') : msg('End session')}</Button></DialogActions>}>
    {failed && <p role="alert">{msg('Could not end your session. Try again.')}</p>}
  </Dialog>
}
