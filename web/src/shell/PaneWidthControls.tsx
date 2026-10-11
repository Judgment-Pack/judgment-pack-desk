import { msg, useLocale } from '../i18n'
import { Tooltip } from '../ui/Tooltip'
import { Button } from '../ui/Button'
import { IconChevronLeft, IconChevronRight } from './icons'

/** Direct pointer and keyboard controls with the same bounds as the divider. */
export function PaneWidthControls({value, min, max, onChange, onReset}: {
  value: number; min: number; max: number; onChange: (width: number) => void; onReset: () => void
}) {
  useLocale()
  return <div className="desk-pane-width-controls" role="group" aria-label={msg('Pane width')}>
    <Tooltip content={msg('Narrower')}><button type="button" className="desk-icon-button" aria-label={msg('Narrower')} disabled={value <= min} onClick={() => onChange(Math.max(min, value - 64))}><IconChevronRight /></button></Tooltip>
    <Tooltip content={msg('Wider')}><button type="button" className="desk-icon-button" aria-label={msg('Wider')} disabled={value >= max} onClick={() => onChange(Math.min(max, value + 64))}><IconChevronLeft /></button></Tooltip>
    <Button variant="quiet" onClick={onReset}>{msg('Reset width')}</Button>
  </div>
}
