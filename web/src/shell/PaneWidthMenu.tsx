import { DropdownMenu } from 'radix-ui'
import { msg, useLocale } from '../i18n'
import { Tooltip } from '../ui/Tooltip'
import { IconMore } from './icons'

/** A pointer alternative to dragging, with the same bounds as the divider. */
export function PaneWidthMenu({value, min, max, onChange, onReset}: {
  value: number; min: number; max: number; onChange: (width: number) => void; onReset: () => void
}) {
  useLocale()
  return <DropdownMenu.Root modal={false}>
    <Tooltip content={msg('Pane width')}><DropdownMenu.Trigger asChild><button type="button" className="desk-icon-button" aria-label={msg('Pane width')}><IconMore /></button></DropdownMenu.Trigger></Tooltip>
    <DropdownMenu.Portal><DropdownMenu.Content className="desk-menu" align="end" sideOffset={6} collisionPadding={16} aria-label={msg('Pane width')}>
      <DropdownMenu.Item className="desk-menu-item" disabled={value <= min} onSelect={() => onChange(Math.max(min, value - 64))}>{msg('Narrower')}</DropdownMenu.Item>
      <DropdownMenu.Item className="desk-menu-item" disabled={value >= max} onSelect={() => onChange(Math.min(max, value + 64))}>{msg('Wider')}</DropdownMenu.Item>
      <DropdownMenu.Item className="desk-menu-item" onSelect={onReset}>{msg('Reset width')}</DropdownMenu.Item>
    </DropdownMenu.Content></DropdownMenu.Portal>
  </DropdownMenu.Root>
}
