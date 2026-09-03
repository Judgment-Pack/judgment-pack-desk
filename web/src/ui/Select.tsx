/**
 * A Radix Select, styled through this component's own module.
 *
 * A native `<select>` cannot be styled to the desk's surface and border in any
 * cross-browser way, and it is the one control on the create form whose list
 * can grow long. Radix gives a listbox that is keyboard-complete — type-ahead,
 * arrows, Home/End, Escape — and exposes its items as `role="option"`, which is
 * what the tests query by.
 *
 * **The options do not exist until the trigger is opened.** That is Radix, not
 * this file, and it is why an assertion about what a select offers has to open
 * it first; `testing/radixGround.test.tsx` writes that down once.
 */
import { Select as RadixSelect } from 'radix-ui'
import { IconChevronDown } from '../shell/icons'
import styles from './Select.module.css'

export interface SelectOption {
  value: string
  label: string
}

export function Select({
  id,
  value,
  onValueChange,
  options,
  placeholder,
  ...described
}: {
  id: string
  value: string | undefined
  onValueChange: (value: string) => void
  options: readonly SelectOption[]
  placeholder?: string
  'aria-describedby'?: string
  'aria-invalid'?: boolean
}) {
  return (
    <RadixSelect.Root value={value} onValueChange={onValueChange}>
      <RadixSelect.Trigger id={id} className={styles.trigger} {...described}>
        <RadixSelect.Value placeholder={placeholder} />
        <RadixSelect.Icon className={styles.icon}>
          <IconChevronDown />
        </RadixSelect.Icon>
      </RadixSelect.Trigger>
      <RadixSelect.Portal>
        <RadixSelect.Content className={styles.content} position="popper" sideOffset={4}>
          <RadixSelect.Viewport className={styles.viewport}>
            {options.map((option) => (
              <RadixSelect.Item key={option.value} value={option.value} className={styles.item}>
                <RadixSelect.ItemText>{option.label}</RadixSelect.ItemText>
              </RadixSelect.Item>
            ))}
          </RadixSelect.Viewport>
        </RadixSelect.Content>
      </RadixSelect.Portal>
    </RadixSelect.Root>
  )
}
