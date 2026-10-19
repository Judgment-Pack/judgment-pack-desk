import type {ComponentProps, ReactNode} from 'react'
import {Button, ButtonLink, type ButtonProps} from '../ui/Button'
import styles from './ChatActions.module.css'
/** Shared chat action geometry. Feature owners retain save, consent and revision guards. */
export function ChatActions({children,detail}:{children:ReactNode;detail?:ReactNode}) {
  return <div className={styles.root}><div className={styles.actions}>{children}</div>{detail && <div className={styles.detail} role="status">{detail}</div>}</div>
}
export function ChatAction({icon,children,size="small",className,...props}:ButtonProps & {icon?:ReactNode}) {
  return <Button className={[size !== "icon" && size !== "small-icon" && props.variant !== "inline" && styles.action, className].filter(Boolean).join(" ")} size={size === "icon" ? "small-icon" : size} {...props}>{icon}{children}</Button>
}
export function ChatActionLink({className,...props}:ComponentProps<typeof ButtonLink>) { return <ButtonLink size="small" className={[styles.action,className].filter(Boolean).join(" ")} {...props}/> }
