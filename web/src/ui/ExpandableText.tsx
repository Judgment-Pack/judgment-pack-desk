import { useId, useLayoutEffect, useRef, useState } from 'react'
import { Button } from './Button'
import styles from './ExpandableText.module.css'

/** Long prose stays readable on touch and keyboard without enlarging pane chrome. */
export function ExpandableText({ text, label }: { text: string; label: string }) {
  const ref = useRef<HTMLParagraphElement>(null)
  const id = useId()
  const [expanded, setExpanded] = useState(false)
  const [clipped, setClipped] = useState(false)
  useLayoutEffect(() => { setExpanded(false) }, [text])
  useLayoutEffect(() => {
    const element = ref.current
    if (!element) return
    const measure = () => setClipped(element.scrollHeight > element.clientHeight + 1)
    const observer = typeof ResizeObserver === 'undefined' ? undefined : new ResizeObserver(measure)
    observer?.observe(element); measure()
    return () => observer?.disconnect()
  }, [text, expanded])
  return <div className={styles.root}>
    <p id={id} ref={ref} className={styles.text} data-expanded={expanded || undefined}>{text}</p>
    {(clipped || expanded) && <Button variant="inline" aria-expanded={expanded} aria-controls={id}
      onClick={() => setExpanded(value => !value)}>{expanded ? 'Show less' : `Read full ${label}`}</Button>}
  </div>
}
