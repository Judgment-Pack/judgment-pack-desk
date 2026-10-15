import type { KeyboardEvent } from 'react'

/** Chat-only shortcut. Leave modified Enter and composition to the text input. */
export function submitOnEnter(event: KeyboardEvent<HTMLElement>, submit: () => void) {
  if (event.defaultPrevented || event.key !== 'Enter' || event.shiftKey || event.ctrlKey || event.metaKey || event.altKey
    || event.nativeEvent.isComposing || event.nativeEvent.keyCode === 229) return
  // A held key must neither submit twice nor insert a stray newline.
  event.preventDefault()
  if (!event.repeat) submit()
}
