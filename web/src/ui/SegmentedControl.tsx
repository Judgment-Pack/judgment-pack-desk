/**
 * One choice out of two or three, shown as the choices rather than as a menu.
 *
 * Edit | Read, Form | JSON, these edits | the saved pack, and each evidence
 * row's present | absent | unknown are the same control: a small closed set
 * where seeing the alternative is the point. A `Select` hides it behind a
 * trigger, and four of these on one toolbar would be four things to open.
 *
 * Radix `ToggleGroup` with `type="single"` gives the roving tab index, the
 * arrow keys and `aria-pressed` per item. What it also gives — and what this
 * component takes away — is **deselection**: pressing the item that is already
 * on reports `""`, an empty group with nothing chosen. That is a real state
 * for a toggle group and is not a state any of these controls has. Read is not
 * "neither Edit nor Read". So an empty answer is refused here and the current
 * value stands, which is what a segmented control does.
 */
import { ToggleGroup } from 'radix-ui'
import { useId } from 'react'
import styles from './SegmentedControl.module.css'

export interface Segment {
  value: string
  label: string
  /** Visible explanatory help, including why an option is unavailable. */
  description?: string
  disabled?: boolean
}

export function SegmentedControl({
  label,
  value,
  onValueChange,
  segments,
  id
}: {
  /** The group's accessible name. */
  label: string
  value: string
  onValueChange: (value: string) => void
  segments: readonly Segment[]
  id?: string
}) {
  const helpId = useId()
  const explanations = segments.filter(segment => segment.description)
  return (
    <div className={styles.container}>
    <ToggleGroup.Root
      id={id}
      type="single"
      className={styles.group}
      aria-label={label}
      aria-describedby={explanations.length ? helpId : undefined}
      value={value}
      onValueChange={(next) => {
        // A deselect is not a choice. Radix reports `""` when the pressed item
        // was already the value, and storing that empties the control.
        if (next === '') return
        onValueChange(next)
      }}
    >
      {segments.map((segment) => (
        <ToggleGroup.Item
          key={segment.value}
          className={styles.segment}
          value={segment.value}
          disabled={segment.disabled}
        >
          {segment.label}
        </ToggleGroup.Item>
      ))}
    </ToggleGroup.Root>
    {explanations.length > 0 && <p id={helpId} className={styles.help}>
      {explanations.map(segment => <span key={segment.value}>{segment.label}: {segment.description}</span>)}
    </p>}
    </div>
  )
}
