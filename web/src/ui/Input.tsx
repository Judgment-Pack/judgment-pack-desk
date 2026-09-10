/**
 * One text input. The wiring comes from `Field`; nothing is invented here.
 *
 * **It takes a ref**, because one input on this desk is deliberately
 * uncontrolled: the assistant key is read off the node and cleared on it, at
 * the instant the request is made, and a `setState` would not have taken effect
 * by then. Before this it was the one control on Admin rendered as the
 * browser's own — a bare `<input>` beside three that carry the desk's border,
 * height and focus ring.
 */
import type { ComponentPropsWithRef } from 'react'
import styles from './Input.module.css'

export function Input({ className, ...rest }: ComponentPropsWithRef<'input'>) {
  return <input {...rest} className={[styles.input, className].filter(Boolean).join(' ')} />
}
