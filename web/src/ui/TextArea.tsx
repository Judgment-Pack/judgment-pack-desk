/** One multi-line input. The wiring comes from `Field`. */
import type { ComponentPropsWithRef } from 'react'
import styles from './TextArea.module.css'

export function TextArea({ className, ...rest }: ComponentPropsWithRef<'textarea'>) {
  return <textarea {...rest} className={[styles.textarea, className].filter(Boolean).join(' ')} />
}
