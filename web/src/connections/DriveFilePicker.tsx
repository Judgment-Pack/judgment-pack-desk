import { useState, type RefObject } from 'react'
import { msg } from '../i18n'
import { Button } from '../ui/Button'
import { Dialog, DialogActions } from '../ui/Dialog'
import { ConnectionsPane } from './ConnectionsPane'
import type { SourceSelection } from './client'

/** Jobs use the same explicit search → select flow as chat, with one file. */
export function DriveFilePicker({ openerRef, onClose, onSelect }: {
 openerRef: RefObject<HTMLElement | null>
 onClose: () => void
 onSelect: (items: SourceSelection[], signal: AbortSignal) => Promise<void>
}) {
 const [target, setTarget] = useState<HTMLDivElement | null>(null)
 return <Dialog open title={msg('Choose from Google Drive')} description={msg('Choose one JSON file up to 200 KB.')} openerRef={openerRef} onOpenChange={open => { if (!open) onClose() }} footer={<DialogActions><Button onClick={onClose}>{msg('Close')}</Button></DialogActions>}>
  <div ref={setTarget} />
  <ConnectionsPane request={{provider:'google-drive', opener:openerRef.current, selection:{limit:1,onSelect}}} target={target} onProvider={() => {}} onClose={onClose} onAttached={onClose} onBusy={() => {}} />
 </Dialog>
}
