import { DropdownMenu } from 'radix-ui'
import { IconCheck, IconChevronDown } from '../shell/icons'
import { Button } from './Button'
import type { SelectOption } from './Select'
import styles from './SortMenu.module.css'

/** Collection ordering is a toolbar command, using the shared menu surface. */
export function SortMenu({ label, value, options, onValueChange }: {
  label: string
  value: string
  options: readonly SelectOption[]
  onValueChange: (value: string) => void
}) {
  return <DropdownMenu.Root>
    <DropdownMenu.Trigger asChild><Button variant="quiet" aria-label={label}>Sort <IconChevronDown /></Button></DropdownMenu.Trigger>
    <DropdownMenu.Portal>
      <DropdownMenu.Content className="desk-menu" align="end" sideOffset={6} collisionPadding={16} aria-label={label}>
        <DropdownMenu.RadioGroup value={value} onValueChange={onValueChange}>
          {options.map(option => <DropdownMenu.RadioItem key={option.value} value={option.value} className="desk-menu-item">
            <span aria-hidden="true" className={styles.indicator}><DropdownMenu.ItemIndicator><IconCheck /></DropdownMenu.ItemIndicator></span>
            {option.label}
          </DropdownMenu.RadioItem>)}
        </DropdownMenu.RadioGroup>
      </DropdownMenu.Content>
    </DropdownMenu.Portal>
  </DropdownMenu.Root>
}
