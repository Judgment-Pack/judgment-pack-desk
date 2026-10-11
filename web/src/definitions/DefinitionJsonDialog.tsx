import type { RefObject } from 'react'
import { msg, useLocale } from '../i18n'
import { CodeBlock } from '../ui/CodeBlock'
import { Dialog, DialogActions } from '../ui/Dialog'
import { Button } from '../ui/Button'

/** Exact retained bytes, including malformed JSON. Viewing never formats or writes a definition. */
export function DefinitionJsonDialog({ open, onOpenChange, text, title, openerRef }: {
  open: boolean; onOpenChange: (open: boolean) => void; text: string; title: string; openerRef: RefObject<HTMLElement | null>
}) {
  useLocale()
  return <Dialog open={open} onOpenChange={onOpenChange} title={title} size="wide" openerRef={openerRef}
    footer={<DialogActions><Button onClick={() => onOpenChange(false)}>{msg('Close')}</Button></DialogActions>}>
    {open && <CodeBlock text={text} label={title} defaultWrap={false} expanded/>}
  </Dialog>
}
