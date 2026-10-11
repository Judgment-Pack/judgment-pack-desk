import { useCallback, useId, useLayoutEffect, useRef, useState, type CSSProperties, type ReactNode } from 'react'
import { msg } from '../i18n'
import { useMeasuredBox } from '../shell/measured'
import { IconClose, IconFocus } from '../shell/icons'
import { Button } from '../ui/Button'
import { PaneDivider } from '../ui/PaneDivider'
import { Tooltip } from '../ui/Tooltip'
import styles from './BuilderSplit.module.css'

/** Viewer preferences only; document bytes and chat state belong to their existing stores. */
export function useBuilderPreference<T extends string | number | boolean>(key: string, fallback: T) {
  const storageKey = `jp-desk:builder:${key}:v1`
  const [value, setValue] = useState<T>(() => {
    try { const saved: unknown = JSON.parse(localStorage.getItem(storageKey) ?? 'null'); if (typeof saved === typeof fallback) return saved as T } catch { /* Optional preference. */ }
    return fallback
  })
  const update = useCallback((next: T) => { setValue(next); try { localStorage.setItem(storageKey, JSON.stringify(next)) } catch { /* Viewing works without storage. */ } }, [storageKey])
  return [value, update] as const
}

/** Selection editing lives inside the route; Assistant stays in AppShell's right pane. */
export function BuilderSplit({ children, editor, title, open, onClose, preference }: {
  children: ReactNode; editor?: ReactNode; title: string; open: boolean; onClose: () => void; preference: 'graph' | 'pack'
}) {
  const id = useId()
  const [frame, setFrame] = useState<HTMLDivElement | null>(null)
  const box = useMeasuredBox(frame)
  const [height, setHeight] = useBuilderPreference<number>(`${preference}:editor-height`, 300)
  const [expanded, setExpanded] = useState(false)
  const max = Math.max(160, (box?.height || 600) - 180)
  const visible = open && !!editor
  const opener = useRef<HTMLElement | null>(null)
  useLayoutEffect(() => {if (visible && document.activeElement instanceof HTMLElement) opener.current = document.activeElement}, [visible])
  const close = () => {onClose(); requestAnimationFrame(() => {if (opener.current?.isConnected && opener.current.getClientRects().length) opener.current.focus({preventScroll: true})})}
  const size = Math.min(max, Math.max(160, Number.isFinite(height) ? height : 300))
  return <div ref={setFrame} className={styles.split} data-editor={visible || undefined} data-expanded={visible && expanded || undefined}
    style={{'--builder-editor-height': `${size}px`} as CSSProperties}>
    <div className={styles.main} hidden={visible && expanded}>{children}</div>
    <section id={id} aria-label={title} className={styles.editor} hidden={!visible}>
      {visible && !expanded && <PaneDivider orientation="horizontal" label={msg('Resize editor')} controls={id} value={size} min={160} max={max}
        onChange={setHeight} onReset={() => setHeight(300)} onCollapse={close} preview={{element: frame, property: '--builder-editor-preview'}}/>}
      <header className={styles.header}><h2>{title}</h2>
        <Tooltip content={expanded ? msg('Restore editor') : msg('Expand editor')}><Button size="icon" variant="quiet" aria-label={expanded ? msg('Restore editor') : msg('Expand editor')} aria-pressed={expanded} onClick={() => setExpanded(!expanded)}><IconFocus/></Button></Tooltip>
        <Tooltip content={msg('Close editor')}><Button size="icon" variant="quiet" aria-label={msg('Close editor')} onClick={close}><IconClose/></Button></Tooltip>
      </header>
      <div className={styles.body}>{editor}</div>
    </section>
  </div>
}
