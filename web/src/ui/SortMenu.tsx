import { msg, useLocale } from '../i18n'
import { MenuChoice } from './Menu'
import type { SelectOption } from './Select'
import styles from './SortMenu.module.css'

/** Sorting and collection filters share one choice-menu presentation. */
export function SortMenu({ label, value, options, onValueChange }: {
  label: string
  value: string
  options: readonly SelectOption[]
  onValueChange: (value: string) => void
}) {
  useLocale()
  return <MenuChoice label={label} triggerLabel={msg('Sort')} triggerClassName={styles.trigger} value={value} onValueChange={onValueChange} options={options}/>
}
