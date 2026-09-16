import { useState, type RefObject } from 'react'
import { EndpointForm } from '../assistant/EndpointForm'
import { useAssistantSlot } from '../assistant/useAssistantSlot'
import { Dialog, DialogActions } from '../ui/Dialog'
import { Button, ButtonLink } from '../ui/Button'

/** The same credential and endpoint controls as Admin; the chat keeps its text. */
export function ConfigureAI({ open, onOpenChange, openerRef }: { open: boolean; onOpenChange: (open: boolean) => void; openerRef: RefObject<HTMLElement | null> }) {
  const slot = useAssistantSlot()
  const [dirty, setDirty] = useState(false)
  const close = () => {
    if (dirty && !window.confirm('Close AI settings and discard the unsaved changes?')) return false
    setDirty(false); onOpenChange(false); return true
  }
  return <Dialog open={open} onOpenChange={next => { if (next) onOpenChange(true); else close() }} title="Configure AI" description="Connect a provider and choose a model. Your API key stays on this computer." openerRef={openerRef}>
    <EndpointForm unavailable={slot.state === 'unavailable'} onDirtyChange={setDirty} />
    <DialogActions><ButtonLink to="/admin#assistant" onClick={event => { if (!close()) event.preventDefault() }} variant="quiet">Open Admin settings</ButtonLink><Button onClick={close}>Done</Button></DialogActions>
  </Dialog>
}
