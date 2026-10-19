/**
 * Shared text and square icon actions, with navigation using the same geometry.
 *
 * `type` defaults to `button`, deliberately. An HTML button inside a form
 * submits it unless told otherwise, so a Cancel that forgot its type is a
 * Cancel that creates the pack. The one control that means to submit says so.
 */
import type { ComponentPropsWithRef } from 'react'
import { Link, type LinkProps } from 'react-router-dom'
import styles from './Button.module.css'

export type ButtonVariant = 'primary' | 'secondary' | 'quiet' | 'inline' | 'danger'

export type ButtonSize = 'default' | 'icon' | 'small' | 'small-icon'

export interface ButtonProps extends ComponentPropsWithRef<'button'> {
  variant?: ButtonVariant
  size?: ButtonSize
}

export function Button({ variant = 'secondary', size = 'default', className, type, ...rest }: ButtonProps) {
  return (
    <button
      {...rest}
      type={type ?? 'button'}
      className={[styles.button, styles[variant], (size === 'small' || size === 'small-icon') && styles.small, (size === 'icon' || size === 'small-icon') && styles.icon, className]
        .filter(Boolean)
        .join(' ')}
    />
  )
}

/** Navigation with the same geometry and interaction states as an action. */
export function ButtonLink({
  variant = 'secondary',
  size = 'default',
  className,
  ...props
}: LinkProps & { variant?: ButtonVariant; size?: ButtonSize }) {
  return (
    <Link
      {...props}
      className={[styles.button, styles[variant], (size === 'small' || size === 'small-icon') && styles.small, (size === 'icon' || size === 'small-icon') && styles.icon, className]
        .filter(Boolean)
        .join(' ')}
    />
  )
}
