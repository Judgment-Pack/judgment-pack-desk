import type { ComponentPropsWithRef, ReactNode } from 'react'
import { Input } from './Input'
import { IconSearch } from '../shell/icons'
import styles from './SearchInput.module.css'

/** Shared Library-style search. Native input semantics, handlers and refs stay on the input. */
export function SearchInput({ className, suffix, type = 'search', ...input }: ComponentPropsWithRef<'input'> & { suffix?: ReactNode }) {
  return <div data-search-field className={[styles.field, className].filter(Boolean).join(' ')}>
    <IconSearch/>
    <Input {...input} type={type} className={styles.input}/>
    {suffix}
  </div>
}
