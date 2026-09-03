/**
 * A label, a control, and the two things a control may need to say about
 * itself — with the id wiring owned here rather than repeated per form.
 *
 * `Field` generates one id, hands it to the control, and composes
 * `aria-describedby` from **only the descriptions actually rendered**. A field
 * that names a hint element it did not render points a screen reader at
 * nothing, which is worse than saying less: the reader announces an empty
 * description and the user has no way to know something was meant to be there.
 *
 * The control is a render prop taking the wiring as an argument. It is the one
 * shape that cannot be got wrong by a caller: there is no way to render an
 * input here without receiving the id, and no way to receive it and not be the
 * element the label points at.
 */
import { useId, type ReactNode } from 'react'
import styles from './Field.module.css'

export interface FieldWiring {
  id: string
  'aria-describedby': string | undefined
  'aria-invalid': boolean | undefined
}

export function Field({
  label,
  hint,
  error,
  children
}: {
  label: string
  hint?: ReactNode
  error?: ReactNode
  children: (wiring: FieldWiring) => ReactNode
}) {
  const id = useId()
  const hintId = `${id}-hint`
  const errorId = `${id}-error`
  const described = [hint ? hintId : undefined, error ? errorId : undefined].filter(Boolean)

  return (
    <div className={styles.field}>
      <label className={styles.label} htmlFor={id}>
        {label}
      </label>
      {children({
        id,
        'aria-describedby': described.length > 0 ? described.join(' ') : undefined,
        'aria-invalid': error ? true : undefined
      })}
      {hint && (
        <p id={hintId} className={styles.hint}>
          {hint}
        </p>
      )}
      {error && (
        <p id={errorId} className={styles.error}>
          {error}
        </p>
      )}
    </div>
  )
}
