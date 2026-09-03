/** One text input. The wiring comes from `Field`; nothing is invented here. */
import type { InputHTMLAttributes } from 'react'
import styles from './Input.module.css'

export function Input({ className, ...rest }: InputHTMLAttributes<HTMLInputElement>) {
  return <input {...rest} className={[styles.input, className].filter(Boolean).join(' ')} />
}
