/**
 * One button, three weights and a destructive action.
 *
 * `type` defaults to `button`, deliberately. An HTML button inside a form
 * submits it unless told otherwise, so a Cancel that forgot its type is a
 * Cancel that creates the pack. The one control that means to submit says so.
 */
import type { ButtonHTMLAttributes } from 'react'
import { Link, type LinkProps } from 'react-router-dom'
import styles from './Button.module.css'

export type ButtonVariant = 'primary' | 'secondary' | 'quiet' | 'inline' | 'danger'

export interface ButtonProps extends ButtonHTMLAttributes<HTMLButtonElement> {
  variant?: ButtonVariant
}

export function Button({ variant = 'secondary', className, type, ...rest }: ButtonProps) {
  return (
    <button
      {...rest}
      type={type ?? 'button'}
      className={[styles.button, styles[variant], className].filter(Boolean).join(' ')}
    />
  )
}

/** Navigation with the same geometry and interaction states as an action. */
export function ButtonLink({ variant = 'secondary', className, ...props }: LinkProps & { variant?: ButtonVariant }) {
  return <Link {...props} className={[styles.button, styles[variant], className].filter(Boolean).join(' ')} />
}
