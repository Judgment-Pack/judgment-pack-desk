import type { MouseEventHandler, Ref } from 'react'
import { IconPanelLeft, IconPanelRight } from './icons'
import { Tooltip } from '../ui/Tooltip'

/** One visual control for retaining or hiding a pane; the caller names its scope. */
export function PaneToggle({ label, expanded, onClick, side = 'left', controls, shortcut, buttonRef, compact, ...attributes }: {
  label: string
  expanded: boolean
  compact?: boolean
  onClick: MouseEventHandler<HTMLButtonElement>
  side?: 'left' | 'right'
  controls?: string
  shortcut?: string
  buttonRef?: Ref<HTMLButtonElement>
  'data-show-folders'?: boolean
}) {
  return <Tooltip content={label} shortcut={shortcut} openOnFocus={false}><button
    {...attributes} ref={buttonRef} type="button" className="desk-icon-button" data-compact={compact || undefined}
    aria-label={label} aria-expanded={expanded} aria-controls={controls} onClick={onClick}
  >{side === 'left' ? <IconPanelLeft /> : <IconPanelRight />}</button></Tooltip>
}
