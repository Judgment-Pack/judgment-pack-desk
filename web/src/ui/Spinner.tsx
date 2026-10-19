import styles from './Spinner.module.css'

/** Decorative motion; the adjacent status text carries the accessible meaning. */
export function Spinner() {
  return <span className={styles.spinner} data-spinner="" aria-hidden="true"/>
}
