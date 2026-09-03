/**
 * What went wrong, announced where it happened.
 *
 * `role="alert"` rather than a styled paragraph: a form-level failure appears
 * after the action that caused it, and a reader that is not told about it
 * leaves the user waiting on a dialog that has already answered. The role is
 * what makes it an announcement; the colour is what makes it findable by
 * everyone else.
 *
 * The reason is its own element. What the desk says and what the failure
 * itself said are two statements, and keeping them apart is what lets the
 * second be read — and asserted — on its own rather than as a tail of the
 * first.
 */
import type { ReactNode } from 'react'
import styles from './Alert.module.css'

export function Alert({ children, reason }: { children: ReactNode; reason?: ReactNode }) {
  return (
    <p role="alert" className={styles.alert}>
      {children}
      {reason !== undefined && reason !== '' && <> <span className={styles.reason}>{reason}</span></>}
    </p>
  )
}
