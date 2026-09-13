import type { ButtonHTMLAttributes, ReactNode } from 'react'
import { IconChevronRight } from '../shell/icons'
import styles from './InspectionRow.module.css'

/** Opens the details of displayed information; it never executes that information. */
export function InspectionRow({ label, value, description, current = false, className, ...props }: {
  label: string
  value?: ReactNode
  description?: ReactNode
  current?: boolean
} & Omit<ButtonHTMLAttributes<HTMLButtonElement>, 'children' | 'type'>) {
  return <button {...props} type="button" data-inspection-row aria-current={current || undefined}
    className={[styles.row, className].filter(Boolean).join(' ')}>
    <span className={styles.text}><span>{label}</span>{description && <span className={styles.description}>{description}</span>}</span>
    {value !== undefined && <span className={styles.value}>{value}</span>}
    <IconChevronRight />
  </button>
}
