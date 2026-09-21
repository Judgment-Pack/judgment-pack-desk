import { useCallback, useEffect, useRef, type ReactNode } from 'react'
import { msg, useLocale } from '../i18n'
import { useDetailsSlot } from '../shell/DetailsSlot'
import { CodeBlock } from '../ui/CodeBlock'
import { Disclosure } from '../ui/Disclosure'
import styles from './ReadingDetails.module.css'

/** The shell owns one temporary reader, replacing rather than stacking details. */
export function useReadingDetails(owner: string) {
  const details = useDetailsSlot()
  const release = useRef<(() => void) | undefined>(undefined)
  useEffect(() => () => { release.current?.(); release.current = undefined }, [owner])
  return useCallback((node: ReactNode, opener: HTMLElement | null = document.activeElement instanceof HTMLElement ? document.activeElement : null) => {
    release.current?.()
    release.current = details.inspect?.(node, opener)
  }, [details.inspect])
}

export function ReadingDetails({ title, children, actions }: { title: string; children: ReactNode; actions?: ReactNode }) {
  const details = useDetailsSlot()
  const root = useRef<HTMLElement>(null)
  useEffect(() => {
    const frame = requestAnimationFrame(() => root.current?.focus({ preventScroll: true }))
    return () => cancelAnimationFrame(frame)
  }, [])
  return <section ref={root} className={styles.reader} tabIndex={-1} aria-label={title} onKeyDown={event => {
    if (event.key === 'Escape' && !event.defaultPrevented) { event.preventDefault(); details.dismissInspection?.() }
  }}><header className={styles.heading}><h2>{title}</h2>{actions}</header><div className={styles.body}>{children}</div></section>
}

export function MessageDetails({ text, input }: { text: string; input: string }) {
  useLocale()
  return <ReadingDetails title={msg('Message details')}>
    <p className={styles.text}>{text}</p>
    <Disclosure title={msg('Exact sent context')}><CodeBlock text={input} label={msg('Sent context')} /></Disclosure>
  </ReadingDetails>
}

export function PackContextDetails({ text }: { text: string }) {
  useLocale()
  return <ReadingDetails title={msg('Pack context')}><p>{msg('The current pack is included with your next message. Proposed edits require your review.')}</p><CodeBlock text={text} label={msg('Pack')} /></ReadingDetails>
}
