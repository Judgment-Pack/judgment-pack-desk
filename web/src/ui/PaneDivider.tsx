import { msg, useLocale } from '../i18n'
import { Tooltip } from './Tooltip'
import { useEffect, useRef, useState, type KeyboardEvent, type PointerEvent } from 'react'
import styles from './PaneDivider.module.css'

/** A temporary CSS size override. The owner commits its preference on release. */
export interface PaneResizePreview {
  element: HTMLElement | null
  property: `--${string}`
}
interface Drag {
  id: number
  origin: number
  initial: number
  next: number
  painted: number
  frame: number | null
  element: HTMLDivElement
  preview: PaneResizePreview | undefined
  cleanup: () => void
}

/** Shared pointer, keyboard, hover and focus behavior for every pane divider. */
export function PaneDivider({ label, controls, value, min, max, onChange, onReset, onCollapse, preview, orientation = 'vertical', paneSide = 'end' }: {
  orientation?: 'vertical' | 'horizontal'
  /** Where the pane lies relative to this divider. */
  paneSide?: 'start' | 'end'
  label: string; controls: string; value: number; min: number; max: number
  onChange: (value: number) => void; onReset: () => void; onCollapse: () => void
  preview?: PaneResizePreview
}) {
  useLocale()
  const horizontal = orientation === 'horizontal'
  const direction = paneSide === 'start' ? -1 : 1
  const dimension = horizontal ? msg('height') : msg('width')
  const help = msg("Drag to resize. Arrow keys adjust {{value0}}; Shift moves faster. Double-click to reset.", { value0: dimension })
  const drag = useRef<Drag | null>(null)
  const [liveValue, setLiveValue] = useState<number | null>(null)
  const [dragging, setDragging] = useState(false)
  const [keyboardFocus, setKeyboardFocus] = useState(false)
  const bound = (next: number) => Math.round(Math.max(min, Math.min(max, next)))
  const position = (event: PointerEvent<HTMLDivElement>) => horizontal ? event.clientY : event.clientX
  const finish = (commit: boolean) => {
    const start = drag.current
    if (!start) return
    drag.current = null
    if (start.frame !== null) cancelAnimationFrame(start.frame)
    const next = bound(commit ? start.next : start.initial)
    // With a CSS preview, no application state or storage changes during a drag.
    // Non-preview consumers still receive frame-coalesced changes.
    if (start.preview ? commit && next !== start.initial : next !== value) onChange(next)
    start.cleanup()
    if (start.element.hasPointerCapture(start.id)) start.element.releasePointerCapture(start.id)
    setLiveValue(null); setDragging(false)
  }
  const move = (event: PointerEvent<HTMLDivElement>) => {
    const start = drag.current
    if (!start || start.id !== event.pointerId) return
    start.next = bound(start.initial + direction * (start.origin - position(event)))
    if (start.frame !== null) return
    start.frame = requestAnimationFrame(() => {
      start.frame = null
      if (drag.current !== start || start.painted === start.next) return
      if (start.preview) start.preview.element!.style.setProperty(start.preview.property, `${start.next}px`)
      else onChange(start.next)
      start.painted = start.next
      setLiveValue(start.next)
    })
  }
  useEffect(() => () => {
    const start = drag.current
    drag.current = null
    if (start) {
      if (start.frame !== null) cancelAnimationFrame(start.frame)
      start.cleanup()
      if (start.element.hasPointerCapture(start.id)) start.element.releasePointerCapture(start.id)
    }
  }, [])
  // A viewport or mode change invalidates the original gesture's bounds.
  useEffect(() => { finish(false) }, [min, max, orientation, paneSide, preview?.element, preview?.property])
  const key = (event: KeyboardEvent<HTMLDivElement>) => {
    if (event.defaultPrevented) return
    const keys = [horizontal ? 'ArrowUp' : 'ArrowLeft', horizontal ? 'ArrowDown' : 'ArrowRight', 'Home', 'End', 'Enter', 'Escape']
    if (!keys.includes(event.key)) return
    const wasDragging = drag.current !== null
    finish(false)
    setKeyboardFocus(true)
    const step = event.shiftKey ? 32 : 8
    if (event.key === keys[0]) onChange(bound(value + direction * step))
    else if (event.key === keys[1]) onChange(bound(value - direction * step))
    else if (event.key === 'Home') onChange(min)
    else if (event.key === 'End') onChange(max)
    else if (event.key === 'Enter') onCollapse()
    else if (!wasDragging) onReset()
    event.preventDefault()
  }
  const shownValue = liveValue ?? value
  return <Tooltip disabled={dragging || keyboardFocus} openOnFocus={false} side="left" content={help}><div className={styles.divider} data-orientation={orientation} data-pane-side={paneSide} data-dragging={dragging || undefined}
    data-keyboard-focus={keyboardFocus || undefined} role="separator" tabIndex={0}
    aria-label={label} aria-controls={controls} aria-orientation={orientation}
    aria-valuemin={min} aria-valuemax={max} aria-valuenow={shownValue} aria-valuetext={horizontal ? msg('{{value}} pixels high', { value: shownValue }) : msg('{{value}} pixels wide', { value: shownValue })}
    aria-description={help}
    onKeyDown={key} onDoubleClick={() => { finish(false); onReset() }}
    onFocus={event => setKeyboardFocus(event.currentTarget.matches(':focus-visible'))}
    onBlur={() => setKeyboardFocus(false)}
    onPointerDown={event => {
      if (event.button !== 0 || !event.isPrimary || drag.current) return
      event.preventDefault(); event.currentTarget.focus({ preventScroll: true })
      setKeyboardFocus(false)
      event.currentTarget.setPointerCapture(event.pointerId)
      const activePreview = preview?.element ? preview : undefined
      const target = activePreview?.element, property = activePreview?.property
      const previous = target && property ? target.style.getPropertyValue(property) : ''
      const priority = target && property ? target.style.getPropertyPriority(property) : ''
      const body = document.body, cursor = body.style.cursor, selection = body.style.userSelect
      body.style.cursor = horizontal ? 'row-resize' : 'col-resize'; body.style.userSelect = 'none'
      drag.current = {
        id: event.pointerId, origin: position(event), initial: value, next: value, painted: value,
        frame: null, element: event.currentTarget, preview: activePreview,
        cleanup: () => {
          if (target && property) {
            if (previous) target.style.setProperty(property, previous, priority)
            else target.style.removeProperty(property)
          }
          body.style.cursor = cursor; body.style.userSelect = selection
        }
      }
      setDragging(true)
    }} onPointerMove={move} onPointerUp={event => {
      if (drag.current?.id !== event.pointerId) return
      // Include the release coordinate even when the last animation frame has not run.
      move(event); finish(true)
    }} onPointerCancel={event => { if (drag.current?.id === event.pointerId) finish(false) }}
    onLostPointerCapture={event => { if (drag.current?.id === event.pointerId) finish(false) }} /></Tooltip>
}
