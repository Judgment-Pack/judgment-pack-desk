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
 *
 * **A value the caller never offered is not a choice, and is dropped.** This
 * is not defensiveness; it is repairing a specific, reproducible Radix
 * behaviour that only appears *inside a form*. There, `Select.Root` mirrors its
 * value into a hidden native `<select>` and dispatches `change` on it whenever
 * the value changes. The options of that native select come from the `Item`s
 * that have registered — and items mount only while the list is open. So a
 * controlled value that changes while the list is closed sets
 * `select.value = "…"` against a select with no such option, which leaves it
 * `""`, and the dispatched `change` reports `""` straight back through
 * `onValueChange`. A caller that stores what it is handed then loses the
 * selection it just made, and the trigger goes blank.
 *
 * That is exactly what a default arriving from the runtime does: the dialog
 * starts on "Empty pack", the example listing answers, the default moves to
 * the first example, and one tick later the choice is `""`. Dropping a value
 * that is not in `options` fixes it precisely — none of these options is ever
 * `""`, so nothing a person can actually pick is filtered.
 */
import { Select as RadixSelect } from 'radix-ui'
import { useMemo } from 'react'
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
  const offered = useMemo(() => new Set(options.map((option) => option.value)), [options])
  return (
    <RadixSelect.Root
      // **Never undefined.** A caller with nothing selected yet — a listing
      // still in flight — used to hand Radix `undefined`, which makes the
      // component uncontrolled; the first real value then switched it to
      // controlled, and React warns about exactly that for the reason it is
      // worth warning about. The empty string is "controlled, and nothing is
      // selected", which is the state that was meant.
      value={value ?? ''}
      onValueChange={(next) => {
        if (offered.has(next)) onValueChange(next)
      }}
    >
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
