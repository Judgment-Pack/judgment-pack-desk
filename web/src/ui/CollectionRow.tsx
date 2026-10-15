import type { HTMLAttributes, MouseEvent as ReactMouseEvent } from 'react'
import styles from './CollectionRow.module.css'

const controls = 'a,button,input,select,textarea,label,summary,[role="button"],[role="link"],[role="checkbox"],[role="switch"],[role="combobox"],[role="menuitem"],[contenteditable]:not([contenteditable="false"]),[data-row-ignore]'

/** Extend a marked link/button to the row, retaining native table and control semantics. */
export function CollectionRow({ actionable = true, className, ...props }: Omit<HTMLAttributes<HTMLTableRowElement>, 'onClick' | 'onAuxClick'> & { actionable?: boolean }) {
  function activate(event: ReactMouseEvent<HTMLTableRowElement>) {
    if (!actionable || event.defaultPrevented || (event.type === 'click' ? event.button !== 0 : event.button !== 1)) return
    const row = event.currentTarget, target = event.target
    if (!(target instanceof Element) || target.closest('tr') !== row || target.closest(controls)) return
    const selection = row.ownerDocument.getSelection()
    if (selection && !selection.isCollapsed && (row.contains(selection.anchorNode) || row.contains(selection.focusNode))) return
    const primary = row.querySelector<HTMLAnchorElement | HTMLButtonElement>('[data-row-primary]')
    if (!primary || primary.closest('tr') !== row || primary.matches(':disabled,[aria-disabled="true"]')) return
    const modified = event.metaKey || event.ctrlKey || event.shiftKey || event.altKey || event.button === 1
    if (modified && primary.tagName !== 'A') return
    event.preventDefault()
    if (event.button === 1) {
      // Browsers do not follow synthetic auxclick events on anchors.
      row.ownerDocument.defaultView?.open((primary as HTMLAnchorElement).href, '_blank', 'noopener')
      return
    }
    if (!modified) primary.focus({ preventScroll: true })
    // Forward modifiers to the native link for Ctrl/Cmd-click. Direct control
    // clicks never enter this delegation.
    primary.dispatchEvent(new MouseEvent(event.type, {
      bubbles: true, cancelable: true,
      button: event.button, ctrlKey: event.ctrlKey, metaKey: event.metaKey,
      shiftKey: event.shiftKey, altKey: event.altKey, detail: event.detail
    }))
  }
  return <tr {...props} className={[styles.row, className].filter(Boolean).join(' ')} data-row-action={actionable || undefined}
    onClick={activate} onAuxClick={activate}/>
}
